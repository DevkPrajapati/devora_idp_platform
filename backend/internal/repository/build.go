package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/idp/platform/backend/internal/database"
	db "github.com/idp/platform/backend/internal/database/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// GitRepositoryRow is a registered repository. Token and webhook secret are
// AES-256-GCM envelopes; nothing in this layer decrypts them.
type GitRepositoryRow = db.GitRepository

// BuildRow is one build attempt.
type BuildRow = db.Build

// ErrRepositoryNotFound distinguishes a missing repository from a database
// failure, so the service can map it to NotFound rather than Internal.
var ErrRepositoryNotFound = errors.New("git repository not found")

// ErrBuildNotFound reports a missing build.
var ErrBuildNotFound = errors.New("build not found")

// BuildRepository persists git repositories and their builds.
type BuildRepository struct {
	queries *db.Queries
}

// NewBuildRepository creates a build repository.
func NewBuildRepository(pool *database.Pool) *BuildRepository {
	return &BuildRepository{queries: db.New(pool)}
}

// UpsertGitRepositoryInput holds repository write parameters.
type UpsertGitRepositoryInput struct {
	ProjectID          pgtype.UUID
	Name               string
	Provider           string
	CloneURL           string
	DefaultBranch      string
	DockerfilePath     string
	BuildContext       string
	ImageRepository    string
	RegistryCredential string
	// Nil leaves any stored value untouched, which is how an edit avoids
	// requiring the user to retype a secret the UI never received.
	TokenEncrypted         []byte
	WebhookSecretEncrypted []byte
	AutoDeploy             bool
	TargetNamespace        string
	TargetDeployment       string
	CreatedBy              string
}

// UpsertGitRepository stores a repository, replacing one with the same name.
func (r *BuildRepository) UpsertGitRepository(ctx context.Context, in UpsertGitRepositoryInput) (*GitRepositoryRow, error) {
	row, err := r.queries.UpsertGitRepository(ctx, db.UpsertGitRepositoryParams{
		ProjectID:              in.ProjectID,
		Name:                   in.Name,
		Provider:               in.Provider,
		CloneUrl:               in.CloneURL,
		DefaultBranch:          in.DefaultBranch,
		DockerfilePath:         in.DockerfilePath,
		BuildContext:           in.BuildContext,
		ImageRepository:        in.ImageRepository,
		RegistryCredential:     in.RegistryCredential,
		TokenEncrypted:         in.TokenEncrypted,
		WebhookSecretEncrypted: in.WebhookSecretEncrypted,
		AutoDeploy:             in.AutoDeploy,
		TargetNamespace:        optionalString(in.TargetNamespace),
		TargetDeployment:       optionalString(in.TargetDeployment),
		CreatedBy:              in.CreatedBy,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert git repository: %w", err)
	}
	return &row, nil
}

// GetGitRepository returns a repository by project and name.
func (r *BuildRepository) GetGitRepository(ctx context.Context, projectID pgtype.UUID, name string) (*GitRepositoryRow, error) {
	row, err := r.queries.GetGitRepository(ctx, db.GetGitRepositoryParams{
		ProjectID: projectID,
		Name:      name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRepositoryNotFound
		}
		return nil, fmt.Errorf("get git repository: %w", err)
	}
	return &row, nil
}

// GetGitRepositoryByID resolves a repository from a webhook URL, where the
// caller is unauthenticated and only knows the identifier.
func (r *BuildRepository) GetGitRepositoryByID(ctx context.Context, id pgtype.UUID) (*GitRepositoryRow, error) {
	row, err := r.queries.GetGitRepositoryByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRepositoryNotFound
		}
		return nil, fmt.Errorf("get git repository by id: %w", err)
	}
	return &row, nil
}

// ListGitRepositories returns a project's repositories.
func (r *BuildRepository) ListGitRepositories(ctx context.Context, projectID pgtype.UUID) ([]GitRepositoryRow, error) {
	rows, err := r.queries.ListGitRepositories(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list git repositories: %w", err)
	}
	return rows, nil
}

// DeleteGitRepository removes a repository. Its builds cascade.
func (r *BuildRepository) DeleteGitRepository(ctx context.Context, projectID pgtype.UUID, name string) error {
	if err := r.queries.DeleteGitRepository(ctx, db.DeleteGitRepositoryParams{
		ProjectID: projectID,
		Name:      name,
	}); err != nil {
		return fmt.Errorf("delete git repository: %w", err)
	}
	return nil
}

