package deployment

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"connectrpc.com/connect"
	"github.com/idp/platform/backend/internal/audit"
	"github.com/idp/platform/backend/internal/auth"
	"github.com/idp/platform/backend/internal/dbadmin"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
	"github.com/idp/platform/backend/internal/kubernetes"
	"github.com/idp/platform/backend/internal/pkg/convert"
	"github.com/idp/platform/backend/internal/pkg/pagination"
	"github.com/idp/platform/backend/internal/repository"
)

var resourceNameRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// PullSecretResolver materialises the registry credentials a namespace should
// pull with and returns their Secret names. It is an interface so the
// deployment package does not depend on credential storage details, and so
// tests can deploy without a database or cluster.
type PullSecretResolver interface {
	EnsureNamespacePullSecrets(ctx context.Context, namespace string) ([]string, error)
	// ProjectSlugForNamespace scopes the generated ingress hostname. Returns ""
	// for a namespace with no project, in which case the namespace name is used
	// so the workload still gets a valid URL.
	ProjectSlugForNamespace(ctx context.Context, namespace string) string
}

// Service handles deployment business logic.
type Service struct {
	k8s         *kubernetes.Client
	namespaces  *repository.NamespaceRepository
	pullSecrets PullSecretResolver
	audit       *audit.Service
}

// NewService creates a deployment service.
func NewService(
	k8s *kubernetes.Client,
	namespaces *repository.NamespaceRepository,
	pullSecrets PullSecretResolver,
	auditSvc *audit.Service,
) *Service {
	return &Service{k8s: k8s, namespaces: namespaces, pullSecrets: pullSecrets, audit: auditSvc}
}

func (s *Service) requireK8s() error {
	if s.k8s == nil {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("kubernetes cluster not connected"))
	}
	return nil
}

// authorizeNamespace resolves the caller and confirms they may act on the given
// namespace. Without this every operation trusts the namespace supplied by the
// client, letting any authenticated user reach another tenant's workloads — or
// cluster namespaces such as kube-system. A namespace the caller cannot see is
// reported as NotFound so the error does not confirm it exists.
func (s *Service) authorizeNamespace(ctx context.Context, namespace string, needWrite bool) (*auth.User, error) {
	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	if needWrite && !user.CanWrite() {
		return nil, connect.NewError(connect.CodePermissionDenied, auth.ErrInsufficientPermissions)
	}

	if strings.TrimSpace(namespace) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace is required"))
	}

	// Admins may act cluster-wide, but the namespace must still be one the
	// platform manages so the API cannot be used to reach arbitrary namespaces.
	ns, err := s.namespaces.GetByName(ctx, namespace)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("namespace not found"))
	}

	if !user.IsAdmin() && ns.OwnerEmail != user.Email {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("namespace not found"))
	}

	return user, nil
}

// resolvePullSecrets returns the imagePullSecrets for a namespace. A namespace
// with no project, or a project with no credentials, yields none — public
// images must keep deploying with nothing configured.
func (s *Service) resolvePullSecrets(ctx context.Context, namespace string) ([]string, error) {
	if s.pullSecrets == nil {
		return nil, nil
	}
	return s.pullSecrets.EnsureNamespacePullSecrets(ctx, namespace)
}

// probeFromProto maps the wire form to the platform spec. A nil message is an
// unconfigured probe, which BuildProbe omits from the pod spec entirely.
func probeFromProto(p *idpv1.Probe) kubernetes.ProbeSpec {
	if p == nil {
		return kubernetes.ProbeSpec{}
	}
	return kubernetes.ProbeSpec{
		Path:                strings.TrimSpace(p.Path),
		Port:                p.Port,
		InitialDelaySeconds: p.InitialDelaySeconds,
		TimeoutSeconds:      p.TimeoutSeconds,
		PeriodSeconds:       p.PeriodSeconds,
		FailureThreshold:    p.FailureThreshold,
	}
}

