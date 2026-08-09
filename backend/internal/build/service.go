// Package build turns a git repository into a running deployment:
// clone, build an image with Kaniko, push it to the project's registry, and
// optionally update the target Deployment.
//
// The platform holds no build state of its own beyond the `builds` table. Each
// build is a Kubernetes Job; the reconciler reads Job status and advances the
// row. A backend restart therefore loses nothing — the Jobs keep running and
// are picked up again on the next pass.
package build

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/idp/platform/backend/internal/audit"
	"github.com/idp/platform/backend/internal/auth"
	db "github.com/idp/platform/backend/internal/database/sqlc"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
	"github.com/idp/platform/backend/internal/kubernetes"
	"github.com/idp/platform/backend/internal/pkg/secretbox"
	"github.com/idp/platform/backend/internal/repository"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"
)

var repositoryNameRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// dockerTagUnsafe matches everything a Docker tag may not contain. Branch names
// legitimately include slashes ("feature/foo"), which are not valid in a tag.
var dockerTagUnsafe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

const maxRepositoryNameLen = 48

// Build statuses, mirroring the database CHECK constraint.
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Trigger kinds.
const (
	TriggerManual  = "manual"
	TriggerWebhook = "webhook"
	TriggerRetry   = "retry"
)

// ErrRepositoryUnknown is returned for a webhook whose repository id does not
// resolve. The HTTP layer maps it to 404 without saying more.
var ErrRepositoryUnknown = errors.New("unknown repository")

// Config holds build execution settings.
type Config struct {
	// Namespace is where build Jobs run. Separate from application namespaces
	// so a build pod cannot read application Secrets.
	Namespace string
	// KanikoImage overrides the default builder image.
	KanikoImage string
	// PublicURL is the platform's externally reachable base URL, used to render
	// the webhook endpoint a user pastes into their git provider.
	PublicURL string
	// Resources bound what a build may consume. A build compiles untrusted
	// repository code, so leaving it unbounded lets one repo starve the cluster.
	Resources kubernetes.ResourceSpec
}

// Service implements build business logic.
type Service struct {
	builds   *repository.BuildRepository
	projects *repository.ProjectRepository
	k8s      *kubernetes.Client
	box      *secretbox.Box
	audit    *audit.Service
	logger   *zap.Logger
	cfg      Config
}

// NewService creates a build service.
func NewService(
	builds *repository.BuildRepository,
	projects *repository.ProjectRepository,
	k8s *kubernetes.Client,
	box *secretbox.Box,
	auditSvc *audit.Service,
	logger *zap.Logger,
	cfg Config,
) *Service {
	if cfg.Namespace == "" {
		cfg.Namespace = "idp-builds"
	}
	return &Service{
		builds: builds, projects: projects, k8s: k8s,
		box: box, audit: auditSvc, logger: logger, cfg: cfg,
	}
}

// BuildNamespace reports where build Jobs run, so callers can stream their logs.
func (s *Service) BuildNamespace() string { return s.cfg.Namespace }

func (s *Service) requireK8s() error {
	if s.k8s == nil {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("kubernetes cluster not connected"))
	}
	return nil
}

func (s *Service) requireEncryption() error {
	if !s.box.Enabled() {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New(
			"IDP_ENCRYPTION_KEY is not configured; refusing to store git tokens unencrypted"))
	}
	return nil
}

// authorizeProject mirrors the registry service: a project the caller cannot
// see reports NotFound so the error does not confirm the slug exists.
func (s *Service) authorizeProject(ctx context.Context, slug string, needWrite bool) (*auth.User, *db.Project, error) {
	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return nil, nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	if needWrite && !user.CanWrite() {
		return nil, nil, connect.NewError(connect.CodePermissionDenied, auth.ErrInsufficientPermissions)
	}
	if strings.TrimSpace(slug) == "" {
		return nil, nil, connect.NewError(connect.CodeInvalidArgument, errors.New("project_slug is required"))
	}

	project, err := s.projects.GetBySlug(ctx, slug)
	if err != nil {
		return nil, nil, connect.NewError(connect.CodeNotFound, errors.New("project not found"))
	}
	if !user.IsAdmin() && project.OwnerEmail != user.Email {
		member, memberErr := s.projects.GetMember(ctx, project.ID, user.Email)
		if memberErr != nil {
			return nil, nil, connect.NewError(connect.CodeNotFound, errors.New("project not found"))
		}
		if needWrite && member.Role != "developer" {
			return nil, nil, connect.NewError(connect.CodePermissionDenied, auth.ErrInsufficientPermissions)
		}
	}
	return user, project, nil
}

