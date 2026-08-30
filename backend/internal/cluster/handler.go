package cluster

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
)

// Handler implements the Connect RPC ClusterService.
type Handler struct {
	service *Service
}

// NewHandler creates a new cluster RPC handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetOverview(
	ctx context.Context,
	req *connect.Request[idpv1.GetOverviewRequest],
) (*connect.Response[idpv1.GetOverviewResponse], error) {
	resp, err := h.service.GetOverview(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ListEvents(
	ctx context.Context,
	req *connect.Request[idpv1.ListEventsRequest],
) (*connect.Response[idpv1.ListEventsResponse], error) {
	resp, err := h.service.ListEvents(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ListPods(
	ctx context.Context,
	req *connect.Request[idpv1.ListPodsRequest],
) (*connect.Response[idpv1.ListPodsResponse], error) {
	resp, err := h.service.ListPods(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ListServices(
	ctx context.Context,
	req *connect.Request[idpv1.ListServicesRequest],
) (*connect.Response[idpv1.ListServicesResponse], error) {
	resp, err := h.service.ListServices(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) GetPodLogs(
	ctx context.Context,
	req *connect.Request[idpv1.GetPodLogsRequest],
) (*connect.Response[idpv1.GetPodLogsResponse], error) {
	resp, err := h.service.GetPodLogs(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

// StreamPodLogs implements the server-streaming RPC. Connect gives one
// ServerStream per request; Send flushes to the wire, so a line reaches the
// browser as soon as the container writes it.
func (h *Handler) StreamPodLogs(
	ctx context.Context,
	req *connect.Request[idpv1.StreamPodLogsRequest],
	stream *connect.ServerStream[idpv1.LogLine],
) error {
	err := h.service.StreamPodLogs(ctx, req.Msg, stream.Send)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return connectErr
		}
		return connect.NewError(connect.CodeInternal, err)
	}
	return nil
}

func (h *Handler) StreamClusterLogs(
	ctx context.Context,
	req *connect.Request[idpv1.StreamClusterLogsRequest],
	stream *connect.ServerStream[idpv1.LogLine],
) error {
	err := h.service.StreamClusterLogs(ctx, req.Msg, stream.Send)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return connectErr
		}
		return connect.NewError(connect.CodeInternal, err)
	}
	return nil
}

func (h *Handler) ListNodes(
	ctx context.Context,
	req *connect.Request[idpv1.ListNodesRequest],
) (*connect.Response[idpv1.ListNodesResponse], error) {
	resp, err := h.service.ListNodes(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) GetResourceMetrics(
	ctx context.Context,
	req *connect.Request[idpv1.GetResourceMetricsRequest],
) (*connect.Response[idpv1.GetResourceMetricsResponse], error) {
	resp, err := h.service.GetResourceMetrics(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ListClusterNamespaces(
	ctx context.Context,
	req *connect.Request[idpv1.ListClusterNamespacesRequest],
) (*connect.Response[idpv1.ListClusterNamespacesResponse], error) {
	resp, err := h.service.ListClusterNamespaces(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) GetNamespaceResources(
	ctx context.Context,
	req *connect.Request[idpv1.GetNamespaceResourcesRequest],
) (*connect.Response[idpv1.GetNamespaceResourcesResponse], error) {
	resp, err := h.service.GetNamespaceResources(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ListClusters(
	ctx context.Context,
	req *connect.Request[idpv1.ListClustersRequest],
) (*connect.Response[idpv1.ListClustersResponse], error) {
	resp, err := h.service.ListClusters(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) CreateCluster(
	ctx context.Context,
	req *connect.Request[idpv1.CreateClusterRequest],
) (*connect.Response[idpv1.ManagedCluster], error) {
	resp, err := h.service.CreateCluster(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ActivateCluster(
	ctx context.Context,
	req *connect.Request[idpv1.ActivateClusterRequest],
) (*connect.Response[idpv1.ManagedCluster], error) {
	resp, err := h.service.ActivateCluster(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) StopCluster(
	ctx context.Context,
	req *connect.Request[idpv1.StopClusterRequest],
) (*connect.Response[idpv1.ManagedCluster], error) {
	resp, err := h.service.StopCluster(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) RestartCluster(
	ctx context.Context,
	req *connect.Request[idpv1.RestartClusterRequest],
) (*connect.Response[idpv1.ManagedCluster], error) {
	resp, err := h.service.RestartCluster(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) DeleteCluster(
	ctx context.Context,
	req *connect.Request[idpv1.DeleteClusterRequest],
) (*connect.Response[idpv1.DeleteClusterResponse], error) {
	resp, err := h.service.DeleteCluster(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}
