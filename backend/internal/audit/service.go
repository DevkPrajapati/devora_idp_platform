package audit

import (
	"context"

	"connectrpc.com/connect"
	"github.com/idp/platform/backend/internal/auth"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
	"github.com/idp/platform/backend/internal/pkg/convert"
	"github.com/idp/platform/backend/internal/pkg/pagination"
	"github.com/idp/platform/backend/internal/repository"
)

// Service handles audit log business logic.
type Service struct {
	repo *repository.AuditRepository
}

// NewService creates an audit service.
func NewService(repo *repository.AuditRepository) *Service {
	return &Service{repo: repo}
}

// Record creates an audit log entry for a platform action.
func (s *Service) Record(ctx context.Context, input repository.CreateAuditLogInput) {
	_, _ = s.repo.Create(ctx, input)
}

// List returns paginated audit logs.
func (s *Service) List(ctx context.Context, req *idpv1.ListAuditLogsRequest) (*idpv1.ListAuditLogsResponse, error) {
	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	if !user.IsAdmin() {
		return nil, connect.NewError(connect.CodePermissionDenied, auth.ErrInsufficientPermissions)
	}

	page, pageSize := pagination.Normalize(1, 20)
	if req.Page != nil {
		page, pageSize = pagination.Normalize(req.Page.Page, req.Page.PageSize)
	}

	filter := repository.ListAuditLogsInput{
		Limit:  pageSize,
		Offset: pagination.Offset(page, pageSize),
	}
	if req.Namespace != "" {
		filter.Namespace = &req.Namespace
	}
	if req.UserId != "" {
		filter.UserID = &req.UserId
	}
	if req.Action != "" {
		filter.Action = &req.Action
	}

	logs, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	total, err := s.repo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	result := make([]*idpv1.AuditLog, 0, len(logs))
	for _, log := range logs {
		result = append(result, convert.AuditLogToProto(log))
	}

	return &idpv1.ListAuditLogsResponse{
		Logs:     result,
		PageInfo: pagination.PageInfo(page, pageSize, total),
	}, nil
}

// RecordFromUser records an audit log using the authenticated user from context.
func (s *Service) RecordFromUser(ctx context.Context, action, namespace, resource, resourceType, result string, details map[string]any) {
	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return
	}

	s.Record(ctx, repository.CreateAuditLogInput{
		UserID:       user.ID,
		UserEmail:    user.Email,
		Action:       action,
		Namespace:    namespace,
		Resource:     resource,
		ResourceType: resourceType,
		Result:       result,
		Details:      details,
	})
}
