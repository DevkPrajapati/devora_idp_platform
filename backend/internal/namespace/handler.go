package namespace

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
)

// Handler implements the Connect RPC NamespaceService.
type Handler struct {
	service *Service
}

// NewHandler creates a new namespace RPC handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) SetNamespaceProject(
	ctx context.Context,
	req *connect.Request[idpv1.SetNamespaceProjectRequest],
) (*connect.Response[idpv1.SetNamespaceProjectResponse], error) {
	resp, err := h.service.SetProject(ctx, req.Msg)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) CreateNamespace(
	ctx context.Context,
	req *connect.Request[idpv1.CreateNamespaceRequest],
) (*connect.Response[idpv1.CreateNamespaceResponse], error) {
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

func (h *Handler) GetNamespace(
	ctx context.Context,
	req *connect.Request[idpv1.GetNamespaceRequest],
) (*connect.Response[idpv1.GetNamespaceResponse], error) {
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

func (h *Handler) ListNamespaces(
	ctx context.Context,
	req *connect.Request[idpv1.ListNamespacesRequest],
) (*connect.Response[idpv1.ListNamespacesResponse], error) {
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

func (h *Handler) DeleteNamespace(
	ctx context.Context,
	req *connect.Request[idpv1.DeleteNamespaceRequest],
) (*connect.Response[idpv1.DeleteNamespaceResponse], error) {
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
