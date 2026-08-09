package audit

import (
	"context"

	"connectrpc.com/connect"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
)

// Handler implements the Connect RPC AuditService.
type Handler struct {
	service *Service
}

// NewHandler creates a new audit RPC handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// ListAuditLogs returns paginated audit logs.
func (h *Handler) ListAuditLogs(
	ctx context.Context,
	req *connect.Request[idpv1.ListAuditLogsRequest],
) (*connect.Response[idpv1.ListAuditLogsResponse], error) {
	resp, err := h.service.List(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}
