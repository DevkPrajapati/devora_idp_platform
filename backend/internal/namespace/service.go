package namespace

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"connectrpc.com/connect"
	"github.com/idp/platform/backend/internal/audit"
	"github.com/idp/platform/backend/internal/auth"
	db "github.com/idp/platform/backend/internal/database/sqlc"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
	"github.com/idp/platform/backend/internal/kubernetes"
	"github.com/idp/platform/backend/internal/pkg/convert"
	"github.com/idp/platform/backend/internal/pkg/pagination"
	"github.com/idp/platform/backend/internal/repository"
	"github.com/jackc/pgx/v5/pgtype"
)

var namespaceNameRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// PullSecretResolver materialises a namespace's project registry credentials.
// Declared here rather than imported so the namespace package stays independent
// of credential storage.
type PullSecretResolver interface {
	EnsureNamespacePullSecrets(ctx context.Context, namespace string) ([]string, error)
}

// Service handles namespace business logic.
type Service struct {
	repo        *repository.NamespaceRepository
	projects    *repository.ProjectRepository
	k8s         *kubernetes.Client
	pullSecrets PullSecretResolver
	audit       *audit.Service
}

// NewService creates a namespace service.
func NewService(
	repo *repository.NamespaceRepository,
	projects *repository.ProjectRepository,
	k8s *kubernetes.Client,
	pullSecrets PullSecretResolver,
	auditSvc *audit.Service,
) *Service {
	return &Service{repo: repo, projects: projects, k8s: k8s, pullSecrets: pullSecrets, audit: auditSvc}
}

// resolveProject maps an optional project slug to its ID. An empty slug yields
// the zero UUID, which stores NULL and leaves the namespace unattached.
func (s *Service) resolveProject(ctx context.Context, slug string) (pgtype.UUID, string, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return pgtype.UUID{}, "", nil
	}
	project, err := s.projects.GetBySlug(ctx, slug)
	if err != nil {
		return pgtype.UUID{}, "", connect.NewError(connect.CodeNotFound, fmt.Errorf("project %q not found", slug))
	}
	return project.ID, project.Slug, nil
}

// projectSlugFor resolves the slug to report for a namespace row. Failures
// degrade to an empty slug: this is display detail, not a reason to fail a read.
func (s *Service) projectSlugFor(ctx context.Context, ns *db.Namespace) string {
	if ns == nil || !ns.ProjectID.Valid {
		return ""
	}
	project, err := s.projects.GetByID(ctx, ns.ProjectID)
	if err != nil {
		return ""
	}
	return project.Slug
}

// SetProject attaches a namespace to a project (or detaches it when the slug is
// empty) and immediately materialises that project's registry credentials, so
// the namespace can pull private images without a redeploy.
func (s *Service) SetProject(ctx context.Context, req *idpv1.SetNamespaceProjectRequest) (*idpv1.SetNamespaceProjectResponse, error) {
	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	if !user.IsAdmin() {
		return nil, connect.NewError(connect.CodePermissionDenied, auth.ErrInsufficientPermissions)
	}

	name := strings.ToLower(strings.TrimSpace(req.Name))
	if !namespaceNameRegex.MatchString(name) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid namespace name"))
	}
	if _, err := s.repo.GetByName(ctx, name); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("namespace not found"))
	}

	projectID, projectSlug, err := s.resolveProject(ctx, req.ProjectSlug)
	if err != nil {
		return nil, err
	}

	ns, err := s.repo.SetProject(ctx, name, projectID)
	if err != nil {
		s.audit.RecordFromUser(ctx, "namespace.set_project", name, name, "namespace", "failure", map[string]any{"error": err.Error()})
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	var synced []string
	if s.pullSecrets != nil && projectID.Valid {
		synced, err = s.pullSecrets.EnsureNamespacePullSecrets(ctx, name)
		if err != nil {
			// The link itself succeeded; report the sync failure rather than
			// pretending the namespace is ready to pull private images.
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}
	if synced == nil {
		synced = []string{}
	}

	s.audit.RecordFromUser(ctx, "namespace.set_project", name, name, "namespace", "success",
		map[string]any{"project": projectSlug, "synced_registry_secrets": synced})

	return &idpv1.SetNamespaceProjectResponse{
		Namespace:             convert.NamespaceToProto(*ns, projectSlug),
		SyncedRegistrySecrets: synced,
	}, nil
}

// Create creates a new tenant namespace.
func (s *Service) Create(ctx context.Context, req *idpv1.CreateNamespaceRequest) (*idpv1.CreateNamespaceResponse, error) {
	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	if !user.IsAdmin() {
		return nil, connect.NewError(connect.CodePermissionDenied, auth.ErrInsufficientPermissions)
	}

	name := strings.ToLower(strings.TrimSpace(req.Name))
	if !namespaceNameRegex.MatchString(name) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid namespace name"))
	}

	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = name
	}

	if s.k8s == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("kubernetes cluster not connected"))
	}

	// Resolved before the namespace exists so an unknown project slug fails
	// cleanly instead of leaving an orphaned namespace behind.
	projectID, projectSlug, err := s.resolveProject(ctx, req.ProjectSlug)
	if err != nil {
		return nil, err
	}

	k8sCfg := kubernetes.NamespaceConfig{
		Name:        name,
		DisplayName: displayName,
		OwnerID:     user.ID,
		OwnerEmail:  user.Email,
		Labels:      req.Labels,
		Annotations: req.Annotations,
	}

	if err := s.k8s.CreateNamespace(ctx, k8sCfg); err != nil {
		s.audit.RecordFromUser(ctx, "namespace.create", name, name, "namespace", "failure", map[string]any{"error": err.Error()})
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	ns, err := s.repo.Create(ctx, repository.CreateNamespaceInput{
		Name:        name,
		DisplayName: displayName,
		Description: req.Description,
		OwnerID:     user.ID,
		OwnerEmail:  user.Email,
		Labels:      req.Labels,
		Annotations: req.Annotations,
		ProjectID:   projectID,
	})
	if err != nil {
		_ = s.k8s.DeleteNamespace(ctx, name)
		s.audit.RecordFromUser(ctx, "namespace.create", name, name, "namespace", "failure", map[string]any{"error": err.Error()})
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Seed the project's registry credentials so the first deployment into this
	// namespace can pull private images. A failure here is reported rather than
	// swallowed, but the namespace itself stays: it is usable for public images
	// and re-syncs on the next credential save or deployment.
	if s.pullSecrets != nil && projectID.Valid {
		if _, syncErr := s.pullSecrets.EnsureNamespacePullSecrets(ctx, name); syncErr != nil {
			s.audit.RecordFromUser(ctx, "namespace.create", name, name, "namespace", "failure",
				map[string]any{"error": syncErr.Error(), "stage": "registry_secret_sync"})
			return nil, connect.NewError(connect.CodeInternal, syncErr)
		}
	}

	s.audit.RecordFromUser(ctx, "namespace.create", name, name, "namespace", "success",
		map[string]any{"project": projectSlug})

	return &idpv1.CreateNamespaceResponse{
		Namespace: convert.NamespaceToProto(*ns, projectSlug),
	}, nil
}

// Get retrieves a namespace by name. Non-admins may only read namespaces they
// own; a foreign namespace reports NotFound rather than PermissionDenied so the
// response does not reveal that it exists.
func (s *Service) Get(ctx context.Context, req *idpv1.GetNamespaceRequest) (*idpv1.GetNamespaceResponse, error) {
	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	ns, err := s.repo.GetByName(ctx, req.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("namespace not found"))
	}

	if !user.IsAdmin() && ns.OwnerEmail != user.Email {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("namespace not found"))
	}

	return &idpv1.GetNamespaceResponse{Namespace: convert.NamespaceToProto(*ns, s.projectSlugFor(ctx, ns))}, nil
}