// SaveRepository registers or updates a repository.
func (s *Service) SaveRepository(ctx context.Context, req *idpv1.SaveGitRepositoryRequest) (*idpv1.SaveGitRepositoryResponse, error) {
	user, project, err := s.authorizeProject(ctx, req.ProjectSlug, true)
	if err != nil {
		return nil, err
	}

	name := strings.ToLower(strings.TrimSpace(req.Name))
	if name == "" || len(name) > maxRepositoryNameLen || !repositoryNameRegex.MatchString(name) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("name must be lowercase alphanumeric with hyphens"))
	}

	// Validated before storage so a bad URL fails here rather than inside a
	// build Job ten seconds later.
	if _, err := kubernetes.BuildCloneURL(req.CloneUrl, "", req.Provider); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	imageRepository := strings.TrimSpace(req.ImageRepository)
	if imageRepository == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("image_repository is required"))
	}
	if strings.Contains(imageRepository, ":") {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("image_repository must not include a tag; tags are generated per build"))
	}
	if strings.TrimSpace(req.RegistryCredential) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("registry_credential is required to push the built image"))
	}

	if req.AutoDeploy && (strings.TrimSpace(req.TargetNamespace) == "" || strings.TrimSpace(req.TargetDeployment) == "") {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("auto_deploy requires target_namespace and target_deployment"))
	}

	// Secrets are only encrypted when supplied. Empty means "keep the stored
	// value", which the SQL COALESCE honours — the UI never receives them, so
	// it cannot echo them back.
	var tokenEnc, webhookEnc []byte
	if req.Token != "" || req.WebhookSecret != "" {
		if err := s.requireEncryption(); err != nil {
			return nil, err
		}
	}
	if req.Token != "" {
		if tokenEnc, err = s.box.Encrypt(req.Token); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("encrypt token: %w", err))
		}
	}
	if req.WebhookSecret != "" {
		if webhookEnc, err = s.box.Encrypt(req.WebhookSecret); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("encrypt webhook secret: %w", err))
		}
	}

	row, err := s.builds.UpsertGitRepository(ctx, repository.UpsertGitRepositoryInput{
		ProjectID:              project.ID,
		Name:                   name,
		Provider:               NormalizeProvider(req.Provider),
		CloneURL:               strings.TrimSpace(req.CloneUrl),
		DefaultBranch:          defaultString(req.DefaultBranch, "main"),
		DockerfilePath:         defaultString(req.DockerfilePath, "Dockerfile"),
		BuildContext:           defaultString(req.BuildContext, "."),
		ImageRepository:        imageRepository,
		RegistryCredential:     strings.TrimSpace(req.RegistryCredential),
		TokenEncrypted:         tokenEnc,
		WebhookSecretEncrypted: webhookEnc,
		AutoDeploy:             req.AutoDeploy,
		TargetNamespace:        strings.TrimSpace(req.TargetNamespace),
		TargetDeployment:       strings.TrimSpace(req.TargetDeployment),
		CreatedBy:              user.Email,
	})
	if err != nil {
		s.audit.RecordFromUser(ctx, "build.repository.save", "", name, "git_repository", "failure",
			map[string]any{"error": err.Error(), "project": project.Slug})
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.audit.RecordFromUser(ctx, "build.repository.save", "", name, "git_repository", "success",
		map[string]any{"project": project.Slug, "clone_url": row.CloneUrl, "auto_deploy": row.AutoDeploy})

	return &idpv1.SaveGitRepositoryResponse{Repository: s.repositoryToProto(project.Slug, row)}, nil
}

