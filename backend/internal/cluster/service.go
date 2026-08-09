package cluster

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
	"github.com/idp/platform/backend/internal/kubernetes"
	"github.com/idp/platform/backend/internal/pkg/convert"
)

// Service handles cluster overview business logic.
type Service struct {
	k8s *kubernetes.Client
}

// NewService creates a cluster service.
func NewService(k8s *kubernetes.Client) *Service {
	return &Service{k8s: k8s}
}

// GetOverview returns cluster statistics.
func (s *Service) GetOverview(ctx context.Context, _ *idpv1.GetOverviewRequest) (*idpv1.GetOverviewResponse, error) {
	if s.k8s == nil {
		return &idpv1.GetOverviewResponse{
			ClusterName: "disconnected",
			Connected:   false,
		}, nil
	}

	overview, err := s.k8s.GetOverview(ctx)
	if err != nil {
		return &idpv1.GetOverviewResponse{
			ClusterName: "disconnected",
			Connected:   false,
		}, nil
	}

	return convert.ClusterOverviewToProto(overview), nil
}

// ListEvents returns recent cluster events.
func (s *Service) ListEvents(ctx context.Context, req *idpv1.ListEventsRequest) (*idpv1.ListEventsResponse, error) {
	if s.k8s == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("kubernetes cluster not connected"))
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
	if s.k8s == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("kubernetes cluster not connected"))
	}

	pods, err := s.k8s.ListPods(ctx, req.Namespace)
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
	if s.k8s == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("kubernetes cluster not connected"))
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
	if s.k8s == nil {
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
	if s.k8s == nil {
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
	if s.k8s == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("kubernetes cluster not connected"))
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
	if s.k8s == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("kubernetes cluster not connected"))
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
	if s.k8s == nil {
		return 0, 0, errors.New("kubernetes cluster not connected")
	}

	metrics, err := s.k8s.GetResourceMetrics(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("read resource metrics: %w", err)
	}

	return metrics.CPUUsagePercent, metrics.MemoryUsagePercent, nil
}
