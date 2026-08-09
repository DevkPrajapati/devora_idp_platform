-- name: UpsertGitRepository :one
INSERT INTO git_repositories (
    project_id, name, provider, clone_url, default_branch,
    dockerfile_path, build_context, image_repository, registry_credential,
    token_encrypted, webhook_secret_encrypted,
    auto_deploy, target_namespace, target_deployment, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
)
ON CONFLICT (project_id, name) DO UPDATE
SET provider            = EXCLUDED.provider,
    clone_url           = EXCLUDED.clone_url,
    default_branch      = EXCLUDED.default_branch,
    dockerfile_path     = EXCLUDED.dockerfile_path,
    build_context       = EXCLUDED.build_context,
    image_repository    = EXCLUDED.image_repository,
    registry_credential = EXCLUDED.registry_credential,
    -- COALESCE keeps the stored secret when the caller sends none, so editing
    -- a repository does not require retyping a token the UI never received.
    token_encrypted          = COALESCE(EXCLUDED.token_encrypted, git_repositories.token_encrypted),
    webhook_secret_encrypted = COALESCE(EXCLUDED.webhook_secret_encrypted, git_repositories.webhook_secret_encrypted),
    auto_deploy         = EXCLUDED.auto_deploy,
    target_namespace    = EXCLUDED.target_namespace,
    target_deployment   = EXCLUDED.target_deployment,
    updated_at          = NOW()
RETURNING *;

-- name: GetGitRepository :one
SELECT * FROM git_repositories
WHERE project_id = $1 AND name = $2
LIMIT 1;

-- name: GetGitRepositoryByID :one
SELECT * FROM git_repositories WHERE id = $1 LIMIT 1;

-- name: ListGitRepositories :many
SELECT * FROM git_repositories
WHERE project_id = $1
ORDER BY name ASC;

-- name: DeleteGitRepository :exec
DELETE FROM git_repositories WHERE project_id = $1 AND name = $2;

-- name: CreateBuild :one
-- The build number is derived inside the insert rather than read first, so two
-- concurrent triggers cannot both compute the same next number.
INSERT INTO builds (
    repository_id, number, branch, commit_sha, image_tag,
    status, trigger_type, triggered_by, retry_of
)
SELECT
    sqlc.arg('repository_id')::uuid,
    COALESCE(MAX(number), 0) + 1,
    sqlc.arg('branch')::text,
    sqlc.arg('commit_sha')::text,
    sqlc.arg('image_tag')::text,
    'pending',
    sqlc.arg('trigger_type')::text,
    sqlc.arg('triggered_by')::text,
    sqlc.narg('retry_of')::uuid
FROM builds
WHERE repository_id = sqlc.arg('repository_id')::uuid
RETURNING *;

-- name: GetBuild :one
SELECT * FROM builds WHERE id = $1 LIMIT 1;

-- name: GetBuildByNumber :one
SELECT * FROM builds WHERE repository_id = $1 AND number = $2 LIMIT 1;

-- name: ListBuilds :many
SELECT * FROM builds
WHERE repository_id = $1
ORDER BY number DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountBuilds :one
SELECT COUNT(*) FROM builds WHERE repository_id = $1;

-- name: ListActiveBuilds :many
SELECT * FROM builds
WHERE status IN ('pending', 'running')
ORDER BY created_at ASC
LIMIT $1;

-- name: MarkBuildRunning :one
UPDATE builds
SET status = 'running', job_name = $2, started_at = COALESCE(started_at, NOW())
WHERE id = $1
RETURNING *;

-- name: FinishBuild :one
UPDATE builds
SET status = $2, error_message = $3, finished_at = NOW()
WHERE id = $1
RETURNING *;

-- name: SetBuildCommit :exec
UPDATE builds SET commit_sha = $2, image_tag = $3 WHERE id = $1;
