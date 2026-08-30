package project

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"connectrpc.com/connect"
	"github.com/idp/platform/backend/internal/audit"
	"github.com/idp/platform/backend/internal/auth"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
	"github.com/idp/platform/backend/internal/keycloak"
	"github.com/idp/platform/backend/internal/kubernetes"
	"github.com/idp/platform/backend/internal/pkg/convert"
	"github.com/idp/platform/backend/internal/pkg/pagination"
	"github.com/idp/platform/backend/internal/repository"
	"github.com/jackc/pgx/v5/pgtype"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

var slugRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

type Service struct {
	repo     *repository.ProjectRepository
	nsRepo   *repository.NamespaceRepository
	k8s      *kubernetes.Client
	audit    *audit.Service
	keycloak *keycloak.Admin
}

func NewService(
	repo *repository.ProjectRepository,
	nsRepo *repository.NamespaceRepository,
	k8s *kubernetes.Client,
	auditSvc *audit.Service,
	kc *keycloak.Admin,
) *Service {
	return &Service{repo: repo, nsRepo: nsRepo, k8s: k8s, audit: auditSvc, keycloak: kc}
}

func (s *Service) Create(ctx context.Context, req *idpv1.CreateProjectRequest) (*idpv1.CreateProjectResponse, error) {
	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	if !user.IsAdmin() {
		return nil, connect.NewError(connect.CodePermissionDenied, auth.ErrInsufficientPermissions)
	}

	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	if !slugRegex.MatchString(slug) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid slug: lowercase alphanumeric with hyphens"))
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = slug
	}

	row, err := s.repo.Create(ctx, repository.CreateProjectInput{
		Slug:        slug,
		Name:        name,
		Description: req.Description,
		OwnerID:     user.ID,
		OwnerEmail:  user.Email,
		Labels:      req.Labels,
	})
	if err != nil {
		s.audit.RecordFromUser(ctx, "project.create", "", slug, "project", "failure", map[string]any{"error": err.Error()})
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.audit.RecordFromUser(ctx, "project.create", "", slug, "project", "success", nil)
	return &idpv1.CreateProjectResponse{Project: convert.ProjectToProto(*row, 0, 0)}, nil
}

func (s *Service) Get(ctx context.Context, req *idpv1.GetProjectRequest) (*idpv1.GetProjectResponse, error) {
	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	row, err := s.repo.GetBySlug(ctx, req.Slug)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("project not found"))
	}
	if !user.IsAdmin() && row.OwnerEmail != user.Email {
		if _, err := s.repo.GetMember(ctx, row.ID, user.Email); err != nil {
			return nil, connect.NewError(connect.CodePermissionDenied, auth.ErrInsufficientPermissions)
		}
	}
	memberCount, _ := s.repo.CountMembers(ctx, row.ID)
	nsCount, _ := s.repo.CountNamespaces(ctx, row.ID)
	return &idpv1.GetProjectResponse{Project: convert.ProjectToProto(*row, int32(memberCount), int32(nsCount))}, nil
}

