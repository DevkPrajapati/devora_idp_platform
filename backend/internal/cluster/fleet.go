package cluster

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/idp/platform/backend/internal/auth"
	"github.com/idp/platform/backend/internal/config"
	db "github.com/idp/platform/backend/internal/database/sqlc"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
	"github.com/idp/platform/backend/internal/kubernetes"
	"github.com/idp/platform/backend/internal/pkg/convert"
	"github.com/idp/platform/backend/internal/repository"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/homedir"
)

var clusterNameRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

const (
	maxClusterNameLen  = 48
	maxWorkers         = int32(3)
	provisionTimeout   = 10 * time.Minute
	statusProvisioning = "provisioning"
	statusStarting     = "starting"
	statusRunning      = "running"
	statusStopping     = "stopping"
	statusStopped      = "stopped"
	statusError        = "error"
	statusDeleting     = "deleting"
)

// ListClusters returns the fleet plus which local provisioners are installed.
func (s *Service) ListClusters(ctx context.Context, _ *idpv1.ListClustersRequest) (*idpv1.ListClustersResponse, error) {
	if s.repo == nil {
		return &idpv1.ListClustersResponse{}, nil
	}
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*idpv1.ManagedCluster, 0, len(rows))
	for i := range rows {
		out = append(out, s.toProto(&rows[i]))
	}
	return &idpv1.ListClustersResponse{
		Clusters:          out,
		KindAvailable:     s.provisioner != nil && s.provisioner.KindAvailable(),
		MinikubeAvailable: s.provisioner != nil && s.provisioner.MinikubeAvailable(),
	}, nil
}

