package dbbrowse

import (
	"context"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/idp/platform/backend/internal/auth"
	"github.com/idp/platform/backend/internal/dbadmin"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
	"github.com/idp/platform/backend/internal/kubernetes"
)

// Service exposes discovered database workloads for the UI.
type Service struct {
	k8s *kubernetes.Client
}

// NewService creates a database browser service. A nil or unbound client means
// no cluster is reachable; List degrades to connected=false rather than panicking.
func NewService(k8s *kubernetes.Client) *Service {
	return &Service{k8s: k8s}
}

// liveDiscoverer reads the clientset at call time. Caching it at construction
// left this page disconnected after the fleet rebound the live cluster — the
// sidebar showed "development" while ListDatabases still reported connected=false.
func (s *Service) liveDiscoverer() *dbadmin.Discoverer {
	if s == nil || s.k8s == nil || !s.k8s.Available() {
		return nil
	}
	return dbadmin.NewDiscoverer(s.k8s.Clientset)
}

func (s *Service) requireCluster(ctx context.Context) error {
	if _, err := auth.UserFromContext(ctx); err != nil {
		return connect.NewError(connect.CodeUnauthenticated, err)
	}
	if s.liveDiscoverer() == nil {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("kubernetes cluster not connected"))
	}
	return nil
}

// ListDatabases returns every Mongo / Postgres / MySQL workload found.
func (s *Service) ListDatabases(
	ctx context.Context,
	req *idpv1.ListDatabasesRequest,
) (*idpv1.ListDatabasesResponse, error) {
	if _, err := auth.UserFromContext(ctx); err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	discoverer := s.liveDiscoverer()
	if discoverer == nil {
		return &idpv1.ListDatabasesResponse{Connected: false}, nil
	}

	instances, err := discoverer.List(ctx, req.Namespace)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := make([]*idpv1.DatabaseInstance, 0, len(instances))
	for i := range instances {
		out = append(out, toProtoInstance(&instances[i]))
	}
	return &idpv1.ListDatabasesResponse{Connected: true, Instances: out}, nil
}

// InspectDatabase reports collections/tables for one workload.
func (s *Service) InspectDatabase(
	ctx context.Context,
	req *idpv1.InspectDatabaseRequest,
) (*idpv1.InspectDatabaseResponse, error) {
	if err := s.requireCluster(ctx); err != nil {
		return nil, err
	}
	if req.Namespace == "" || req.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace and name are required"))
	}

	instance, creds, host, port, closer, err := s.dial(ctx, req.Namespace, req.Name)
	if err != nil {
		return nil, err
	}
	defer closer()

	overview, err := dbadmin.Inspect(ctx, instance.Engine, creds, host, port)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("inspect: %w", err))
	}

	tables := make([]*idpv1.DatabaseTable, 0, len(overview.Tables))
	for _, t := range overview.Tables {
		tables = append(tables, &idpv1.DatabaseTable{
			Schema:      t.Schema,
			Name:        t.Name,
			RowEstimate: t.RowEstimate,
			SizeBytes:   t.SizeBytes,
		})
	}

	return &idpv1.InspectDatabaseResponse{
		Engine:            string(overview.Engine),
		Database:          overview.Database,
		Version:           overview.Version,
		TableCount:        overview.TableCount,
		SchemaCount:       overview.SchemaCount,
		SizeBytes:         overview.SizeBytes,
		ActiveConnections: overview.ActiveConnections,
		Tables:            tables,
		TablesTruncated:   overview.TablesTruncated,
		InspectedAt:       overview.InspectedAt.UTC().Format(time.RFC3339),
	}, nil
}