// ListRepositories returns a project's repositories.
func (s *Service) ListRepositories(ctx context.Context, req *idpv1.ListGitRepositoriesRequest) (*idpv1.ListGitRepositoriesResponse, error) {
	_, project, err := s.authorizeProject(ctx, req.ProjectSlug, false)
	if err != nil {
		return nil, err
	}

	rows, err := s.builds.ListGitRepositories(ctx, project.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := make([]*idpv1.GitRepository, 0, len(rows))
	for i := range rows {
		out = append(out, s.repositoryToProto(project.Slug, &rows[i]))
	}
	return &idpv1.ListGitRepositoriesResponse{Repositories: out}, nil
}

// DeleteRepository removes a repository and, by cascade, its build history.
func (s *Service) DeleteRepository(ctx context.Context, req *idpv1.DeleteGitRepositoryRequest) (*idpv1.DeleteGitRepositoryResponse, error) {
	_, project, err := s.authorizeProject(ctx, req.ProjectSlug, true)
	if err != nil {
		return nil, err
	}
	name := strings.ToLower(strings.TrimSpace(req.Name))
	if _, err := s.builds.GetGitRepository(ctx, project.ID, name); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("git repository not found"))
	}
	if err := s.builds.DeleteGitRepository(ctx, project.ID, name); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.audit.RecordFromUser(ctx, "build.repository.delete", "", name, "git_repository", "success",
		map[string]any{"project": project.Slug})
	return &idpv1.DeleteGitRepositoryResponse{}, nil
}

// TriggerBuild starts a build from the UI.
func (s *Service) TriggerBuild(ctx context.Context, req *idpv1.TriggerBuildRequest) (*idpv1.TriggerBuildResponse, error) {
	user, project, err := s.authorizeProject(ctx, req.ProjectSlug, true)
	if err != nil {
		return nil, err
	}
	if err := s.requireK8s(); err != nil {
		return nil, err
	}

	repo, err := s.builds.GetGitRepository(ctx, project.ID, strings.ToLower(strings.TrimSpace(req.Name)))
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("git repository not found"))
	}

	branch := defaultString(req.Branch, repo.DefaultBranch)
	record, err := s.startBuild(ctx, repo, branch, "", TriggerManual, user.Email, pgtype.UUID{})
	if err != nil {
		return nil, err
	}
	return &idpv1.TriggerBuildResponse{Build: buildToProto(record, repo.Name)}, nil
}

// RetryBuild re-runs a previous build's branch and commit.
//
// A retry is a new row rather than a reset of the old one, so the history shows
// that a retry happened and what each attempt produced.
func (s *Service) RetryBuild(ctx context.Context, req *idpv1.RetryBuildRequest) (*idpv1.RetryBuildResponse, error) {
	user, project, err := s.authorizeProject(ctx, req.ProjectSlug, true)
	if err != nil {
		return nil, err
	}
	if err := s.requireK8s(); err != nil {
		return nil, err
	}

	repo, err := s.builds.GetGitRepository(ctx, project.ID, strings.ToLower(strings.TrimSpace(req.Name)))
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("git repository not found"))
	}

	previous, err := s.builds.GetBuildByNumber(ctx, repo.ID, req.BuildNumber)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("build not found"))
	}

	record, err := s.startBuild(ctx, repo, previous.Branch, previous.CommitSha, TriggerRetry, user.Email, previous.ID)
	if err != nil {
		return nil, err
	}
	return &idpv1.RetryBuildResponse{Build: buildToProto(record, repo.Name)}, nil
}