// CreateCluster registers or provisions a cluster. Kind/minikube create is
// asynchronous: the row is returned as provisioning and a background job
// finishes the work so the RPC is not held open for minutes.
func (s *Service) CreateCluster(ctx context.Context, req *idpv1.CreateClusterRequest) (*idpv1.ManagedCluster, error) {
	if s.repo == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("cluster fleet is not configured"))
	}
	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	name := strings.ToLower(strings.TrimSpace(req.Name))
	if err := validateClusterName(name); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	display := strings.TrimSpace(req.DisplayName)
	if display == "" {
		display = name
	}
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" {
		provider = kubernetes.ProviderKind
	}
	switch provider {
	case kubernetes.ProviderKind, kubernetes.ProviderMinikube, kubernetes.ProviderImported:
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("provider must be kind, minikube, or imported"))
	}
	if req.WorkerCount < 0 || req.WorkerCount > maxWorkers {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("worker_count must be between 0 and %d", maxWorkers))
	}

	if _, err := s.repo.GetByName(ctx, name); err == nil {
		return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("cluster %q already exists", name))
	} else if !errors.Is(err, repository.ErrClusterNotFound) {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	count, err := s.repo.Count(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	activate := req.Activate || count == 0

	switch provider {
	case kubernetes.ProviderImported:
		row, err := s.createImported(ctx, user, name, display, req.Kubeconfig, activate)
		if err != nil {
			return nil, err
		}
		s.record(ctx, "cluster.create", name, "success", map[string]any{
			"provider": provider,
			"activate": activate,
		})
		return s.toProto(row), nil
	default:
		row, err := s.repo.Create(ctx, repository.CreateClusterInput{
			Name:        name,
			DisplayName: display,
			Provider:    provider,
			Status:      statusProvisioning,
			CreatedBy:   user.Email,
			NodeCount:   1 + req.WorkerCount,
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		s.logLine(row.ID, "provision", fmt.Sprintf("queued %s create for cluster %s", provider, name))
		jobCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), provisionTimeout)
		jobCtx = s.withJobLogs(jobCtx, row.ID, "provision")
		go func() {
			defer cancel()
			s.provisionLocal(jobCtx, row.ID, name, provider, req.KubernetesVersion, req.WorkerCount, activate)
		}()
		s.record(ctx, "cluster.create", name, "success", map[string]any{
			"provider": provider,
			"status":   statusProvisioning,
		})
		return s.toProto(row), nil
	}
}

func (s *Service) createImported(ctx context.Context, user *auth.User, name, display, kubeconfig string, activate bool) (*db.Cluster, error) {
	raw := strings.TrimSpace(kubeconfig)
	if raw == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("kubeconfig is required when importing a cluster"))
	}
	server, err := kubernetes.ValidateKubeconfig([]byte(raw))
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	sealed, err := s.sealKubeconfig([]byte(raw))
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewClientFromKubeconfig([]byte(raw), name, s.k8sCfg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("kubeconfig is not usable: %w", err))
	}
	pingCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("could not reach the imported cluster: %w", err))
	}
	version, nodes := clusterFacts(pingCtx, client)

	row, err := s.repo.Create(ctx, repository.CreateClusterInput{
		Name:                name,
		DisplayName:         display,
		Provider:            kubernetes.ProviderImported,
		Status:              statusRunning,
		KubeconfigEncrypted: sealed,
		ServerURL:           server,
		KubernetesVersion:   version,
		NodeCount:           nodes,
		CreatedBy:           user.Email,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.logLine(row.ID, "provision", "imported kubeconfig; API ping succeeded")
	if activate {
		if err := s.bindActive(ctx, row, client); err != nil {
			return nil, err
		}
		row, err = s.repo.Get(ctx, row.ID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		s.logLine(row.ID, "lifecycle", "activated as the platform cluster")
	}
	return row, nil
}

func (s *Service) provisionLocal(ctx context.Context, id pgtype.UUID, name, provider, version string, workers int32, activate bool) {
	var (
		kubeconfig []byte
		server     string
		err        error
	)
	s.logLine(id, "provision", fmt.Sprintf("starting %s create cluster %s", provider, name))
	switch provider {
	case kubernetes.ProviderKind:
		kubeconfig, server, err = s.provisioner.CreateKind(ctx, kubernetes.CreateKindSpec{
			Name:              name,
			KubernetesVersion: version,
			WorkerCount:       workers,
		})
	case kubernetes.ProviderMinikube:
		kubeconfig, server, err = s.provisioner.CreateMinikube(ctx, name, version)
	default:
		err = fmt.Errorf("unsupported provider %s", provider)
	}
	if err != nil {
		s.logLine(id, "provision", "failed: "+err.Error())
		if s.logger != nil {
			s.logger.Error("cluster provision failed", zap.String("name", name), zap.Error(err))
		}
		_, _ = s.repo.SetStatus(ctx, id, statusError, err.Error())
		s.record(ctx, "cluster.provision", name, "failure", map[string]any{
			"error": err.Error(),
		})
		return
	}

	sealed, sealErr := s.sealKubeconfig(kubeconfig)
	if sealErr != nil && s.logger != nil {
		s.logger.Warn("could not encrypt kubeconfig; cluster can still be activated via the provisioner", zap.Error(sealErr))
		sealed = nil
	}
	client, clientErr := kubernetes.NewClientFromKubeconfig(kubeconfig, name, s.k8sCfg)
	versionOut, nodes := version, int32(1+workers)
	if clientErr == nil {
		v, n := clusterFacts(ctx, client)
		if v != "" {
			versionOut = v
		}
		if n > 0 {
			nodes = n
		}
	}
	row, err := s.repo.UpdateRuntime(ctx, repository.UpdateRuntimeInput{
		ID:                  id,
		KubeconfigEncrypted: sealed,
		ServerURL:           server,
		KubernetesVersion:   versionOut,
		NodeCount:           nodes,
		Status:              statusRunning,
	})
	if err != nil {
		s.logLine(id, "provision", "failed to save cluster row: "+err.Error())
		if s.logger != nil {
			s.logger.Error("cluster row update failed", zap.Error(err))
		}
		return
	}
	s.logLine(id, "provision", fmt.Sprintf("cluster %s is running", name))
	if provider == kubernetes.ProviderMinikube {
		s.enableLocalAddons(ctx, name)
	}
	if activate && clientErr == nil {
		if err := s.bindActive(ctx, row, client); err != nil {
			s.logLine(id, "provision", "activate after create failed: "+err.Error())
			if s.logger != nil {
				s.logger.Error("activate after provision failed", zap.Error(err))
			}
		} else {
			s.logLine(id, "lifecycle", "activated as the platform cluster")
		}
	}
	s.record(ctx, "cluster.provision", name, "success", map[string]any{
		"provider": provider,
		"activate": activate,
	})
}

// ActivateCluster makes the named cluster the one every platform service uses.
func (s *Service) ActivateCluster(ctx context.Context, req *idpv1.ActivateClusterRequest) (*idpv1.ManagedCluster, error) {
	row, err := s.requireCluster(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if row.Status != statusRunning && row.Status != statusStopped {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("cluster %s is %s and cannot be activated", row.Name, row.Status))
	}
	client, err := s.clientFor(ctx, row)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("cluster %s is not reachable: %w", row.Name, err))
	}
	if row.Status != statusRunning {
		row, err = s.repo.SetStatus(ctx, row.ID, statusRunning, "")
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}
	if err := s.bindActive(ctx, row, client); err != nil {
		return nil, err
	}
	row, err = s.repo.Get(ctx, row.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.record(ctx, "cluster.activate", row.Name, "success", nil)
	s.logLine(row.ID, "lifecycle", "activated as the platform cluster")
	return s.toProto(row), nil
}

// StopCluster pauses a local cluster. The RPC returns as soon as the stop is
// queued: minikube/kind stop takes long enough that holding the request open
// made the UI look frozen. Imported clusters are disconnected, not destroyed.
func (s *Service) StopCluster(ctx context.Context, req *idpv1.StopClusterRequest) (*idpv1.ManagedCluster, error) {
	row, err := s.requireCluster(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if busyStatus(row.Status) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("cluster %s is %s", row.Name, row.Status))
	}
	if row.Status == statusStopped {
		return s.toProto(row), nil
	}
	id := uuidString(row.ID)
	if !s.jobs.begin(id, "stop") {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("cluster %s already has a lifecycle job running", row.Name))
	}

	if row.IsActive && s.hub != nil {
		s.hub.Bind(nil)
		_ = s.repo.ClearActive(ctx)
	}
	row, err = s.repo.SetStatus(ctx, row.ID, statusStopping, "")
	if err != nil {
		s.jobs.end(id)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	jobCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), provisionTimeout)
	jobCtx = s.withJobLogs(jobCtx, row.ID, "lifecycle")
	go func() {
		defer cancel()
		defer s.jobs.end(id)
		s.stopLocal(jobCtx, row)
	}()
	s.record(ctx, "cluster.stop", row.Name, "success", map[string]any{"queued": true})
	return s.toProto(row), nil
}