// resourcesFromProto maps the wire form to the platform spec. A nil message
// leaves every field empty, which omits resources from the container.
func resourcesFromProto(r *idpv1.ResourceLimits) kubernetes.ResourceSpec {
	if r == nil {
		return kubernetes.ResourceSpec{}
	}
	return kubernetes.ResourceSpec{
		CPURequest:    strings.TrimSpace(r.CpuRequest),
		CPULimit:      strings.TrimSpace(r.CpuLimit),
		MemoryRequest: strings.TrimSpace(r.MemoryRequest),
		MemoryLimit:   strings.TrimSpace(r.MemoryLimit),
	}
}

// ListTemplates returns the platform's golden paths. Authentication is required
// so the catalogue is not a public endpoint, but no namespace or project scope
// applies — templates are the same for everyone.
func (s *Service) ListTemplates(ctx context.Context, _ *idpv1.ListDeploymentTemplatesRequest) (*idpv1.ListDeploymentTemplatesResponse, error) {
	if _, err := auth.UserFromContext(ctx); err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	return &idpv1.ListDeploymentTemplatesResponse{Templates: Templates()}, nil
}

// resolveIngressHost decides the hostname a workload should be published under.
// Returns "" when no Ingress should exist, which is not an error.
func (s *Service) resolveIngressHost(ctx context.Context, namespace, app, custom string, disabled bool) (string, error) {
	if disabled || !s.k8s.Ingress.Enabled {
		return "", nil
	}

	// A custom hostname is taken verbatim so teams can front a workload with a
	// real domain, but it is still validated before it reaches the API server.
	if strings.TrimSpace(custom) != "" {
		return kubernetes.ValidateHostname(custom)
	}

	// Scope by project when there is one; fall back to the namespace so an
	// unattached namespace still produces a valid three-label hostname.
	scope := ""
	if s.pullSecrets != nil {
		scope = s.pullSecrets.ProjectSlugForNamespace(ctx, namespace)
	}
	if scope == "" {
		scope = namespace
	}

	return kubernetes.BuildIngressHost(app, scope, s.k8s.Ingress.Domain)
}

