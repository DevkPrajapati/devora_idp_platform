package dbbrowse

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
)

// Handler implements the Connect RPC DatabaseService.
type Handler struct {
	service *Service
}

// NewHandler creates a new database browser RPC handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

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

func (h *Handler) ListDatabases(
	ctx context.Context,
	req *connect.Request[idpv1.ListDatabasesRequest],
) (*connect.Response[idpv1.ListDatabasesResponse], error) {
	return respond(h.service.ListDatabases(ctx, req.Msg))
}

func (h *Handler) InspectDatabase(
	ctx context.Context,
	req *connect.Request[idpv1.InspectDatabaseRequest],
) (*connect.Response[idpv1.InspectDatabaseResponse], error) {
	return respond(h.service.InspectDatabase(ctx, req.Msg))
}

func (h *Handler) QueryDocuments(
	ctx context.Context,
	req *connect.Request[idpv1.QueryDocumentsRequest],
) (*connect.Response[idpv1.QueryDocumentsResponse], error) {
	return respond(h.service.QueryDocuments(ctx, req.Msg))
}

func (h *Handler) ExportDatabase(
	ctx context.Context,
	req *connect.Request[idpv1.ExportDatabaseRequest],
) (*connect.Response[idpv1.ExportDatabaseResponse], error) {
	return respond(h.service.ExportDatabase(ctx, req.Msg))
}

func (h *Handler) ImportDatabase(
	ctx context.Context,
	req *connect.Request[idpv1.ImportDatabaseRequest],
) (*connect.Response[idpv1.ImportDatabaseResponse], error) {
	return respond(h.service.ImportDatabase(ctx, req.Msg))
}

func (h *Handler) EnsureDatabasePersistence(
	ctx context.Context,
	req *connect.Request[idpv1.EnsureDatabasePersistenceRequest],
) (*connect.Response[idpv1.EnsureDatabasePersistenceResponse], error) {
	return respond(h.service.EnsureDatabasePersistence(ctx, req.Msg))
}
