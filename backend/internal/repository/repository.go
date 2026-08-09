package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"

	"github.com/idp/platform/backend/internal/database"
	db "github.com/idp/platform/backend/internal/database/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// AuditRepository handles audit log persistence.
type AuditRepository struct {
	queries *db.Queries
}

// NewAuditRepository creates an audit repository.
func NewAuditRepository(pool *database.Pool) *AuditRepository {
	return &AuditRepository{queries: db.New(pool)}
}

// CreateAuditLogInput holds audit log creation parameters.
type CreateAuditLogInput struct {
	UserID       string
	UserEmail    string
	Action       string
	Namespace    string
	Resource     string
	ResourceType string
	Result       string
	Details      map[string]any
	IPAddress    string
}

// Create stores a new audit log entry.
func (r *AuditRepository) Create(ctx context.Context, input CreateAuditLogInput) (*db.AuditLog, error) {
	var details []byte
	if input.Details != nil {
		var err error
		details, err = json.Marshal(input.Details)
		if err != nil {
			return nil, fmt.Errorf("marshal details: %w", err)
		}
	}

	var ipAddr *netip.Addr
	if input.IPAddress != "" {
		addr, err := netip.ParseAddr(input.IPAddress)
		if err == nil {
			ipAddr = &addr
		}
	}

	row, err := r.queries.CreateAuditLog(ctx, db.CreateAuditLogParams{
		UserID:       input.UserID,
		UserEmail:    input.UserEmail,
		Action:       input.Action,
		Namespace:    optionalString(input.Namespace),
		Resource:     optionalString(input.Resource),
		ResourceType: optionalString(input.ResourceType),
		Result:       input.Result,
		Details:      details,
		IpAddress:    ipAddr,
	})
	if err != nil {
		return nil, fmt.Errorf("create audit log: %w", err)
	}
	return &row, nil
}

// ListAuditLogsInput holds list parameters.
type ListAuditLogsInput struct {
	Namespace *string
	UserID    *string
	Action    *string
	Limit     int32
	Offset    int32
}