// Create creates a new deployment.
func (s *Service) Create(ctx context.Context, req *idpv1.CreateDeploymentRequest) (*idpv1.CreateDeploymentResponse, error) {
	if _, err := s.authorizeNamespace(ctx, req.Namespace, true); err != nil {
		return nil, err
	}
	if err := s.requireK8s(); err != nil {
		return nil, err
	}

	name := strings.ToLower(strings.TrimSpace(req.Name))
	if !resourceNameRegex.MatchString(name) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid deployment name"))
	}
	if strings.TrimSpace(req.Image) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("image is required"))
	}

	// env_vars is the pre-split field. Treating it as non-sensitive keeps older
	// clients working; anything genuinely secret must come via secret_vars.
	configVars := make(map[string]string)
	// Deprecated field read deliberately: existing clients still send
	// env_vars, and dropping it would silently discard their configuration.
	//nolint:staticcheck // SA1019: intentional backward compatibility.
	for _, kv := range req.EnvVars {
		configVars[kv.Key] = kv.Value
	}
	for _, kv := range req.ConfigVars {
		configVars[kv.Key] = kv.Value
	}

	secretVars := make(map[string]string)
	for _, kv := range req.SecretVars {
		secretVars[kv.Key] = kv.Value
	}

	// A name that collides across the two maps would end up defined twice in
	// envFrom, where the Secret silently wins — surface it instead.
	for key := range secretVars {
		if _, dup := configVars[key]; dup {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("%q is defined as both a config and a secret variable", key))
		}
	}
	if err := kubernetes.ValidateEnvMap(configVars, "config variables"); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := kubernetes.ValidateEnvMap(secretVars, "secret variables"); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	port := req.Port
	if port == 0 {
		port = kubernetes.DefaultContainerPort
	}
	if port < 1 || port > 65535 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("port must be between 1 and 65535"))
	}
	serviceType := kubernetes.NormalizeServiceType(req.ServiceType)

	resources := resourcesFromProto(req.Resources)
	if err := resources.Validate(); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	readiness := probeFromProto(req.ReadinessProbe)
	liveness := probeFromProto(req.LivenessProbe)
	if err := readiness.Validate("readiness"); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := liveness.Validate("liveness"); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	dbEngine, isDatabase := dbadmin.EngineForImage(req.Image)
	ingressDisabled := req.IngressDisabled || isDatabase

	// Resolved before anything is created so an invalid custom hostname fails
	// the request rather than leaving a workload with no route to it.
	ingressHost, err := s.resolveIngressHost(ctx, req.Namespace, name, req.Hostname, ingressDisabled)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// Resolve pull secrets before creating anything. A private image deployed
	// without them sits in ImagePullBackOff, so a credential problem is worth
	// failing the request over rather than shipping a broken workload.
	pullSecrets, pullErr := s.resolvePullSecrets(ctx, req.Namespace)
	if err := pullErr; err != nil {
		s.audit.RecordFromUser(ctx, "deployment.create", req.Namespace, name, "deployment", "failure", map[string]any{"error": err.Error()})
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}

	replicas := req.Replicas
	wantPVC := req.Persistent
	if isDatabase {
		// RWO database volumes cannot be shared across replicas.
		replicas = 1
		wantPVC = !req.DisablePersistence
	}

	pvcName := ""
	mountPath := ""
	storageSize := strings.TrimSpace(req.StorageSize)
	if storageSize == "" {
		storageSize = dbadmin.DefaultPVCSize
	}
	if wantPVC {
		if isDatabase {
			if spec, ok := dbadmin.SpecFor(dbEngine); ok {
				mountPath = spec.DataDir
			}
		} else if req.Persistent {
			// Non-DB persistence still needs a mount path; use a generic data dir.
			mountPath = "/data"
		}
		if mountPath == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("persistence requested but no data directory is known for this image"))
		}
		pvcName = dbadmin.WorkloadPVCName(name)
		if err := s.k8s.EnsurePVC(ctx, req.Namespace, pvcName, storageSize, map[string]string{
			"app": name,
		}); err != nil {
			s.audit.RecordFromUser(ctx, "deployment.create", req.Namespace, name, "deployment", "failure", map[string]any{"error": err.Error()})
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create pvc: %w", err))
		}
		if isDatabase && dbEngine == dbadmin.EnginePostgres {
			// Official postgres image refuses an empty volume at the default
			// data path unless PGDATA points at a subdirectory.
			if _, ok := configVars["PGDATA"]; !ok {
				configVars["PGDATA"] = mountPath + "/pgdata"
			}
		}
	}

	spec := kubernetes.DeploymentSpec{
		Namespace:             req.Namespace,
		Name:                  name,
		Image:                 req.Image,
		Replicas:              replicas,
		Port:                  port,
		ServiceType:           serviceType,
		ConfigVars:            configVars,
		SecretVars:            secretVars,
		Labels:                req.Labels,
		ImagePullSecrets:      pullSecrets,
		ReadinessProbe:        readiness,
		LivenessProbe:         liveness,
		Resources:             resources,
		PersistentVolumeClaim: pvcName,
		MountPath:             mountPath,
	}

	// Recorded as a label so it is visible in the cluster, not just in the
	// platform's own audit trail.
	if templateID := strings.TrimSpace(req.TemplateId); templateID != "" {
		if spec.Labels == nil {
			spec.Labels = map[string]string{}
		}
		spec.Labels["idp.platform/template"] = templateID
	}

	deployment, err := s.k8s.CreateDeployment(ctx, spec)
	if err != nil {
		if pvcName != "" {
			_ = s.k8s.DeletePVC(ctx, req.Namespace, pvcName)
		}
		s.audit.RecordFromUser(ctx, "deployment.create", req.Namespace, name, "deployment", "failure", map[string]any{"error": err.Error()})
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// A Deployment with no Service has no ClusterIP, DNS name or node port, so
	// nothing can reach its pods. Roll the Deployment back rather than leave an
	// unreachable workload behind reporting success.
	svc, err := s.k8s.CreateWorkloadService(ctx, kubernetes.WorkloadServiceSpec{
		Namespace:   req.Namespace,
		Name:        name,
		Port:        port,
		ServiceType: serviceType,
		Labels:      req.Labels,
	})
	if err != nil {
		_ = s.k8s.DeleteDeployment(ctx, req.Namespace, name)
		_ = s.k8s.DeleteWorkloadConfig(ctx, req.Namespace, name)
		if pvcName != "" {
			_ = s.k8s.DeletePVC(ctx, req.Namespace, pvcName)
		}
		s.audit.RecordFromUser(ctx, "deployment.create", req.Namespace, name, "deployment", "failure", map[string]any{"error": err.Error()})
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	deployment.ServiceType = svc.Type
	deployment.NodePort = svc.NodePort
	deployment.ClusterIP = svc.ClusterIP
	deployment.ClusterAddress = svc.ClusterAddress

	// The Ingress is best-effort: a cluster with no ingress controller, or one
	// that rejects the resource, should not cost the user a working workload
	// that is already reachable by ClusterIP or NodePort. The failure surfaces
	// as an absent URL plus an audit entry rather than a failed deployment.
	if ingressHost != "" {
		ingressErr := s.k8s.EnsureIngress(ctx, s.k8s.Ingress, kubernetes.IngressSpec{
			Namespace:   req.Namespace,
			Name:        name,
			Host:        ingressHost,
			ServicePort: port,
			Labels:      req.Labels,
		})
		if ingressErr != nil {
			s.audit.RecordFromUser(ctx, "deployment.ingress.create", req.Namespace, name, "ingress", "failure",
				map[string]any{"error": ingressErr.Error(), "host": ingressHost})
		} else {
			deployment.IngressHost = ingressHost
			deployment.URL = s.k8s.Ingress.Scheme() + "://" + ingressHost
		}
	}

	// Variable *names* are audited, never values: an audit log that recorded
	// DB_PASSWORD's value would recreate the exact leak this feature removes.
	s.audit.RecordFromUser(ctx, "deployment.create", req.Namespace, name, "deployment", "success", map[string]any{
		"image":        req.Image,
		"port":         port,
		"service_type": serviceType,
		"config_keys":  sortedKeys(configVars),
		"secret_keys":  sortedKeys(secretVars),
		"pvc":          pvcName,
		"database":     isDatabase,
	})
	return &idpv1.CreateDeploymentResponse{Deployment: convert.DeploymentToProto(deployment)}, nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func keyValues(m map[string]string) []*idpv1.KeyValue {
	keys := sortedKeys(m)
	out := make([]*idpv1.KeyValue, 0, len(keys))
	for _, k := range keys {
		out = append(out, &idpv1.KeyValue{Key: k, Value: m[k]})
	}
	return out
}

// ListRollouts returns a deployment's revision history, newest first.
func (s *Service) ListRollouts(ctx context.Context, req *idpv1.ListRolloutsRequest) (*idpv1.ListRolloutsResponse, error) {
	if _, err := s.authorizeNamespace(ctx, req.Namespace, false); err != nil {
		return nil, err
	}
	if err := s.requireK8s(); err != nil {
		return nil, err
	}

	name := strings.ToLower(strings.TrimSpace(req.Name))
	if !resourceNameRegex.MatchString(name) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid deployment name"))
	}

	rollouts, err := s.k8s.ListRollouts(ctx, req.Namespace, name)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("deployment not found"))
	}

	out := make([]*idpv1.Rollout, 0, len(rollouts))
	for i := range rollouts {
		out = append(out, convert.RolloutToProto(rollouts[i]))
	}
	return &idpv1.ListRolloutsResponse{Rollouts: out}, nil
}