func (s *Service) stopLocal(ctx context.Context, row *db.Cluster) {
	s.logLine(row.ID, "lifecycle", fmt.Sprintf("stopping %s cluster %s", row.Provider, row.Name))
	var err error
	switch row.Provider {
	case kubernetes.ProviderKind:
		err = s.provisioner.StopKind(ctx, row.Name)
	case kubernetes.ProviderMinikube:
		err = s.provisioner.StopMinikube(ctx, row.Name)
	case kubernetes.ProviderImported:
		// Disconnect only. Destroying someone else's API server is not a stop.
	}
	if err != nil {
		s.logLine(row.ID, "lifecycle", "stop failed: "+err.Error())
		_, _ = s.repo.SetStatus(ctx, row.ID, statusError, err.Error())
		s.record(ctx, "cluster.stop", row.Name, "failure", map[string]any{"error": err.Error()})
		return
	}
	_, _ = s.repo.SetStatus(ctx, row.ID, statusStopped, "")
	s.logLine(row.ID, "lifecycle", "stopped")
	s.record(ctx, "cluster.stop", row.Name, "success", nil)
}

// RestartCluster starts a stopped local cluster, or re-pings an imported one.
// The start is asynchronous so the UI can show progress instead of waiting out
// a multi-minute minikube start.
func (s *Service) RestartCluster(ctx context.Context, req *idpv1.RestartClusterRequest) (*idpv1.ManagedCluster, error) {
	row, err := s.requireCluster(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if busyStatus(row.Status) && row.Status != statusStarting {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("cluster %s is %s", row.Name, row.Status))
	}
	if row.Provider == kubernetes.ProviderImported {
		return s.ActivateCluster(ctx, &idpv1.ActivateClusterRequest{Id: req.Id})
	}
	id := uuidString(row.ID)
	if !s.jobs.begin(id, "start") {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("cluster %s already has a lifecycle job running", row.Name))
	}

	row, err = s.repo.SetStatus(ctx, row.ID, statusStarting, "")
	if err != nil {
		s.jobs.end(id)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	jobCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), provisionTimeout)
	jobCtx = s.withJobLogs(jobCtx, row.ID, "lifecycle")
	go func() {
		defer cancel()
		defer s.jobs.end(id)
		s.restartLocal(jobCtx, row)
	}()
	s.record(ctx, "cluster.restart", row.Name, "success", map[string]any{"queued": true})
	return s.toProto(row), nil
}

