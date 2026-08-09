package storage

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
)

// Handler implements the Connect RPC StorageService.
type Handler struct {
	service *Service
}

// NewHandler creates a new storage RPC handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// respond forwards an already-coded Connect error untouched and wraps anything
// else as Internal, matching the other handlers in this codebase.
func respond[T any](resp *T, err error) (*connect.Response[T], error) {
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) GetStorageOverview(
	ctx context.Context,
	req *connect.Request[idpv1.GetStorageOverviewRequest],
) (*connect.Response[idpv1.GetStorageOverviewResponse], error) {
	return respond(h.service.GetStorageOverview(ctx, req.Msg))
}

func (h *Handler) ListPersistentVolumeClaims(
	ctx context.Context,
	req *connect.Request[idpv1.ListPersistentVolumeClaimsRequest],
) (*connect.Response[idpv1.ListPersistentVolumeClaimsResponse], error) {
	return respond(h.service.ListPersistentVolumeClaims(ctx, req.Msg))
}

func (h *Handler) ListPersistentVolumes(
	ctx context.Context,
	req *connect.Request[idpv1.ListPersistentVolumesRequest],
) (*connect.Response[idpv1.ListPersistentVolumesResponse], error) {
	return respond(h.service.ListPersistentVolumes(ctx, req.Msg))
}

func (h *Handler) ListStorageClasses(
	ctx context.Context,
	req *connect.Request[idpv1.ListStorageClassesRequest],
) (*connect.Response[idpv1.ListStorageClassesResponse], error) {
	return respond(h.service.ListStorageClasses(ctx, req.Msg))
}

func (h *Handler) ListNodeStorage(
	ctx context.Context,
	req *connect.Request[idpv1.ListNodeStorageRequest],
) (*connect.Response[idpv1.ListNodeStorageResponse], error) {
	return respond(h.service.ListNodeStorage(ctx, req.Msg))
}