// startBuild records a build and launches its Job.
func (s *Service) startBuild(
	ctx context.Context,
	repo *repository.GitRepositoryRow,
	branch, commitSHA, trigger, triggeredBy string,
	retryOf pgtype.UUID,
) (*repository.BuildRow, error) {
	record, err := s.builds.CreateBuild(ctx, repository.CreateBuildInput{
		RepositoryID: repo.ID,
		Branch:       branch,
		CommitSHA:    commitSHA,
		TriggerType:  trigger,
		TriggeredBy:  triggeredBy,
		RetryOf:      retryOf,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	running, err := s.launchBuildJob(ctx, repo, record, branch, commitSHA)
	if err != nil {
		return nil, err
	}

	s.audit.RecordFromUser(ctx, "build.trigger", s.cfg.Namespace, repo.Name, "build", "success", map[string]any{
		"build":   record.Number,
		"branch":  branch,
		"image":   repo.ImageRepository + ":" + running.ImageTag,
		"trigger": trigger,
	})
	return running, nil
}

// launchBuildJob creates the Kaniko Job for an existing build row.
func (s *Service) launchBuildJob(
	ctx context.Context,
	repo *repository.GitRepositoryRow,
	record *repository.BuildRow,
	branch, commitSHA string,
) (*repository.BuildRow, error) {
	tag := BuildImageTag(branch, commitSHA, record.Number)
	destination := repo.ImageRepository + ":" + tag
	if err := s.builds.SetCommit(ctx, record.ID, commitSHA, tag); err != nil {
		return nil, s.failBuild(ctx, record, err.Error())
	}
	record.ImageTag = tag

	token := ""
	if len(repo.TokenEncrypted) > 0 {
		if !s.box.Enabled() {
			return nil, s.failBuild(ctx, record, "IDP_ENCRYPTION_KEY is not configured; cannot decrypt the git token")
		}
		var err error
		token, err = s.box.Decrypt(repo.TokenEncrypted)
		if err != nil {
			return nil, s.failBuild(ctx, record, "stored git token could not be decrypted; re-save the repository")
		}
	}

	if err := s.k8s.EnsureBuildNamespace(ctx, s.cfg.Namespace); err != nil {
		return nil, s.failBuild(ctx, record, err.Error())
	}

	registrySecret, err := s.ensureBuildRegistrySecret(ctx, repo)
	if err != nil {
		return nil, s.failBuild(ctx, record, err.Error())
	}

	jobName := BuildJobName(repo.Name, record.Number)
	spec := kubernetes.BuildJobSpec{
		Namespace:          s.cfg.Namespace,
		JobName:            jobName,
		BuildID:            uuidString(record.ID),
		CloneURL:           repo.CloneUrl,
		Ref:                branch,
		GitProvider:        repo.Provider,
		GitToken:           token,
		DockerfilePath:     repo.DockerfilePath,
		BuildContext:       repo.BuildContext,
		Destination:        destination,
		RegistrySecretName: registrySecret,
		KanikoImage:        s.cfg.KanikoImage,
		Resources:          s.cfg.Resources,
	}
	if err := s.k8s.CreateBuildJob(ctx, spec); err != nil {
		return nil, s.failBuild(ctx, record, err.Error())
	}

	running, err := s.builds.MarkRunning(ctx, record.ID, jobName)
	if err != nil {
		cleanupCtx := context.WithoutCancel(ctx)
		_ = s.k8s.DeleteBuildJob(cleanupCtx, s.cfg.Namespace, jobName)
		return nil, s.failBuild(cleanupCtx, record, err.Error())
	}
	return running, nil
}

// failBuild marks a build failed before it ever ran and returns the error to
// report. Leaving the row pending would strand it in the reconciler forever.
// WithoutCancel keeps a client disconnect from aborting the status write.
func (s *Service) failBuild(ctx context.Context, record *repository.BuildRow, reason string) error {
	reason = kubernetes.HumanizeClusterError(reason)
	persistCtx := context.WithoutCancel(ctx)
	if _, err := s.builds.Finish(persistCtx, record.ID, StatusFailed, reason); err != nil && s.logger != nil {
		s.logger.Error("could not mark build failed", zap.Error(err))
	}
	return connect.NewError(connect.CodeFailedPrecondition, errors.New(reason))
}

// ensureBuildRegistrySecret copies the project's push credential into the build
// namespace. Build Jobs run outside application namespaces, so the Secret the
// registry feature created there is not reachable from here.
func (s *Service) ensureBuildRegistrySecret(ctx context.Context, repo *repository.GitRepositoryRow) (string, error) {
	secretName := kubernetes.RegistrySecretName(repo.RegistryCredential)
	if err := s.k8s.CopyRegistrySecret(ctx, secretName, s.cfg.Namespace); err != nil {
		return "", fmt.Errorf(
			"registry credential %q is not available for pushing: %w", repo.RegistryCredential, err)
	}
	return secretName, nil
}

// ListBuilds returns a repository's build history.
func (s *Service) ListBuilds(ctx context.Context, req *idpv1.ListBuildsRequest) (*idpv1.ListBuildsResponse, error) {
	_, project, err := s.authorizeProject(ctx, req.ProjectSlug, false)
	if err != nil {
		return nil, err
	}

	repo, err := s.builds.GetGitRepository(ctx, project.ID, strings.ToLower(strings.TrimSpace(req.Name)))
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("git repository not found"))
	}

	limit := req.Limit
	if limit <= 0 || limit > 100 {
		limit = 30
	}

	rows, err := s.builds.ListBuilds(ctx, repo.ID, limit, req.Offset)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	total, err := s.builds.CountBuilds(ctx, repo.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := make([]*idpv1.Build, 0, len(rows))
	for i := range rows {
		out = append(out, buildToProto(&rows[i], repo.Name))
	}
	return &idpv1.ListBuildsResponse{
		Builds:         out,
		TotalCount:     total,
		BuildNamespace: s.cfg.Namespace,
	}, nil
}

// HandleWebhook authenticates a provider delivery and starts a build.
//
// It is called from a plain HTTP route rather than a Connect RPC: git providers
// post their own payload shapes and cannot be asked to speak Connect.
func (s *Service) HandleWebhook(ctx context.Context, repositoryID string, body []byte, headers WebhookHeaders) (*repository.BuildRow, error) {
	var id pgtype.UUID
	if err := id.Scan(repositoryID); err != nil {
		return nil, ErrRepositoryUnknown
	}

	repo, err := s.builds.GetGitRepositoryByID(ctx, id)
	if err != nil {
		return nil, ErrRepositoryUnknown
	}

	secret := ""
	if len(repo.WebhookSecretEncrypted) > 0 && s.box.Enabled() {
		if decrypted, decErr := s.box.Decrypt(repo.WebhookSecretEncrypted); decErr == nil {
			secret = decrypted
		}
	}
	// Verified before the payload is parsed: an unauthenticated caller must not
	// be able to reach any parsing logic at all.
	if err := VerifySignature(repo.Provider, secret, body, headers); err != nil {
		return nil, err
	}

	event, err := ParsePushEvent(repo.Provider, body, headers.EventType)
	if err != nil {
		return nil, err
	}
	if event.Deleted {
		return nil, ErrUnsupportedEvent
	}
	// A tag push yields no branch; building it under the default branch's name
	// would mislabel the resulting image.
	if event.Branch == "" {
		return nil, ErrUnsupportedEvent
	}
	// Only the configured branch auto-builds. Building every branch would let
	// any pushed branch deploy to the target environment.
	if !strings.EqualFold(event.Branch, repo.DefaultBranch) {
		return nil, ErrUnsupportedEvent
	}
	if err := s.requireK8s(); err != nil {
		return nil, err
	}

	return s.startBuild(ctx, repo, event.Branch, event.CommitSHA, TriggerWebhook, "webhook", pgtype.UUID{})
}

// Reconcile advances pending and running builds toward a terminal state, and
// runs the deploy step for those that succeeded.
//
// Polling rather than watching: a build finishes on the order of minutes, so a
// short poll is simpler than a watch that must be re-established after every
// disconnect, and it recovers automatically after a backend restart.
func (s *Service) Reconcile(ctx context.Context) {
	if s.k8s == nil {
		return
	}

	active, err := s.builds.ListActiveBuilds(ctx, 50)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("list active builds", zap.Error(err))
		}
		return
	}

	for i := range active {
		record := &active[i]
		if record.JobName == "" {
			s.resumePendingBuild(ctx, record)
			continue
		}

		status, err := s.k8s.GetBuildJobStatus(ctx, s.cfg.Namespace, record.JobName)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("build job status", zap.String("job", record.JobName), zap.Error(err))
			}
			continue
		}
		if !status.Finished {
			continue
		}

		if !status.Succeeded {
			reason := status.Reason
			if reason == "" {
				reason = "build failed"
			}
			if _, err := s.builds.Finish(ctx, record.ID, StatusFailed, reason); err != nil && s.logger != nil {
				s.logger.Error("finish failed build", zap.Error(err))
			}
			continue
		}

		if _, err := s.builds.Finish(ctx, record.ID, StatusSucceeded, ""); err != nil && s.logger != nil {
			s.logger.Error("finish build", zap.Error(err))
		}
		s.deployIfConfigured(ctx, record)
	}
}