// Rollback restores a previous revision, equivalent to `kubectl rollout undo`.
func (s *Service) Rollback(ctx context.Context, req *idpv1.RollbackDeploymentRequest) (*idpv1.RollbackDeploymentResponse, error) {
	if _, err := s.authorizeNamespace(ctx, req.Namespace, true); err != nil {
		return nil, err
	}
	if err := s.requireK8s(); err != nil {
		return nil, err
	}

	name := strings.ToLower(strings.TrimSpace(req.Name))
	if !resourceNameRegex.MatchString(name) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid deployment name"))
	}
	if req.Revision < 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("revision must not be negative"))
	}

	revision, err := s.k8s.RollbackDeployment(ctx, req.Namespace, name, req.Revision)
	if err != nil {
		s.audit.RecordFromUser(ctx, "deployment.rollback", req.Namespace, name, "deployment", "failure",
			map[string]any{"error": err.Error(), "requested_revision": req.Revision})
		// "no previous revision", "revision not found" and "already active" are
		// all the caller asking for something that cannot be done, not server
		// faults — FailedPrecondition keeps them out of the 5xx bucket.
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}

	deployment, err := s.k8s.GetDeployment(ctx, req.Namespace, name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.audit.RecordFromUser(ctx, "deployment.rollback", req.Namespace, name, "deployment", "success",
		map[string]any{"revision": revision, "image": deployment.Image})

	return &idpv1.RollbackDeploymentResponse{
		Deployment: convert.DeploymentToProto(deployment),
		Revision:   revision,
	}, nil
}

