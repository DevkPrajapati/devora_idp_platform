package registry

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
)

// Handler implements the Connect RPC RegistryService.
type Handler struct {
	service *Service
}

// NewHandler creates a new registry RPC handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) SaveRegistryCredential(
	ctx context.Context,
	req *connect.Request[idpv1.SaveRegistryCredentialRequest],
) (*connect.Response[idpv1.SaveRegistryCredentialResponse], error) {
	resp, err := h.service.Save(ctx, req.Msg)
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ListRegistryCredentials(
	ctx context.Context,
	req *connect.Request[idpv1.ListRegistryCredentialsRequest],
) (*connect.Response[idpv1.ListRegistryCredentialsResponse], error) {
	resp, err := h.service.List(ctx, req.Msg)
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) DeleteRegistryCredential(
	ctx context.Context,
	req *connect.Request[idpv1.DeleteRegistryCredentialRequest],
) (*connect.Response[idpv1.DeleteRegistryCredentialResponse], error) {
	resp, err := h.service.Delete(ctx, req.Msg)
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) TestRegistryConnection(
	ctx context.Context,
	req *connect.Request[idpv1.TestRegistryConnectionRequest],
) (*connect.Response[idpv1.TestRegistryConnectionResponse], error) {
	resp, err := h.service.TestConnection(ctx, req.Msg)
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(resp), nil
}

// asConnectError preserves the status code the service chose. Wrapping
// everything as Internal would turn a validation error into a 500 and hide the
// message the UI needs to show.
func asConnectError(err error) error {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr
	}
	return connect.NewError(connect.CodeInternal, err)
}
