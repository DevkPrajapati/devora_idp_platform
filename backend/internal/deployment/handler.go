package deployment

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
)

// Handler implements the Connect RPC DeploymentService.
type Handler struct {
	service *Service
}

// NewHandler creates a new deployment RPC handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListDeploymentTemplates(
	ctx context.Context,
	req *connect.Request[idpv1.ListDeploymentTemplatesRequest],
) (*connect.Response[idpv1.ListDeploymentTemplatesResponse], error) {
	resp, err := h.service.ListTemplates(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ListRollouts(
	ctx context.Context,
	req *connect.Request[idpv1.ListRolloutsRequest],
) (*connect.Response[idpv1.ListRolloutsResponse], error) {
	resp, err := h.service.ListRollouts(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) RollbackDeployment(
	ctx context.Context,
	req *connect.Request[idpv1.RollbackDeploymentRequest],
) (*connect.Response[idpv1.RollbackDeploymentResponse], error) {
	resp, err := h.service.Rollback(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) GetDeploymentConfig(
	ctx context.Context,
	req *connect.Request[idpv1.GetDeploymentConfigRequest],
) (*connect.Response[idpv1.GetDeploymentConfigResponse], error) {
	resp, err := h.service.GetConfig(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) UpdateDeploymentConfig(
	ctx context.Context,
	req *connect.Request[idpv1.UpdateDeploymentConfigRequest],
) (*connect.Response[idpv1.UpdateDeploymentConfigResponse], error) {
	resp, err := h.service.UpdateConfig(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) CreateDeployment(
	ctx context.Context,
	req *connect.Request[idpv1.CreateDeploymentRequest],
) (*connect.Response[idpv1.CreateDeploymentResponse], error) {
	resp, err := h.service.Create(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) GetDeployment(
	ctx context.Context,
	req *connect.Request[idpv1.GetDeploymentRequest],
) (*connect.Response[idpv1.GetDeploymentResponse], error) {
	resp, err := h.service.Get(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ListDeployments(
	ctx context.Context,
	req *connect.Request[idpv1.ListDeploymentsRequest],
) (*connect.Response[idpv1.ListDeploymentsResponse], error) {
	resp, err := h.service.List(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ScaleDeployment(
	ctx context.Context,
	req *connect.Request[idpv1.ScaleDeploymentRequest],
) (*connect.Response[idpv1.ScaleDeploymentResponse], error) {
	resp, err := h.service.Scale(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) RestartDeployment(
	ctx context.Context,
	req *connect.Request[idpv1.RestartDeploymentRequest],
) (*connect.Response[idpv1.RestartDeploymentResponse], error) {
	resp, err := h.service.Restart(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) DeleteDeployment(
	ctx context.Context,
	req *connect.Request[idpv1.DeleteDeploymentRequest],
) (*connect.Response[idpv1.DeleteDeploymentResponse], error) {
	resp, err := h.service.Delete(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}