func (s *Service) restartLocal(ctx context.Context, row *db.Cluster) {
	s.logLine(row.ID, "lifecycle", fmt.Sprintf("starting %s cluster %s", row.Provider, row.Name))
	var (
		kubeconfig []byte
		server     string
		err        error
	)
	switch row.Provider {
	case kubernetes.ProviderKind:
		if row.Status == statusRunning {
			if stopErr := s.provisioner.StopKind(ctx, row.Name); stopErr != nil && s.logger != nil {
				s.logger.Warn("kind stop before restart returned an error", zap.Error(stopErr))
			}
		}
		kubeconfig, server, err = s.provisioner.StartKind(ctx, row.Name)
	case kubernetes.ProviderMinikube:
		kubeconfig, server, err = s.provisioner.StartMinikube(ctx, row.Name)
		if err == nil {
			s.enableLocalAddons(ctx, row.Name)
		}
	default:
		err = fmt.Errorf("unknown provider %s", row.Provider)
	}
	if err != nil {
		s.logLine(row.ID, "lifecycle", "start failed: "+err.Error())
		_, _ = s.repo.SetStatus(ctx, row.ID, statusError, err.Error())
		s.record(ctx, "cluster.restart", row.Name, "failure", map[string]any{"error": err.Error()})
		return
	}

	sealed, _ := s.sealKubeconfig(kubeconfig)
	client, err := kubernetes.NewClientFromKubeconfig(kubeconfig, row.Name, s.k8sCfg)
	if err != nil {
		s.logLine(row.ID, "lifecycle", "kubeconfig unusable: "+err.Error())
		_, _ = s.repo.SetStatus(ctx, row.ID, statusError, err.Error())
		return
	}
	version, nodes := clusterFacts(ctx, client)
	updated, err := s.repo.UpdateRuntime(ctx, repository.UpdateRuntimeInput{
		ID:                  row.ID,
		KubeconfigEncrypted: sealed,
		ServerURL:           server,
		KubernetesVersion:   version,
		NodeCount:           nodes,
		Status:              statusRunning,
	})
	if err != nil {
		s.logLine(row.ID, "lifecycle", "failed to save cluster row: "+err.Error())
		return
	}
	if updated.IsActive || s.hub == nil || !s.hub.Live().Available() {
		if err := s.bindActive(ctx, updated, client); err != nil {
			s.logLine(row.ID, "lifecycle", "activate after start failed: "+err.Error())
		} else {
			s.logLine(row.ID, "lifecycle", "activated as the platform cluster")
		}
	}
	s.logLine(row.ID, "lifecycle", "started")
	s.record(ctx, "cluster.restart", row.Name, "success", nil)
}

