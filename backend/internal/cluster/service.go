package cluster

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/idp/platform/backend/internal/audit"
	"github.com/idp/platform/backend/internal/config"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
	"github.com/idp/platform/backend/internal/kubernetes"
	"github.com/idp/platform/backend/internal/pkg/convert"
	"github.com/idp/platform/backend/internal/pkg/secretbox"
	"github.com/idp/platform/backend/internal/repository"
	"go.uber.org/zap"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// Service handles cluster overview and fleet lifecycle.
type Service struct {
	k8s         *kubernetes.Client
	hub         *kubernetes.Hub
	repo        *repository.ClusterRepository
	box         *secretbox.Box
	provisioner *kubernetes.Provisioner
	audit       *audit.Service
	logger      *zap.Logger
	k8sCfg      config.KubernetesConfig
	logs        *clusterLogHub
	state       StateReconciler
	watch       *watcher
	capacity    *capacityWatch
	jobs        jobSet
}

// NewService creates a cluster service. Fleet fields may be nil in tests that
// only exercise overview/log RPCs.
func NewService(k8s *kubernetes.Client) *Service {
	return &Service{k8s: k8s, provisioner: kubernetes.NewProvisioner(), logs: newClusterLogHub()}
}

// FleetOptions wires the admin cluster lifecycle.
type FleetOptions struct {
	Hub         *kubernetes.Hub
	Repo        *repository.ClusterRepository
	Box         *secretbox.Box
	Provisioner *kubernetes.Provisioner
	Audit       *audit.Service
	Logger      *zap.Logger
	Config      config.KubernetesConfig
	// State retires platform records that belonged to a cluster which has since
	// been rebuilt. Optional; when nil, identity changes are logged only.
	State StateReconciler
}

// WithFleet attaches fleet dependencies. Called from server wiring so tests
// that construct NewService(k8s) keep compiling.
func (s *Service) WithFleet(opts FleetOptions) *Service {
	s.hub = opts.Hub
	s.repo = opts.Repo
	s.box = opts.Box
	if opts.Provisioner != nil {
		s.provisioner = opts.Provisioner
	}
	s.audit = opts.Audit
	s.logger = opts.Logger
	s.k8sCfg = opts.Config
	s.state = opts.State
	if s.hub != nil && s.k8s == nil {
		s.k8s = s.hub.Live()
	}
	return s
}

func (s *Service) disconnectedOverview() *idpv1.GetOverviewResponse {
	return &idpv1.GetOverviewResponse{
		ClusterName: "disconnected",
		Connected:   false,
	}
}

func (s *Service) live() bool {
	return s.k8s != nil && s.k8s.Available()
}

// GetOverview returns cluster statistics.
func (s *Service) GetOverview(ctx context.Context, _ *idpv1.GetOverviewRequest) (*idpv1.GetOverviewResponse, error) {
	if !s.live() {
		return s.disconnectedOverview(), nil
	}

	overview, err := s.k8s.GetOverview(ctx)
	if err != nil {
		return s.disconnectedOverview(), nil
	}

	return convert.ClusterOverviewToProto(overview), nil
}

// ListEvents returns recent cluster events.
func (s *Service) ListEvents(ctx context.Context, req *idpv1.ListEventsRequest) (*idpv1.ListEventsResponse, error) {
	if !s.live() {
		return &idpv1.ListEventsResponse{}, nil
	}

	events, err := s.k8s.ListEvents(ctx, req.Namespace, req.Limit)
	if err != nil {
		return nil, kubernetes.ConnectError(err)
	}

	result := make([]*idpv1.ClusterEvent, 0, len(events))
	for _, e := range events {
		result = append(result, convert.ClusterEventToProto(e))
	}

	return &idpv1.ListEventsResponse{Events: result}, nil
}

// ListPods returns the list of pods.
func (s *Service) ListPods(ctx context.Context, req *idpv1.ListPodsRequest) (*idpv1.ListPodsResponse, error) {
	if !s.live() {
		return &idpv1.ListPodsResponse{}, nil
	}

	pods, err := s.k8s.ListPods(ctx, req.Namespace, req.App)
	if err != nil {
		return nil, kubernetes.ConnectError(err)
	}

	result := make([]*idpv1.PodInfo, 0, len(pods))
	for _, p := range pods {
		result = append(result, convert.PodToProto(p))
	}

	return &idpv1.ListPodsResponse{Pods: result}, nil
}

// ListServices returns the list of services.
func (s *Service) ListServices(ctx context.Context, req *idpv1.ListServicesRequest) (*idpv1.ListServicesResponse, error) {
	if !s.live() {
		return &idpv1.ListServicesResponse{}, nil
	}

	svcs, err := s.k8s.ListServices(ctx, req.Namespace)
	if err != nil {
		return nil, kubernetes.ConnectError(err)
	}

	result := make([]*idpv1.ServiceInfo, 0, len(svcs))
	for _, svc := range svcs {
		result = append(result, convert.ServiceToProto(svc))
	}

	return &idpv1.ListServicesResponse{Services: result}, nil
}

