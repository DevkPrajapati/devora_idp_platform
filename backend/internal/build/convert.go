package build

import (
	"context"
	"strings"
	"time"

	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
	"github.com/idp/platform/backend/internal/kubernetes"
	"github.com/idp/platform/backend/internal/repository"
	"github.com/jackc/pgx/v5/pgtype"
)

// repositoryToProto renders a repository for the API.
//
// Neither the git token nor the webhook secret has a field on the wire: they
// are write-only, so a compromised read path cannot hand out credentials that
// grant push access to a source repository.
func (s *Service) repositoryToProto(projectSlug string, row *repository.GitRepositoryRow) *idpv1.GitRepository {
	return &idpv1.GitRepository{
		ProjectSlug:        projectSlug,
		Name:               row.Name,
		Provider:           row.Provider,
		CloneUrl:           row.CloneUrl,
		DefaultBranch:      row.DefaultBranch,
		DockerfilePath:     row.DockerfilePath,
		BuildContext:       row.BuildContext,
		ImageRepository:    row.ImageRepository,
		RegistryCredential: row.RegistryCredential,
		AutoDeploy:         row.AutoDeploy,
		TargetNamespace:    derefString(row.TargetNamespace),
		TargetDeployment:   derefString(row.TargetDeployment),
		// Reported so the UI can tell the user whether a webhook will be
		// accepted, without revealing the secret itself.
		HasToken:         len(row.TokenEncrypted) > 0,
		HasWebhookSecret: len(row.WebhookSecretEncrypted) > 0,
		WebhookUrl:       s.webhookURL(row.ID),
		CreatedAt:        timestampString(row.CreatedAt),
		UpdatedAt:        timestampString(row.UpdatedAt),
	}
}

// webhookURL renders the endpoint a user pastes into their git provider.
func (s *Service) webhookURL(id pgtype.UUID) string {
	base := strings.TrimRight(s.cfg.PublicURL, "/")
	if base == "" {
		// Relative when no public URL is configured, so the UI still shows the
		// correct path rather than a URL pointing at the wrong host.
		return WebhookPath + "/" + uuidString(id)
	}
	return base + WebhookPath + "/" + uuidString(id)
}

// buildToProto renders a build record.
func buildToProto(row *repository.BuildRow, repositoryName string) *idpv1.Build {
	return &idpv1.Build{
		RepositoryName: repositoryName,
		Number:         row.Number,
		Branch:         row.Branch,
		CommitSha:      row.CommitSha,
		ImageTag:       row.ImageTag,
		Status:         row.Status,
		TriggerType:    row.TriggerType,
		TriggeredBy:    row.TriggeredBy,
		JobName:        row.JobName,
		ErrorMessage:   derefString(row.ErrorMessage),
		StartedAt:      timestampString(row.StartedAt),
		FinishedAt:     timestampString(row.FinishedAt),
		CreatedAt:      timestampString(row.CreatedAt),
	}
}

// recordSystemAudit records an action the platform took on its own initiative,
// with no user in the request context — auto-deploy runs from the reconciler,
// long after the request that triggered the build has returned.
func (s *Service) recordSystemAudit(ctx context.Context, namespace, resource, result string, details map[string]any) {
	s.audit.Record(ctx, repository.CreateAuditLogInput{
		UserID:       "system",
		UserEmail:    "system",
		Action:       "build.deploy",
		Namespace:    namespace,
		Resource:     resource,
		ResourceType: "deployment",
		Result:       result,
		Details:      details,
	})
}

// BuildResourceDefaults bounds what a build Job may consume. A build compiles
// untrusted repository code, so an unbounded one lets a single repository
// starve every workload on the node.
func BuildResourceDefaults() kubernetes.ResourceSpec {
	return kubernetes.ResourceSpec{
		CPURequest:    "500m",
		CPULimit:      "2",
		MemoryRequest: "1Gi",
		MemoryLimit:   "4Gi",
	}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func timestampString(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return ""
	}
	return ts.Time.UTC().Format(time.RFC3339)
}