// List returns paginated namespaces.
func (s *Service) List(ctx context.Context, req *idpv1.ListNamespacesRequest) (*idpv1.ListNamespacesResponse, error) {
	page, pageSize := pagination.Normalize(1, 20)
	if req.Page != nil {
		page, pageSize = pagination.Normalize(req.Page.Page, req.Page.PageSize)
	}

	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	var status *string
	if req.Status != "" {
		status = &req.Status
	}

	// Scope non-admins to their own namespaces in SQL. Filtering after the
	// query would silently drop rows from an already-paginated page and report
	// a total that does not match what the caller can actually see.
	var ownerEmail *string
	if !user.IsAdmin() {
		ownerEmail = &user.Email
	}

	rows, err := s.repo.List(ctx, status, ownerEmail, pageSize, pagination.Offset(page, pageSize))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	total, err := s.repo.Count(ctx, status, ownerEmail)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Slugs are resolved through a per-page cache: without it a page of 20
	// namespaces sharing one project would issue 20 identical project lookups.
	slugCache := make(map[string]string)
	namespaces := make([]*idpv1.Namespace, 0, len(rows))
	for i := range rows {
		row := rows[i]
		key := row.ProjectID.String()
		slug, cached := slugCache[key]
		if !cached {
			slug = s.projectSlugFor(ctx, &row)
			slugCache[key] = slug
		}
		namespaces = append(namespaces, convert.NamespaceToProto(row, slug))
	}

	return &idpv1.ListNamespacesResponse{
		Namespaces: namespaces,
		PageInfo:   pagination.PageInfo(page, pageSize, total),
	}, nil
}

// Delete removes a namespace.
func (s *Service) Delete(ctx context.Context, req *idpv1.DeleteNamespaceRequest) (*idpv1.DeleteNamespaceResponse, error) {
	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	if !user.IsAdmin() {
		return nil, connect.NewError(connect.CodePermissionDenied, auth.ErrInsufficientPermissions)
	}

	if _, err := s.repo.GetByName(ctx, req.Name); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("namespace not found"))
	}

	if s.k8s != nil {
		if err := s.k8s.DeleteNamespace(ctx, req.Name); err != nil {
			s.audit.RecordFromUser(ctx, "namespace.delete", req.Name, req.Name, "namespace", "failure", map[string]any{"error": err.Error()})
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}

	if err := s.repo.MarkDeleted(ctx, req.Name); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.audit.RecordFromUser(ctx, "namespace.delete", req.Name, req.Name, "namespace", "success", nil)
	return &idpv1.DeleteNamespaceResponse{}, nil
}