// DeleteCluster unregisters a cluster. Kind/minikube clusters are destroyed;
// imported clusters are only removed from the platform. The destroy runs in
// the background so the RPC is not held open for minutes.
func (s *Service) DeleteCluster(ctx context.Context, req *idpv1.DeleteClusterRequest) (*idpv1.DeleteClusterResponse, error) {
	row, err := s.requireCluster(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	id := uuidString(row.ID)
	if !s.jobs.begin(id, "delete") {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("cluster %s already has a lifecycle job running", row.Name))
	}
	if _, err := s.repo.SetStatus(ctx, row.ID, statusDeleting, ""); err != nil {
		s.jobs.end(id)
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if row.IsActive && s.hub != nil {
		s.hub.Bind(nil)
		_ = s.repo.ClearActive(ctx)
	}

	jobCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), provisionTimeout)
	jobCtx = s.withJobLogs(jobCtx, row.ID, "lifecycle")
	go func() {
		defer cancel()
		defer s.jobs.end(id)
		s.deleteLocal(jobCtx, row)
	}()
	s.record(ctx, "cluster.delete", row.Name, "success", map[string]any{"queued": true})
	return &idpv1.DeleteClusterResponse{}, nil
}

func (s *Service) deleteLocal(ctx context.Context, row *db.Cluster) {
	s.logLine(row.ID, "lifecycle", fmt.Sprintf("deleting %s cluster %s", row.Provider, row.Name))

	var err error
	switch row.Provider {
	case kubernetes.ProviderKind:
		err = s.provisioner.DeleteKind(ctx, row.Name)
		if err == nil {
			if exists, existsErr := s.provisioner.KindClusterExists(ctx, row.Name); existsErr == nil && exists {
				err = fmt.Errorf("kind cluster %s still exists after delete", row.Name)
			}
		}
	case kubernetes.ProviderMinikube:
		err = s.provisioner.DeleteMinikube(ctx, row.Name)
		if err == nil {
			if exists, existsErr := s.provisioner.MinikubeProfileExists(ctx, row.Name); existsErr == nil && exists {
				err = fmt.Errorf("minikube profile %s still exists after delete", row.Name)
			}
		}
	}
	if err != nil {
		s.logLine(row.ID, "lifecycle", "delete failed: "+err.Error())
		if s.logger != nil {
			s.logger.Error("cluster delete failed; keeping the fleet row", zap.String("name", row.Name), zap.Error(err))
		}
		_, _ = s.repo.SetStatus(ctx, row.ID, statusError, err.Error())
		s.record(ctx, "cluster.delete", row.Name, "failure", map[string]any{"error": err.Error()})
		return
	}

	s.retireDeletedClusterState(ctx, row)
	if err := s.repo.Delete(ctx, row.ID); err != nil {
		s.logLine(row.ID, "lifecycle", "failed to remove fleet row: "+err.Error())
		_, _ = s.repo.SetStatus(ctx, row.ID, statusError, err.Error())
		return
	}
	s.logLine(row.ID, "lifecycle", "removed from the platform")
	s.record(ctx, "cluster.delete", row.Name, "success", map[string]any{"provider": row.Provider})
	if row.IsActive {
		s.failoverActive(ctx)
	}
}