// List returns paginated audit logs.
func (r *AuditRepository) List(ctx context.Context, input ListAuditLogsInput) ([]db.AuditLog, error) {
	rows, err := r.queries.ListAuditLogs(ctx, db.ListAuditLogsParams{
		Namespace: input.Namespace,
		UserID:    input.UserID,
		Action:    input.Action,
		Limit:     input.Limit,
		Offset:    input.Offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list audit logs: %w", err)
	}
	return rows, nil
}

// Count returns total audit logs matching filters.
func (r *AuditRepository) Count(ctx context.Context, input ListAuditLogsInput) (int64, error) {
	count, err := r.queries.CountAuditLogs(ctx, db.CountAuditLogsParams{
		Namespace: input.Namespace,
		UserID:    input.UserID,
		Action:    input.Action,
	})
	if err != nil {
		return 0, fmt.Errorf("count audit logs: %w", err)
	}
	return count, nil
}

// NamespaceRepository handles namespace metadata persistence.
type NamespaceRepository struct {
	queries *db.Queries
}

// NewNamespaceRepository creates a namespace repository.
func NewNamespaceRepository(pool *database.Pool) *NamespaceRepository {
	return &NamespaceRepository{queries: db.New(pool)}
}

// CreateNamespaceInput holds namespace creation parameters.
type CreateNamespaceInput struct {
	Name        string
	DisplayName string
	Description string
	OwnerID     string
	OwnerEmail  string
	Labels      map[string]string
	Annotations map[string]string
	// ProjectID links the namespace to its owning project. An invalid (zero)
	// UUID stores NULL, leaving the namespace unattached and therefore outside
	// the reach of any project-scoped registry credential.
	ProjectID pgtype.UUID
}

// Create stores namespace metadata.
func (r *NamespaceRepository) Create(ctx context.Context, input CreateNamespaceInput) (*db.Namespace, error) {
	labels, err := json.Marshal(input.Labels)
	if err != nil {
		return nil, fmt.Errorf("marshal labels: %w", err)
	}
	annotations, err := json.Marshal(input.Annotations)
	if err != nil {
		return nil, fmt.Errorf("marshal annotations: %w", err)
	}

	row, err := r.queries.CreateNamespace(ctx, db.CreateNamespaceParams{
		Name:        input.Name,
		DisplayName: input.DisplayName,
		Description: optionalString(input.Description),
		OwnerID:     input.OwnerID,
		OwnerEmail:  input.OwnerEmail,
		Labels:      labels,
		Annotations: annotations,
		Status:      "active",
		ProjectID:   input.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("create namespace: %w", err)
	}
	return &row, nil
}

// SetProject attaches a namespace to a project, or detaches it when projectID
// is the zero value.
func (r *NamespaceRepository) SetProject(ctx context.Context, name string, projectID pgtype.UUID) (*db.Namespace, error) {
	row, err := r.queries.SetNamespaceProject(ctx, db.SetNamespaceProjectParams{
		Name:      name,
		ProjectID: projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("set namespace project: %w", err)
	}
	return &row, nil
}

// ListByProject returns the active namespaces owned by a project. This is the
// fan-out set for materialising project-scoped Kubernetes resources such as
// registry pull Secrets.
func (r *NamespaceRepository) ListByProject(ctx context.Context, projectID pgtype.UUID) ([]db.Namespace, error) {
	rows, err := r.queries.ListNamespacesByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list namespaces by project: %w", err)
	}
	return rows, nil
}

// GetByName retrieves a namespace by name.
func (r *NamespaceRepository) GetByName(ctx context.Context, name string) (*db.Namespace, error) {
	row, err := r.queries.GetNamespaceByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("get namespace: %w", err)
	}
	return &row, nil
}

// List returns paginated namespaces. A non-nil ownerEmail restricts results to
// that owner, so tenant scoping happens in SQL and pagination stays correct.
func (r *NamespaceRepository) List(ctx context.Context, status, ownerEmail *string, limit, offset int32) ([]db.Namespace, error) {
	rows, err := r.queries.ListNamespaces(ctx, db.ListNamespacesParams{
		Status:     status,
		OwnerEmail: ownerEmail,
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	return rows, nil
}

// Count returns total namespaces, scoped to ownerEmail when it is non-nil.
func (r *NamespaceRepository) Count(ctx context.Context, status, ownerEmail *string) (int64, error) {
	count, err := r.queries.CountNamespaces(ctx, db.CountNamespacesParams{
		Status:     status,
		OwnerEmail: ownerEmail,
	})
	if err != nil {
		return 0, fmt.Errorf("count namespaces: %w", err)
	}
	return count, nil
}

// MarkDeleted soft-deletes a namespace.
func (r *NamespaceRepository) MarkDeleted(ctx context.Context, name string) error {
	if err := r.queries.DeleteNamespace(ctx, name); err != nil {
		return fmt.Errorf("delete namespace: %w", err)
	}
	return nil
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// -----------------------------------------------------------------------------
// ProjectRepository
// -----------------------------------------------------------------------------

type ProjectRow = db.Project

type ProjectRepository struct {
	queries *db.Queries
}

func NewProjectRepository(pool *database.Pool) *ProjectRepository {
	return &ProjectRepository{queries: db.New(pool)}
}

type CreateProjectInput struct {
	Slug        string
	Name        string
	Description string
	OwnerID     string
	OwnerEmail  string
	Labels      map[string]string
}

func (r *ProjectRepository) Create(ctx context.Context, in CreateProjectInput) (*db.Project, error) {
	labels, err := json.Marshal(in.Labels)
	if err != nil {
		return nil, fmt.Errorf("marshal labels: %w", err)
	}
	row, err := r.queries.CreateProject(ctx, db.CreateProjectParams{
		Slug:        in.Slug,
		Name:        in.Name,
		Description: optionalString(in.Description),
		OwnerID:     in.OwnerID,
		OwnerEmail:  in.OwnerEmail,
		Labels:      labels,
	})
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return &row, nil
}

func (r *ProjectRepository) GetBySlug(ctx context.Context, slug string) (*db.Project, error) {
	row, err := r.queries.GetProjectBySlug(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	return &row, nil
}

// GetByID resolves a project from the foreign key stored on a namespace, which
// is how a deployment finds the credentials it should pull with.
func (r *ProjectRepository) GetByID(ctx context.Context, id pgtype.UUID) (*db.Project, error) {
	row, err := r.queries.GetProjectByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get project by id: %w", err)
	}
	return &row, nil
}

func (r *ProjectRepository) List(ctx context.Context, limit, offset int32) ([]db.Project, error) {
	rows, err := r.queries.ListProjects(ctx, db.ListProjectsParams{Limit: limit, Offset: offset})
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return rows, nil
}

func (r *ProjectRepository) ListForMember(ctx context.Context, email string, limit, offset int32) ([]db.Project, error) {
	rows, err := r.queries.ListProjectsForMember(ctx, db.ListProjectsForMemberParams{
		UserEmail: email,
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list projects for member: %w", err)
	}
	return rows, nil
}

func (r *ProjectRepository) Count(ctx context.Context) (int64, error) {
	c, err := r.queries.CountProjects(ctx)
	if err != nil {
		return 0, fmt.Errorf("count projects: %w", err)
	}
	return c, nil
}

func (r *ProjectRepository) CountForMember(ctx context.Context, email string) (int64, error) {
	c, err := r.queries.CountProjectsForMember(ctx, email)
	if err != nil {
		return 0, fmt.Errorf("count projects for member: %w", err)
	}
	return c, nil
}

type UpdateProjectInput struct {
	Slug        string
	Name        string
	Description string
	Labels      map[string]string
}

func (r *ProjectRepository) Update(ctx context.Context, in UpdateProjectInput) (*db.Project, error) {
	labels, err := json.Marshal(in.Labels)
	if err != nil {
		return nil, fmt.Errorf("marshal labels: %w", err)
	}
	row, err := r.queries.UpdateProject(ctx, db.UpdateProjectParams{
		Slug:        in.Slug,
		Name:        in.Name,
		Description: optionalString(in.Description),
		Labels:      labels,
	})
	if err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	return &row, nil
}

func (r *ProjectRepository) Delete(ctx context.Context, slug string) error {
	if err := r.queries.DeleteProject(ctx, slug); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

func (r *ProjectRepository) CountMembers(ctx context.Context, projectID pgtype.UUID) (int64, error) {
	c, err := r.queries.CountProjectMembers(ctx, projectID)
	if err != nil {
		return 0, fmt.Errorf("count members: %w", err)
	}
	return c, nil
}

func (r *ProjectRepository) CountNamespaces(ctx context.Context, projectID pgtype.UUID) (int64, error) {
	c, err := r.queries.CountProjectNamespaces(ctx, projectID)
	if err != nil {
		return 0, fmt.Errorf("count namespaces: %w", err)
	}
	return c, nil
}

type AddMemberInput struct {
	ProjectID pgtype.UUID
	UserID    string
	UserEmail string
	Role      string
}

func (r *ProjectRepository) AddMember(ctx context.Context, in AddMemberInput) (*db.ProjectMember, error) {
	row, err := r.queries.AddProjectMember(ctx, db.AddProjectMemberParams{
		ProjectID: in.ProjectID,
		UserID:    in.UserID,
		UserEmail: in.UserEmail,
		Role:      in.Role,
	})
	if err != nil {
		return nil, fmt.Errorf("add member: %w", err)
	}
	return &row, nil
}

func (r *ProjectRepository) RemoveMember(ctx context.Context, projectID pgtype.UUID, email string) error {
	if err := r.queries.RemoveProjectMember(ctx, db.RemoveProjectMemberParams{
		ProjectID: projectID,
		UserEmail: email,
	}); err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	return nil
}

func (r *ProjectRepository) ListMembers(ctx context.Context, projectID pgtype.UUID) ([]db.ProjectMember, error) {
	rows, err := r.queries.ListProjectMembers(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return rows, nil
}

func (r *ProjectRepository) GetMember(ctx context.Context, projectID pgtype.UUID, email string) (*db.ProjectMember, error) {
	row, err := r.queries.GetProjectMember(ctx, db.GetProjectMemberParams{
		ProjectID: projectID,
		UserEmail: email,
	})
	if err != nil {
		return nil, fmt.Errorf("get member: %w", err)
	}
	return &row, nil
}