// QueryDocuments returns a capped page of JSON documents/rows.
func (s *Service) QueryDocuments(
	ctx context.Context,
	req *idpv1.QueryDocumentsRequest,
) (*idpv1.QueryDocumentsResponse, error) {
	if err := s.requireCluster(ctx); err != nil {
		return nil, err
	}
	if req.Namespace == "" || req.Name == "" || req.Table == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace, name and table are required"))
	}

	instance, creds, host, port, closer, err := s.dial(ctx, req.Namespace, req.Name)
	if err != nil {
		return nil, err
	}
	defer closer()

	result, err := dbadmin.QueryDocuments(
		ctx,
		instance.Engine,
		creds,
		host,
		port,
		req.Schema,
		req.Table,
		req.Limit,
		req.Skip,
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("query: %w", err))
	}

	return &idpv1.QueryDocumentsResponse{
		Documents: result.Documents,
		Returned:  result.Returned,
		Limit:     result.Limit,
		Skip:      result.Skip,
		Truncated: result.Truncated,
	}, nil
}

// ExportDatabase produces a dump archive via tools inside the database pod.
func (s *Service) ExportDatabase(
	ctx context.Context,
	req *idpv1.ExportDatabaseRequest,
) (*idpv1.ExportDatabaseResponse, error) {
	if err := s.requireCluster(ctx); err != nil {
		return nil, err
	}
	if req.Namespace == "" || req.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace and name are required"))
	}

	instance, creds, err := s.resolveReady(ctx, req.Namespace, req.Name)
	if err != nil {
		return nil, err
	}

	archive, filename, contentType, err := dbadmin.DumpArchive(ctx, instance.Engine, creds, s.execFn(instance))
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}

	return &idpv1.ExportDatabaseResponse{
		Archive:     archive,
		Filename:    fmt.Sprintf("%s-%s", req.Name, filename),
		ContentType: contentType,
		SizeBytes:   int64(len(archive)),
	}, nil
}

// ImportDatabase restores an archive into the database pod.
func (s *Service) ImportDatabase(
	ctx context.Context,
	req *idpv1.ImportDatabaseRequest,
) (*idpv1.ImportDatabaseResponse, error) {
	if err := s.requireCluster(ctx); err != nil {
		return nil, err
	}
	if req.Namespace == "" || req.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace and name are required"))
	}
	if len(req.Archive) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("archive is required"))
	}

	instance, creds, err := s.resolveReady(ctx, req.Namespace, req.Name)
	if err != nil {
		return nil, err
	}

	if err := dbadmin.RestoreArchive(ctx, instance.Engine, creds, req.Archive, s.execFn(instance)); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}

	return &idpv1.ImportDatabaseResponse{
		Message:   "import completed",
		SizeBytes: int64(len(req.Archive)),
	}, nil
}

// EnsureDatabasePersistence creates a PVC and mounts it on the Deployment.
func (s *Service) EnsureDatabasePersistence(
	ctx context.Context,
	req *idpv1.EnsureDatabasePersistenceRequest,
) (*idpv1.EnsureDatabasePersistenceResponse, error) {
	if err := s.requireCluster(ctx); err != nil {
		return nil, err
	}
	if req.Namespace == "" || req.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace and name are required"))
	}

	discoverer := s.liveDiscoverer()
	if discoverer == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("kubernetes cluster not connected"))
	}
	instance, err := discoverer.Get(ctx, req.Namespace, req.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	spec, ok := dbadmin.SpecFor(instance.Engine)
	if !ok || spec.DataDir == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("engine has no data directory"))
	}

	size := strings.TrimSpace(req.StorageSize)
	if size == "" {
		size = dbadmin.DefaultPVCSize
	}
	pvcName := dbadmin.WorkloadPVCName(instance.Name)

	if err := s.k8s.EnsurePVC(ctx, req.Namespace, pvcName, size, map[string]string{
		"app": instance.Name,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create pvc: %w", err))
	}

	patched, err := s.k8s.AttachPVCToDeployment(ctx, req.Namespace, instance.Name, pvcName, spec.DataDir)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("attach pvc: %w", err))
	}

	msg := "PVC ready. Export data first if the volume was empty — attaching persistence recreates the pod."
	if !patched {
		msg = "Persistence already configured."
	}

	return &idpv1.EnsureDatabasePersistenceResponse{
		PvcName:     pvcName,
		MountPath:   spec.DataDir,
		StorageSize: size,
		Patched:     patched,
		Message:     msg,
	}, nil
}