func (s *Service) List(ctx context.Context, req *idpv1.ListProjectsRequest) (*idpv1.ListProjectsResponse, error) {
	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	page, pageSize := pagination.Normalize(1, 20)
	if req.Page != nil {
		page, pageSize = pagination.Normalize(req.Page.Page, req.Page.PageSize)
	}

	scoped := !user.IsAdmin() || req.MineOnly

	var (
		rows  []repository.ProjectRow
		total int64
	)
	if scoped {
		rows, err = s.repo.ListForMember(ctx, user.Email, pageSize, pagination.Offset(page, pageSize))
		if err == nil {
			total, err = s.repo.CountForMember(ctx, user.Email)
		}
	} else {
		rows, err = s.repo.List(ctx, pageSize, pagination.Offset(page, pageSize))
		if err == nil {
			total, err = s.repo.Count(ctx)
		}
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	projects := make([]*idpv1.Project, 0, len(rows))
	for _, r := range rows {
		mc, _ := s.repo.CountMembers(ctx, r.ID)
		nc, _ := s.repo.CountNamespaces(ctx, r.ID)
		projects = append(projects, convert.ProjectToProto(r, int32(mc), int32(nc)))
	}
	return &idpv1.ListProjectsResponse{
		Projects: projects,
		PageInfo: pagination.PageInfo(page, pageSize, total),
	}, nil
}

func (s *Service) Update(ctx context.Context, req *idpv1.UpdateProjectRequest) (*idpv1.UpdateProjectResponse, error) {
	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	if !user.IsAdmin() {
		return nil, connect.NewError(connect.CodePermissionDenied, auth.ErrInsufficientPermissions)
	}
	row, err := s.repo.Update(ctx, repository.UpdateProjectInput{
		Slug:        req.Slug,
		Name:        req.Name,
		Description: req.Description,
		Labels:      req.Labels,
	})
	if err != nil {
		s.audit.RecordFromUser(ctx, "project.update", "", req.Slug, "project", "failure", map[string]any{"error": err.Error()})
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.audit.RecordFromUser(ctx, "project.update", "", req.Slug, "project", "success", nil)
	return &idpv1.UpdateProjectResponse{Project: convert.ProjectToProto(*row, 0, 0)}, nil
}

func (s *Service) Delete(ctx context.Context, req *idpv1.DeleteProjectRequest) (*idpv1.DeleteProjectResponse, error) {
	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	if !user.IsAdmin() {
		return nil, connect.NewError(connect.CodePermissionDenied, auth.ErrInsufficientPermissions)
	}
	row, err := s.repo.GetBySlug(ctx, req.Slug)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("project not found"))
	}
	if err := s.releaseNamespaces(ctx, row.ID); err != nil {
		s.audit.RecordFromUser(ctx, "project.delete", "", req.Slug, "project", "failure", map[string]any{"error": err.Error()})
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := s.repo.Delete(ctx, req.Slug); err != nil {
		s.audit.RecordFromUser(ctx, "project.delete", "", req.Slug, "project", "failure", map[string]any{"error": err.Error()})
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.audit.RecordFromUser(ctx, "project.delete", "", req.Slug, "project", "success", nil)
	return &idpv1.DeleteProjectResponse{}, nil
}

// releaseNamespaces unbinds the project's namespaces from the platform so the
// project row can be deleted. Kubernetes delete is best-effort: a disconnected
// cluster must not block an admin from removing a project.
func (s *Service) releaseNamespaces(ctx context.Context, projectID pgtype.UUID) error {
	if s.nsRepo == nil {
		return nil
	}
	nss, err := s.nsRepo.ListByProject(ctx, projectID)
	if err != nil {
		return err
	}
	for i := range nss {
		name := nss[i].Name
		if s.k8s.Available() {
			if err := s.k8s.DeleteNamespace(ctx, name); err != nil && !apierrors.IsNotFound(err) {
				// Keep going: the IDP record is the source of truth for the console.
			}
		}
		if err := s.nsRepo.MarkDeleted(ctx, name); err != nil {
			return fmt.Errorf("detach namespace %s: %w", name, err)
		}
	}
	return nil
}

func (s *Service) AddMember(ctx context.Context, req *idpv1.AddMemberRequest) (*idpv1.AddMemberResponse, error) {
	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	if !user.IsAdmin() {
		return nil, connect.NewError(connect.CodePermissionDenied, auth.ErrInsufficientPermissions)
	}
	role := strings.ToLower(strings.TrimSpace(req.Role))
	if role != "developer" && role != "viewer" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("role must be developer or viewer"))
	}
	email := strings.ToLower(strings.TrimSpace(req.UserEmail))
	if email == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user_email required"))
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = email
	}
	proj, err := s.repo.GetBySlug(ctx, req.ProjectSlug)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("project not found"))
	}

	// Provision the Keycloak login first so a DB-only membership can never
	// strand someone who cannot sign in to the platform.
	var kcCreated bool
	loginUsername := username
	if s.keycloak != nil && s.keycloak.Enabled() {
		kcID, created, kcErr := s.keycloak.EnsureUserWithRole(ctx, keycloak.EnsureUserInput{
			Email:     email,
			Username:  username,
			Password:  req.Password,
			Temporary: req.TemporaryPassword,
			RealmRole: role,
		})
		if kcErr != nil {
			s.audit.RecordFromUser(ctx, "project.member.add", "", proj.Slug, "project_member", "failure",
				map[string]any{"error": kcErr.Error(), "email": email, "phase": "keycloak"})
			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf(
				"could not create Keycloak login for %s: %w", email, kcErr))
		}
		kcCreated = created
		_ = kcID
	} else if strings.TrimSpace(req.Password) != "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New(
			"Keycloak admin is not configured; cannot create a login account from the IDP UI"))
	}

	m, err := s.repo.AddMember(ctx, repository.AddMemberInput{
		ProjectID: proj.ID,
		UserID:    email,
		UserEmail: email,
		Role:      role,
	})
	if err != nil {
		s.audit.RecordFromUser(ctx, "project.member.add", "", proj.Slug, "project_member", "failure", map[string]any{"error": err.Error(), "email": email})
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.audit.RecordFromUser(ctx, "project.member.add", "", proj.Slug, "project_member", "success",
		map[string]any{"email": email, "role": role, "keycloak_created": kcCreated, "login": loginUsername})
	return &idpv1.AddMemberResponse{
		Member:              convert.ProjectMemberToProto(*m),
		KeycloakUserCreated: kcCreated,
		LoginUsername:       loginUsername,
	}, nil
}

func (s *Service) RemoveMember(ctx context.Context, req *idpv1.RemoveMemberRequest) (*idpv1.RemoveMemberResponse, error) {
	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	if !user.IsAdmin() {
		return nil, connect.NewError(connect.CodePermissionDenied, auth.ErrInsufficientPermissions)
	}
	proj, err := s.repo.GetBySlug(ctx, req.ProjectSlug)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("project not found"))
	}
	if err := s.repo.RemoveMember(ctx, proj.ID, strings.ToLower(req.UserEmail)); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.audit.RecordFromUser(ctx, "project.member.remove", "", proj.Slug, "project_member", "success", map[string]any{"email": req.UserEmail})
	return &idpv1.RemoveMemberResponse{}, nil
}

func (s *Service) ListMembers(ctx context.Context, req *idpv1.ListMembersRequest) (*idpv1.ListMembersResponse, error) {
	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	proj, err := s.repo.GetBySlug(ctx, req.ProjectSlug)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("project not found"))
	}
	if !user.IsAdmin() && proj.OwnerEmail != user.Email {
		if _, err := s.repo.GetMember(ctx, proj.ID, user.Email); err != nil {
			return nil, connect.NewError(connect.CodePermissionDenied, auth.ErrInsufficientPermissions)
		}
	}
	members, err := s.repo.ListMembers(ctx, proj.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*idpv1.ProjectMember, 0, len(members))
	for _, m := range members {
		out = append(out, convert.ProjectMemberToProto(m))
	}
	return &idpv1.ListMembersResponse{Members: out}, nil
}