// CreateBuildInput holds build creation parameters.
type CreateBuildInput struct {
	RepositoryID pgtype.UUID
	Branch       string
	CommitSHA    string
	ImageTag     string
	TriggerType  string
	TriggeredBy  string
	RetryOf      pgtype.UUID
}

// CreateBuild records a new build attempt. The build number is assigned by the
// database so concurrent triggers cannot collide.
func (r *BuildRepository) CreateBuild(ctx context.Context, in CreateBuildInput) (*BuildRow, error) {
	row, err := r.queries.CreateBuild(ctx, db.CreateBuildParams{
		RepositoryID: in.RepositoryID,
		Branch:       in.Branch,
		CommitSha:    in.CommitSHA,
		ImageTag:     in.ImageTag,
		TriggerType:  in.TriggerType,
		TriggeredBy:  in.TriggeredBy,
		RetryOf:      in.RetryOf,
	})
	if err != nil {
		return nil, fmt.Errorf("create build: %w", err)
	}
	return &row, nil
}

// GetBuild returns a build by id.
func (r *BuildRepository) GetBuild(ctx context.Context, id pgtype.UUID) (*BuildRow, error) {
	row, err := r.queries.GetBuild(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBuildNotFound
		}
		return nil, fmt.Errorf("get build: %w", err)
	}
	return &row, nil
}

// GetBuildByNumber returns a build by its per-repository number, which is what
// the UI and URLs use.
func (r *BuildRepository) GetBuildByNumber(ctx context.Context, repositoryID pgtype.UUID, number int64) (*BuildRow, error) {
	row, err := r.queries.GetBuildByNumber(ctx, db.GetBuildByNumberParams{
		RepositoryID: repositoryID,
		Number:       number,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBuildNotFound
		}
		return nil, fmt.Errorf("get build by number: %w", err)
	}
	return &row, nil
}

// ListBuilds returns a repository's builds, newest first.
func (r *BuildRepository) ListBuilds(ctx context.Context, repositoryID pgtype.UUID, limit, offset int32) ([]BuildRow, error) {
	rows, err := r.queries.ListBuilds(ctx, db.ListBuildsParams{
		RepositoryID: repositoryID,
		Limit:        limit,
		Offset:       offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list builds: %w", err)
	}
	return rows, nil
}

// CountBuilds returns the total for pagination.
func (r *BuildRepository) CountBuilds(ctx context.Context, repositoryID pgtype.UUID) (int64, error) {
	count, err := r.queries.CountBuilds(ctx, repositoryID)
	if err != nil {
		return 0, fmt.Errorf("count builds: %w", err)
	}
	return count, nil
}

// ListActiveBuilds returns builds still pending or running, for the reconciler
// that advances them toward a terminal state.
func (r *BuildRepository) ListActiveBuilds(ctx context.Context, limit int32) ([]BuildRow, error) {
	rows, err := r.queries.ListActiveBuilds(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list active builds: %w", err)
	}
	return rows, nil
}

// MarkRunning records that a build's Job was created.
func (r *BuildRepository) MarkRunning(ctx context.Context, id pgtype.UUID, jobName string) (*BuildRow, error) {
	row, err := r.queries.MarkBuildRunning(ctx, db.MarkBuildRunningParams{ID: id, JobName: jobName})
	if err != nil {
		return nil, fmt.Errorf("mark build running: %w", err)
	}
	return &row, nil
}

// Finish moves a build to a terminal status.
func (r *BuildRepository) Finish(ctx context.Context, id pgtype.UUID, status, errorMessage string) (*BuildRow, error) {
	row, err := r.queries.FinishBuild(ctx, db.FinishBuildParams{
		ID:           id,
		Status:       status,
		ErrorMessage: optionalString(errorMessage),
	})
	if err != nil {
		return nil, fmt.Errorf("finish build: %w", err)
	}
	return &row, nil
}

// SetCommit records the resolved commit and image tag.
func (r *BuildRepository) SetCommit(ctx context.Context, id pgtype.UUID, commitSHA, imageTag string) error {
	if err := r.queries.SetBuildCommit(ctx, db.SetBuildCommitParams{
		ID:        id,
		CommitSha: commitSHA,
		ImageTag:  imageTag,
	}); err != nil {
		return fmt.Errorf("set build commit: %w", err)
	}
	return nil
}