// GetPodLogs returns container logs for a pod.
func (s *Service) GetPodLogs(ctx context.Context, req *idpv1.GetPodLogsRequest) (*idpv1.GetPodLogsResponse, error) {
	if !s.live() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("kubernetes cluster not connected"))
	}
	if req.Namespace == "" || req.PodName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace and pod_name are required"))
	}

	logs, err := s.k8s.GetPodLogs(ctx, req.Namespace, req.PodName, int64(req.TailLines))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return &idpv1.GetPodLogsResponse{Logs: logs}, nil
}

// StreamPodLogs pushes log lines to emit until the client disconnects or the
// container exits.
//
// The service does not accumulate lines: emit writes straight to the transport,
// so a slow client applies backpressure to the read from Kubernetes instead of
// growing an unbounded buffer in the backend.
func (s *Service) StreamPodLogs(
	ctx context.Context,
	req *idpv1.StreamPodLogsRequest,
	emit func(*idpv1.LogLine) error,
) error {
	if !s.live() {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("kubernetes cluster not connected"))
	}
	if strings.TrimSpace(req.Namespace) == "" || strings.TrimSpace(req.PodName) == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace and pod_name are required"))
	}

	err := s.k8s.StreamPodLogs(ctx, kubernetes.LogStreamOptions{
		Namespace: req.Namespace,
		PodName:   req.PodName,
		Container: req.Container,
		TailLines: int64(req.TailLines),
		Follow:    req.Follow,
	}, func(line kubernetes.LogLine) error {
		return emit(convert.LogLineToProto(line))
	})
	if err != nil {
		// The client going away is the normal way a follow stream ends, not a
		// failure to report.
		if ctx.Err() != nil {
			return nil
		}
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return connectErr
		}
		return connect.NewError(connect.CodeInternal, err)
	}
	return nil
}

// ListNodes returns cluster nodes.
func (s *Service) ListNodes(ctx context.Context, _ *idpv1.ListNodesRequest) (*idpv1.ListNodesResponse, error) {
	if !s.live() {
		return &idpv1.ListNodesResponse{}, nil
	}

	nodes, err := s.k8s.ListNodes(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	result := make([]*idpv1.NodeInfo, 0, len(nodes))
	for _, n := range nodes {
		result = append(result, convert.NodeToProto(n))
	}

	return &idpv1.ListNodesResponse{Nodes: result}, nil
}

// GetResourceMetrics returns cluster resource utilization.
func (s *Service) GetResourceMetrics(ctx context.Context, _ *idpv1.GetResourceMetricsRequest) (*idpv1.GetResourceMetricsResponse, error) {
	if !s.live() {
		return &idpv1.GetResourceMetricsResponse{}, nil
	}

	metrics, err := s.k8s.GetResourceMetrics(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return convert.ResourceMetricsToProto(metrics), nil
}

// ResourceUsage returns current CPU and memory utilisation as whole
// percentages, satisfying metrics.Source.
//
// Separate from GetResourceMetrics because the sampler is not an RPC caller:
// it wants two numbers, not a protobuf message wrapped in a connect.Error.
func (s *Service) ResourceUsage(ctx context.Context) (int32, int32, error) {
	if !s.live() {
		return 0, 0, errors.New("kubernetes cluster not connected")
	}

	metrics, err := s.k8s.GetResourceMetrics(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("read resource metrics: %w", err)
	}

	return metrics.CPUUsagePercent, metrics.MemoryUsagePercent, nil
}

// ListClusterNamespaces returns every namespace the API server currently has.
func (s *Service) ListClusterNamespaces(ctx context.Context, _ *idpv1.ListClusterNamespacesRequest) (*idpv1.ListClusterNamespacesResponse, error) {
	if !s.live() {
		return &idpv1.ListClusterNamespacesResponse{}, nil
	}

	namespaces, err := s.k8s.ListClusterNamespaces(ctx)
	if err != nil {
		return nil, kubernetes.ConnectError(err)
	}

	result := make([]*idpv1.ClusterNamespace, 0, len(namespaces))
	for _, ns := range namespaces {
		result = append(result, convert.ClusterNamespaceToProto(ns))
	}
	return &idpv1.ListClusterNamespacesResponse{Namespaces: result}, nil
}

// GetNamespaceResources returns the live resource tree for one namespace.
func (s *Service) GetNamespaceResources(ctx context.Context, req *idpv1.GetNamespaceResourcesRequest) (*idpv1.GetNamespaceResourcesResponse, error) {
	if !s.live() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("kubernetes cluster not connected"))
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	inv, err := s.k8s.GetNamespaceResources(ctx, req.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("namespace %s not found", req.Name))
		}
		return nil, kubernetes.ConnectError(err)
	}
	return convert.NamespaceInventoryToProto(inv), nil
}