func (s *Service) retireDeletedClusterState(ctx context.Context, row *db.Cluster) {
	if s.state == nil {
		return
	}
	var retired []string
	if row.ClusterUid != nil && *row.ClusterUid != "" {
		names, err := s.state.RetireCluster(ctx, *row.ClusterUid)
		if err != nil && s.logger != nil {
			s.logger.Error("could not retire namespaces of deleted cluster", zap.Error(err))
		} else {
			retired = append(retired, names...)
		}
	}
	if row.IsActive {
		names, err := s.state.RetireUnprovenanced(ctx)
		if err != nil && s.logger != nil {
			s.logger.Error("could not retire unprovenanced namespaces", zap.Error(err))
		} else {
			retired = append(retired, names...)
		}
	}
	for _, name := range retired {
		s.logLine(row.ID, "lifecycle", "retired namespace record "+name)
	}
}

func uuidString(id pgtype.UUID) string {
	return fmt.Sprintf("%x", id.Bytes)
}

// Bootstrap registers the env kubeconfig as the default fleet cluster when
// the table is empty, and rebinds the hub to the stored active cluster.
func (s *Service) Bootstrap(ctx context.Context) {
	if s.repo == nil || s.hub == nil {
		return
	}

	// A kubeconfig on disk is not a live cluster. Probing first — and
	// disconnecting on failure — is what stops every page from hanging on a
	// stopped minikube for the full Kubernetes request timeout.
	if live := s.hub.Live(); live != nil && live.Bound() {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := live.Ping(pingCtx)
		cancel()
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("environment cluster is not reachable; starting disconnected", zap.Error(err))
			}
			s.hub.Bind(nil)
		}
	}

	if err := s.seedDefault(ctx); err != nil && s.logger != nil {
		s.logger.Warn("cluster fleet seed skipped", zap.Error(err))
	}
	s.repairProviders(ctx)
	active, err := s.repo.GetActive(ctx)
	if err != nil || active == nil {
		return
	}
	if s.hub.Live() != nil && s.hub.Live().Available() {
		s.adoptIdentity(ctx, active, s.hub.Live())
		if s.logger != nil {
			s.logger.Info("active cluster restored", zap.String("name", active.Name), zap.String("provider", active.Provider))
		}
		return
	}
	client, err := s.clientFor(ctx, active)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("could not restore active cluster", zap.String("name", active.Name), zap.Error(err))
		}
		return
	}
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	err = client.Ping(pingCtx)
	cancel()
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("active cluster is not reachable", zap.String("name", active.Name), zap.Error(err))
		}
		if active.Status == statusRunning {
			_, _ = s.repo.SetStatus(ctx, active.ID, statusStopped, "cluster is not reachable")
		}
		return
	}
	s.hub.Bind(client)
	s.adoptIdentity(ctx, active, client)
	if s.logger != nil {
		s.logger.Info("active cluster restored", zap.String("name", active.Name), zap.String("provider", active.Provider))
	}
}

func (s *Service) seedDefault(ctx context.Context) error {
	n, err := s.repo.Count(ctx)
	if err != nil || n > 0 {
		return err
	}
	if s.hub == nil || !s.hub.Live().Available() {
		return nil
	}
	raw, err := readEnvKubeconfig(s.k8sCfg)
	if err != nil {
		raw = nil
	}
	var server string
	if len(raw) > 0 {
		server, _ = kubernetes.ValidateKubeconfig(raw)
	}

	contextName := s.hub.Live().Name
	// Recording a local cluster as "imported" makes the platform treat it as
	// someone else's compute: stop only disconnects, delete only drops the
	// database row, and restart only re-pings instead of starting the cluster.
	// That is why deleting the seeded cluster from the UI left the real
	// minikube running with all its old namespaces, and why start/stop from the
	// UI did nothing. Detect the owning tool so the lifecycle actually applies.
	provider := kubernetes.ProviderFromKubeconfig(raw, contextName)
	name := kubernetes.ProfileFromContext(provider, contextName)
	if err := validateClusterName(name); err != nil {
		name = "default"
		provider = kubernetes.ProviderImported
	}

	// A kind or minikube cluster's kubeconfig is regenerated by its own tool on
	// every start, so storing a copy would only create a stale one to fall back
	// to. Imported clusters have no such source and must keep theirs.
	var sealed []byte
	if provider == kubernetes.ProviderImported && len(raw) > 0 {
		sealed, _ = s.sealKubeconfig(raw)
	}

	version, nodes := clusterFacts(ctx, s.hub.Live())
	row, err := s.repo.Create(ctx, repository.CreateClusterInput{
		Name:                name,
		DisplayName:         name,
		Provider:            provider,
		Status:              statusRunning,
		KubeconfigEncrypted: sealed,
		ServerURL:           server,
		KubernetesVersion:   version,
		NodeCount:           nodes,
		Active:              true,
		CreatedBy:           "system",
	})
	if err != nil {
		return err
	}
	if s.logger != nil {
		s.logger.Info("registered the environment cluster in the fleet",
			zap.String("name", name), zap.String("provider", provider))
	}
	s.adoptIdentity(ctx, row, s.hub.Live())
	return nil
}