// GetConfig returns a deployment's configuration: non-sensitive values in
// full, sensitive variables by name only.
func (s *Service) GetConfig(ctx context.Context, req *idpv1.GetDeploymentConfigRequest) (*idpv1.GetDeploymentConfigResponse, error) {
	if _, err := s.authorizeNamespace(ctx, req.Namespace, false); err != nil {
		return nil, err
	}
	if err := s.requireK8s(); err != nil {
		return nil, err
	}

	name := strings.ToLower(strings.TrimSpace(req.Name))
	if !resourceNameRegex.MatchString(name) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid deployment name"))
	}
	if _, err := s.k8s.GetDeployment(ctx, req.Namespace, name); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("deployment not found"))
	}

	configVars, secretKeys, err := s.k8s.GetWorkloadConfig(ctx, req.Namespace, name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return &idpv1.GetDeploymentConfigResponse{
		ConfigVars:    keyValues(configVars),
		SecretKeys:    secretKeys,
		ConfigMapName: kubernetes.WorkloadConfigMapName(name),
		SecretName:    kubernetes.WorkloadSecretName(name),
	}, nil
}

// UpdateConfig replaces the non-sensitive configuration wholesale and applies a
// partial update to the sensitive one.
//
// The asymmetry is deliberate. The client is shown every config value, so it
// can send the full desired set. It is never shown a secret value, so a full
// replace would delete every secret the user did not retype; instead it sends
// only what it changed plus an explicit removal list.
func (s *Service) UpdateConfig(ctx context.Context, req *idpv1.UpdateDeploymentConfigRequest) (*idpv1.UpdateDeploymentConfigResponse, error) {
	if _, err := s.authorizeNamespace(ctx, req.Namespace, true); err != nil {
		return nil, err
	}
	if err := s.requireK8s(); err != nil {
		return nil, err
	}

	name := strings.ToLower(strings.TrimSpace(req.Name))
	if !resourceNameRegex.MatchString(name) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid deployment name"))
	}
	if _, err := s.k8s.GetDeployment(ctx, req.Namespace, name); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("deployment not found"))
	}

	configVars := make(map[string]string, len(req.ConfigVars))
	for _, kv := range req.ConfigVars {
		configVars[kv.Key] = kv.Value
	}
	setSecrets := make(map[string]string, len(req.SecretVars))
	for _, kv := range req.SecretVars {
		setSecrets[kv.Key] = kv.Value
	}

	if err := kubernetes.ValidateEnvMap(configVars, "config variables"); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := kubernetes.ValidateEnvMap(setSecrets, "secret variables"); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	for _, key := range req.RemovedSecretKeys {
		if err := kubernetes.ValidateEnvKey(key); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		if _, contradictory := setSecrets[key]; contradictory {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("%q is both set and removed in the same request", key))
		}
	}
	for key := range setSecrets {
		if _, dup := configVars[key]; dup {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("%q is defined as both a config and a secret variable", key))
		}
	}

	if _, err := s.k8s.MergeWorkloadSecret(ctx, req.Namespace, name, setSecrets, req.RemovedSecretKeys); err != nil {
		s.auditConfigFailure(ctx, req.Namespace, name, err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := s.k8s.EnsureWorkloadConfig(ctx, req.Namespace, name, configVars, nil); err != nil {
		s.auditConfigFailure(ctx, req.Namespace, name, err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Running containers read env once, at start. Without this the call would
	// report success while every pod kept serving the previous values.
	restarted, err := s.k8s.ApplyConfigChecksum(ctx, req.Namespace, name, configVars)
	if err != nil {
		s.auditConfigFailure(ctx, req.Namespace, name, err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	_, secretKeys, err := s.k8s.GetWorkloadConfig(ctx, req.Namespace, name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.audit.RecordFromUser(ctx, "deployment.config.update", req.Namespace, name, "deployment", "success", map[string]any{
		"config_keys":         sortedKeys(configVars),
		"secret_keys_set":     sortedKeys(setSecrets),
		"secret_keys_removed": req.RemovedSecretKeys,
		"restarted":           restarted,
	})

	return &idpv1.UpdateDeploymentConfigResponse{
		ConfigVars: keyValues(configVars),
		SecretKeys: secretKeys,
		Restarted:  restarted,
	}, nil
}

func (s *Service) auditConfigFailure(ctx context.Context, namespace, name string, err error) {
	s.audit.RecordFromUser(ctx, "deployment.config.update", namespace, name, "deployment", "failure",
		map[string]any{"error": err.Error()})
}

// Get retrieves a deployment.
func (s *Service) Get(ctx context.Context, req *idpv1.GetDeploymentRequest) (*idpv1.GetDeploymentResponse, error) {
	if _, err := s.authorizeNamespace(ctx, req.Namespace, false); err != nil {
		return nil, err
	}
	if err := s.requireK8s(); err != nil {
		return nil, err
	}

	deployment, err := s.k8s.GetDeployment(ctx, req.Namespace, req.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return &idpv1.GetDeploymentResponse{Deployment: convert.DeploymentToProto(deployment)}, nil
}

// List lists deployments in a namespace.
func (s *Service) List(ctx context.Context, req *idpv1.ListDeploymentsRequest) (*idpv1.ListDeploymentsResponse, error) {
	if _, err := s.authorizeNamespace(ctx, req.Namespace, false); err != nil {
		return nil, err
	}
	if err := s.requireK8s(); err != nil {
		return nil, err
	}

	all, err := s.k8s.ListDeployments(ctx, req.Namespace)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	page, pageSize := pagination.Normalize(1, 20)
	if req.Page != nil {
		page, pageSize = pagination.Normalize(req.Page.Page, req.Page.PageSize)
	}

	total := int64(len(all))
	offset := pagination.Offset(page, pageSize)
	end := offset + pageSize
	if offset > int32(len(all)) {
		offset = int32(len(all))
	}
	if end > int32(len(all)) {
		end = int32(len(all))
	}

	slice := all[offset:end]
	deployments := make([]*idpv1.Deployment, 0, len(slice))
	for i := range slice {
		deployments = append(deployments, convert.DeploymentToProto(&slice[i]))
	}

	return &idpv1.ListDeploymentsResponse{
		Deployments: deployments,
		PageInfo:    pagination.PageInfo(page, pageSize, total),
	}, nil
}

// Scale scales a deployment.
func (s *Service) Scale(ctx context.Context, req *idpv1.ScaleDeploymentRequest) (*idpv1.ScaleDeploymentResponse, error) {
	if _, err := s.authorizeNamespace(ctx, req.Namespace, true); err != nil {
		return nil, err
	}
	if err := s.requireK8s(); err != nil {
		return nil, err
	}

	deployment, err := s.k8s.ScaleDeployment(ctx, req.Namespace, req.Name, req.Replicas)
	if err != nil {
		s.audit.RecordFromUser(ctx, "deployment.scale", req.Namespace, req.Name, "deployment", "failure", map[string]any{"error": err.Error()})
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.audit.RecordFromUser(ctx, "deployment.scale", req.Namespace, req.Name, "deployment", "success", map[string]any{"replicas": req.Replicas})
	return &idpv1.ScaleDeploymentResponse{Deployment: convert.DeploymentToProto(deployment)}, nil
}

// Restart restarts a deployment.
func (s *Service) Restart(ctx context.Context, req *idpv1.RestartDeploymentRequest) (*idpv1.RestartDeploymentResponse, error) {
	if _, err := s.authorizeNamespace(ctx, req.Namespace, true); err != nil {
		return nil, err
	}
	if err := s.requireK8s(); err != nil {
		return nil, err
	}

	deployment, err := s.k8s.RestartDeployment(ctx, req.Namespace, req.Name)
	if err != nil {
		s.audit.RecordFromUser(ctx, "deployment.restart", req.Namespace, req.Name, "deployment", "failure", map[string]any{"error": err.Error()})
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.audit.RecordFromUser(ctx, "deployment.restart", req.Namespace, req.Name, "deployment", "success", nil)
	return &idpv1.RestartDeploymentResponse{Deployment: convert.DeploymentToProto(deployment)}, nil
}

// Delete deletes a deployment.
func (s *Service) Delete(ctx context.Context, req *idpv1.DeleteDeploymentRequest) (*idpv1.DeleteDeploymentResponse, error) {
	if _, err := s.authorizeNamespace(ctx, req.Namespace, true); err != nil {
		return nil, err
	}
	if err := s.requireK8s(); err != nil {
		return nil, err
	}

	if err := s.k8s.DeleteDeployment(ctx, req.Namespace, req.Name); err != nil {
		s.audit.RecordFromUser(ctx, "deployment.delete", req.Namespace, req.Name, "deployment", "failure", map[string]any{"error": err.Error()})
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Remove the fronting Service too, otherwise it lingers with no endpoints
	// and blocks recreating the deployment under the same name.
	if err := s.k8s.DeleteWorkloadService(ctx, req.Namespace, req.Name); err != nil {
		s.audit.RecordFromUser(ctx, "deployment.delete", req.Namespace, req.Name, "deployment", "failure", map[string]any{"error": err.Error()})
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// A surviving Ingress keeps advertising a hostname that now routes to
	// nothing, which reads as a broken app rather than a deleted one.
	if err := s.k8s.DeleteIngress(ctx, req.Namespace, req.Name); err != nil {
		s.audit.RecordFromUser(ctx, "deployment.delete", req.Namespace, req.Name, "deployment", "failure", map[string]any{"error": err.Error()})
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// The ConfigMap and Secret would otherwise outlive the workload, leaving
	// stale credentials readable in the namespace indefinitely.
	if err := s.k8s.DeleteWorkloadConfig(ctx, req.Namespace, req.Name); err != nil {
		s.audit.RecordFromUser(ctx, "deployment.delete", req.Namespace, req.Name, "deployment", "failure", map[string]any{"error": err.Error()})
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.audit.RecordFromUser(ctx, "deployment.delete", req.Namespace, req.Name, "deployment", "success", nil)
	return &idpv1.DeleteDeploymentResponse{}, nil
}
