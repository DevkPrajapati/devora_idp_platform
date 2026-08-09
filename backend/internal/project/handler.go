package project

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) CreateProject(ctx context.Context, req *connect.Request[idpv1.CreateProjectRequest]) (*connect.Response[idpv1.CreateProjectResponse], error) {
	return wrap(h.service.Create(ctx, req.Msg))
}
func (h *Handler) GetProject(ctx context.Context, req *connect.Request[idpv1.GetProjectRequest]) (*connect.Response[idpv1.GetProjectResponse], error) {
	return wrap(h.service.Get(ctx, req.Msg))
}
func (h *Handler) ListProjects(ctx context.Context, req *connect.Request[idpv1.ListProjectsRequest]) (*connect.Response[idpv1.ListProjectsResponse], error) {
	return wrap(h.service.List(ctx, req.Msg))
}
func (h *Handler) UpdateProject(ctx context.Context, req *connect.Request[idpv1.UpdateProjectRequest]) (*connect.Response[idpv1.UpdateProjectResponse], error) {
	return wrap(h.service.Update(ctx, req.Msg))
}
func (h *Handler) DeleteProject(ctx context.Context, req *connect.Request[idpv1.DeleteProjectRequest]) (*connect.Response[idpv1.DeleteProjectResponse], error) {
	return wrap(h.service.Delete(ctx, req.Msg))
}
func (h *Handler) AddMember(ctx context.Context, req *connect.Request[idpv1.AddMemberRequest]) (*connect.Response[idpv1.AddMemberResponse], error) {
	return wrap(h.service.AddMember(ctx, req.Msg))
}
func (h *Handler) RemoveMember(ctx context.Context, req *connect.Request[idpv1.RemoveMemberRequest]) (*connect.Response[idpv1.RemoveMemberResponse], error) {
	return wrap(h.service.RemoveMember(ctx, req.Msg))
}
func (h *Handler) ListMembers(ctx context.Context, req *connect.Request[idpv1.ListMembersRequest]) (*connect.Response[idpv1.ListMembersResponse], error) {
	return wrap(h.service.ListMembers(ctx, req.Msg))
}

func wrap[T any](resp *T, err error) (*connect.Response[T], error) {
	if err != nil {
		var ce *connect.Error
		if errors.As(err, &ce) {
			return nil, ce
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}