func (s *Service) enableLocalAddons(ctx context.Context, profile string) {
	if s.provisioner == nil {
		return
	}
	if err := s.provisioner.EnableMinikubeAddon(ctx, profile, "metrics-server"); err != nil {
		s.logLineByName(profile, "lifecycle", "metrics-server addon: "+err.Error())
		if s.logger != nil {
			s.logger.Warn("could not enable metrics-server; HPA will be unavailable until it is installed",
				zap.String("cluster", profile), zap.Error(err))
		}
		return
	}
	s.logLineByName(profile, "lifecycle", "enabled metrics-server addon (required for CPU/memory autoscaling)")
}

func (s *Service) logLineByName(name, source, message string) {
	if s.repo == nil {
		return
	}
	row, err := s.repo.GetByName(context.Background(), name)
	if err != nil {
		return
	}
	s.logLine(row.ID, source, message)
}

func (s *Service) failoverActive(ctx context.Context) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return
	}
	for i := range rows {
		if rows[i].Status != statusRunning {
			continue
		}
		client, err := s.clientFor(ctx, &rows[i])
		if err != nil {
			continue
		}
		if err := client.Ping(ctx); err != nil {
			continue
		}
		_ = s.bindActive(ctx, &rows[i], client)
		return
	}
}

func (s *Service) bindActive(ctx context.Context, row *db.Cluster, client *kubernetes.Client) error {
	if s.hub == nil {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("cluster hub is not configured"))
	}
	if _, err := s.repo.SetActive(ctx, row.ID); err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}
	s.hub.Bind(client)
	// Identity is checked after the bind so the reconciler reads through the
	// same client the platform will serve from, and every path that makes a
	// cluster active — bootstrap, activate, restart, provision, failover —
	// gets the rebuild check without having to remember to ask for it.
	s.adoptIdentity(ctx, row, client)
	return nil
}

func (s *Service) clientFor(ctx context.Context, row *db.Cluster) (*kubernetes.Client, error) {
	raw, err := s.openKubeconfig(ctx, row)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewClientFromKubeconfig(raw, row.Name, s.k8sCfg)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return client, nil
}

// openKubeconfig resolves the kubeconfig to reach a fleet cluster.
//
// For kind and minikube the live tool is authoritative and a stored copy is
// only a fallback. Both hand out a fresh API server host port on every start,
// and regenerate the cluster CA on recreate, so a kubeconfig captured at
// provision time stops working the first time the cluster is restarted — which
// is why the platform reported a perfectly healthy local cluster as
// unreachable. Imported clusters have no local tool to ask, so the stored copy
// is all there is.
func (s *Service) openKubeconfig(ctx context.Context, row *db.Cluster) ([]byte, error) {
	switch row.Provider {
	case kubernetes.ProviderKind:
		kc, _, err := s.provisioner.KindKubeconfig(ctx, row.Name)
		if err == nil {
			return kc, nil
		}
		return s.storedKubeconfig(row, err)
	case kubernetes.ProviderMinikube:
		kc, _, err := s.provisioner.MinikubeKubeconfig(ctx, row.Name)
		if err == nil {
			return kc, nil
		}
		return s.storedKubeconfig(row, err)
	default:
		if raw, err := s.storedKubeconfig(row, nil); err == nil {
			return raw, nil
		}
		raw, err := readEnvKubeconfig(s.k8sCfg)
		if err != nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("no stored kubeconfig for %s", row.Name))
		}
		return raw, nil
	}
}