const (
	pendingLaunchGrace = 15 * time.Second
	pendingLaunchTimeout = 3 * time.Minute
)

// resumePendingBuild finishes or relaunches builds whose Job was never recorded.
// This happens when the HTTP request is cancelled mid-launch or MarkRunning fails
// after the Job was already created.
func (s *Service) resumePendingBuild(ctx context.Context, record *repository.BuildRow) {
	if !record.CreatedAt.Valid {
		return
	}
	age := time.Since(record.CreatedAt.Time)
	if age < pendingLaunchGrace {
		return
	}

	repo, err := s.builds.GetGitRepositoryByID(ctx, record.RepositoryID)
	if err != nil {
		if age > pendingLaunchTimeout {
			_ = s.failBuild(ctx, record, "build launch did not complete; click Retry")
		}
		return
	}

	jobName := BuildJobName(repo.Name, record.Number)
	if _, err := s.k8s.GetBuildJobStatus(ctx, s.cfg.Namespace, jobName); err == nil {
		if _, markErr := s.builds.MarkRunning(ctx, record.ID, jobName); markErr != nil && s.logger != nil {
			s.logger.Warn("sync running build", zap.String("job", jobName), zap.Error(markErr))
		}
		return
	}

	if age > pendingLaunchTimeout {
		_ = s.failBuild(ctx, record, "build launch did not complete; click Retry")
		return
	}

	if _, err := s.launchBuildJob(ctx, repo, record, record.Branch, record.CommitSha); err != nil && s.logger != nil {
		s.logger.Warn("resume pending build", zap.Int64("build", record.Number), zap.Error(err))
	}
}