func (s *Service) resolveReady(
	ctx context.Context,
	namespace, name string,
) (*dbadmin.Instance, dbadmin.Credentials, error) {
	discoverer := s.liveDiscoverer()
	if discoverer == nil {
		return nil, dbadmin.Credentials{}, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("kubernetes cluster not connected"))
	}
	instance, err := discoverer.Get(ctx, namespace, name)
	if err != nil {
		return nil, dbadmin.Credentials{}, connect.NewError(connect.CodeNotFound, err)
	}
	if !instance.Ready {
		return nil, dbadmin.Credentials{}, connect.NewError(
			connect.CodeFailedPrecondition,
			fmt.Errorf("database %s/%s is not ready", namespace, name),
		)
	}
	ref := dbadmin.Ref{
		Namespace: instance.Namespace,
		PodName:   instance.PodName,
		Container: instance.Container,
		Port:      instance.Port,
	}
	creds, err := discoverer.Credentials(ctx, ref, instance.Engine)
	if err != nil {
		return nil, dbadmin.Credentials{}, connect.NewError(
			connect.CodeFailedPrecondition,
			fmt.Errorf("credentials: %w", err),
		)
	}
	return instance, creds, nil
}

func (s *Service) execFn(instance *dbadmin.Instance) dbadmin.ExecFunc {
	return func(ctx context.Context, command []string, stdin []byte) ([]byte, []byte, error) {
		result, err := s.k8s.ExecInPod(ctx, instance.Namespace, instance.PodName, instance.Container, command, stdin)
		if result == nil {
			return nil, nil, err
		}
		return result.Stdout, result.Stderr, err
	}
}

// dial resolves an instance, opens a localhost port-forward, and returns the
// local host/port plus a cleanup function. Credentials never leave the service.
func (s *Service) dial(
	ctx context.Context,
	namespace, name string,
) (*dbadmin.Instance, dbadmin.Credentials, string, int32, func(), error) {
	discoverer := s.liveDiscoverer()
	if discoverer == nil {
		return nil, dbadmin.Credentials{}, "", 0, nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("kubernetes cluster not connected"))
	}
	instance, err := discoverer.Get(ctx, namespace, name)
	if err != nil {
		return nil, dbadmin.Credentials{}, "", 0, nil, connect.NewError(connect.CodeNotFound, err)
	}
	if !instance.Ready {
		return nil, dbadmin.Credentials{}, "", 0, nil, connect.NewError(
			connect.CodeFailedPrecondition,
			fmt.Errorf("database %s/%s is not ready", namespace, name),
		)
	}

	ref := dbadmin.Ref{
		Namespace: instance.Namespace,
		PodName:   instance.PodName,
		Container: instance.Container,
		Port:      instance.Port,
	}
	creds, err := discoverer.Credentials(ctx, ref, instance.Engine)
	if err != nil {
		return nil, dbadmin.Credentials{}, "", 0, nil, connect.NewError(
			connect.CodeFailedPrecondition,
			fmt.Errorf("credentials: %w", err),
		)
	}

	fwd, err := s.k8s.PortForwardPod(ctx, instance.Namespace, instance.PodName, instance.Port)
	if err != nil {
		return nil, dbadmin.Credentials{}, "", 0, nil, connect.NewError(
			connect.CodeUnavailable,
			fmt.Errorf("port-forward: %w", err),
		)
	}

	return instance, creds, "127.0.0.1", int32(fwd.LocalPort), fwd.Close, nil
}

func toProtoInstance(in *dbadmin.Instance) *idpv1.DatabaseInstance {
	return &idpv1.DatabaseInstance{
		Namespace:              in.Namespace,
		Name:                   in.Name,
		Engine:                 string(in.Engine),
		EngineName:             in.EngineName,
		Image:                  in.Image,
		PodName:                in.PodName,
		Container:              in.Container,
		Port:                   in.Port,
		Ready:                  in.Ready,
		ServiceName:            in.ServiceName,
		PersistentVolumeClaims: in.PersistentVolumeClaims,
		CredentialsResolved:    in.CredentialsResolved,
		CredentialsHint:        in.CredentialsHint,
	}
}