// storedKubeconfig decrypts the saved kubeconfig. liveErr, when non-nil, is the
// reason the authoritative source could not be reached and is reported instead
// of a bare "no stored kubeconfig" if there is nothing to fall back to.
func (s *Service) storedKubeconfig(row *db.Cluster, liveErr error) ([]byte, error) {
	if len(row.KubeconfigEncrypted) == 0 {
		if liveErr != nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, liveErr)
		}
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("no stored kubeconfig for %s", row.Name))
	}
	if s.box == nil || !s.box.Enabled() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("IDP_ENCRYPTION_KEY is required to open stored kubeconfig"))
	}
	plain, err := s.box.Decrypt(row.KubeconfigEncrypted)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("decrypt kubeconfig: %w", err))
	}
	if liveErr != nil && s.logger != nil {
		s.logger.Warn("falling back to the stored kubeconfig",
			zap.String("cluster", row.Name), zap.Error(liveErr))
	}
	return []byte(plain), nil
}

func (s *Service) sealKubeconfig(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, nil
	}
	if s.box == nil || !s.box.Enabled() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("IDP_ENCRYPTION_KEY is required to store cluster credentials"))
	}
	sealed, err := s.box.Encrypt(string(plain))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return sealed, nil
}

func (s *Service) requireCluster(ctx context.Context, id string) (*db.Cluster, error) {
	if s.repo == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("cluster fleet is not configured"))
	}
	parsed, err := parseUUID(id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("id is required"))
	}
	row, err := s.repo.Get(ctx, parsed)
	if err != nil {
		if errors.Is(err, repository.ErrClusterNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("cluster not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return row, nil
}

func (s *Service) toProto(row *db.Cluster) *idpv1.ManagedCluster {
	connected := false
	if row.IsActive && s.k8s != nil && s.k8s.Available() {
		connected = true
	}
	return convert.ManagedClusterToProto(row, connected)
}

func (s *Service) record(ctx context.Context, action, resource, result string, details map[string]any) {
	if s.audit == nil {
		return
	}
	s.audit.RecordFromUser(ctx, action, "", resource, "cluster", result, details)
}

func validateClusterName(name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if len(name) > maxClusterNameLen {
		return fmt.Errorf("name must be at most %d characters", maxClusterNameLen)
	}
	if !clusterNameRegex.MatchString(name) {
		return fmt.Errorf("name must be a lowercase DNS label (a-z, 0-9, hyphens)")
	}
	return nil
}

func clusterFacts(ctx context.Context, client *kubernetes.Client) (version string, nodes int32) {
	if client == nil || client.Clientset == nil {
		return "", 0
	}
	sv, err := client.Clientset.Discovery().ServerVersion()
	if err == nil && sv != nil {
		version = sv.GitVersion
	}
	list, err := client.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err == nil {
		nodes = int32(len(list.Items))
	}
	return version, nodes
}

func parseUUID(raw string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(strings.TrimSpace(raw)); err != nil {
		return pgtype.UUID{}, err
	}
	return id, nil
}

func readEnvKubeconfig(cfg config.KubernetesConfig) ([]byte, error) {
	path := cfg.Kubeconfig
	if path == "" {
		if env := os.Getenv("KUBECONFIG"); env != "" {
			path = env
		} else if home := homedir.HomeDir(); home != "" {
			path = filepath.Join(home, ".kube", "config")
		}
	}
	if path == "" {
		return nil, fmt.Errorf("no kubeconfig path")
	}
	return os.ReadFile(path)
}