// deployIfConfigured runs the pipeline's final step. A deploy failure does not
// reopen the build: the image was built and pushed, which is what the build
// promised. The failure is logged and audited instead.
func (s *Service) deployIfConfigured(ctx context.Context, record *repository.BuildRow) {
	repo, err := s.builds.GetGitRepositoryByID(ctx, record.RepositoryID)
	if err != nil || !repo.AutoDeploy {
		return
	}
	if repo.TargetNamespace == nil || repo.TargetDeployment == nil {
		return
	}

	image := repo.ImageRepository + ":" + record.ImageTag
	changed, err := s.k8s.SetDeploymentImage(ctx, *repo.TargetNamespace, *repo.TargetDeployment, image)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("auto-deploy failed",
				zap.String("repository", repo.Name), zap.String("image", image), zap.Error(err))
		}
		s.recordSystemAudit(ctx, *repo.TargetNamespace, *repo.TargetDeployment, "failure",
			map[string]any{"error": err.Error(), "image": image})
		return
	}

	s.recordSystemAudit(ctx, *repo.TargetNamespace, *repo.TargetDeployment, "success",
		map[string]any{"image": image, "changed": changed, "build": record.Number})
}

// StartReconciler runs Reconcile on an interval until ctx is cancelled.
func (s *Service) StartReconciler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Reconcile(ctx)
		}
	}
}

// BuildImageTag renders a deterministic, Docker-legal tag.
//
// Branch names may contain slashes and other characters a tag may not, so they
// are sanitised rather than passed through. The build number is the fallback
// when no commit is known, which keeps tags unique for manual builds.
func BuildImageTag(branch, commitSHA string, number int64) string {
	safeBranch := dockerTagUnsafe.ReplaceAllString(strings.TrimSpace(branch), "-")
	safeBranch = strings.Trim(safeBranch, "-._")
	if safeBranch == "" {
		safeBranch = "build"
	}
	if len(safeBranch) > 40 {
		safeBranch = safeBranch[:40]
	}

	suffix := fmt.Sprintf("b%d", number)
	if sha := strings.TrimSpace(commitSHA); len(sha) >= 7 {
		suffix = sha[:7]
	}

	// A tag may not start with a separator and is capped at 128 characters.
	tag := strings.TrimLeft(safeBranch+"-"+suffix, "-._")
	if len(tag) > 128 {
		tag = tag[:128]
	}
	return tag
}

// BuildJobName renders the Kubernetes Job name for a build.
func BuildJobName(repositoryName string, number int64) string {
	name := fmt.Sprintf("build-%s-%d", repositoryName, number)
	if len(name) > 63 {
		// Job names are DNS labels; truncating keeps the number, which is what
		// makes the name unique.
		suffix := fmt.Sprintf("-%d", number)
		name = name[:63-len(suffix)] + suffix
	}
	return name
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	value, err := id.Value()
	if err != nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
