-- +goose Up
-- +goose StatementBegin

-- A git repository the platform can build from. Scoped to a project so it
-- inherits that project's registry credentials for the push step.
CREATE TABLE IF NOT EXISTS git_repositories (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id               UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name                     TEXT NOT NULL
                             CHECK (name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$' AND length(name) <= 48),
    -- Determines how a webhook is authenticated and how its payload is parsed.
    provider                 TEXT NOT NULL DEFAULT 'generic'
                             CHECK (provider IN ('github', 'gitlab', 'bitbucket', 'generic')),
    clone_url                TEXT NOT NULL,
    default_branch           TEXT NOT NULL DEFAULT 'main',
    dockerfile_path          TEXT NOT NULL DEFAULT 'Dockerfile',
    build_context            TEXT NOT NULL DEFAULT '.',
    -- Where built images are pushed, e.g. ghcr.io/acme/api (no tag).
    image_repository         TEXT NOT NULL,
    -- registry_credentials.name used to authenticate the push. Deliberately not
    -- a foreign key: a credential can be renamed or recreated without orphaning
    -- the repo, and the build reports a clear error if it is missing.
    registry_credential      TEXT NOT NULL,
    -- Git access token for private clones. NULL for public repositories.
    -- AES-256-GCM envelope from internal/pkg/secretbox.
    token_encrypted          BYTEA,
    -- Shared secret for verifying inbound webhooks, same envelope format.
    webhook_secret_encrypted BYTEA,
    -- When true a successful build updates target_deployment's image.
    auto_deploy              BOOLEAN NOT NULL DEFAULT FALSE,
    target_namespace         TEXT,
    target_deployment        TEXT,
    created_by               TEXT NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, name),
    -- Auto-deploy without a target would silently do nothing.
    CONSTRAINT git_repositories_auto_deploy_target CHECK (
        auto_deploy = FALSE
        OR (target_namespace IS NOT NULL AND target_deployment IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_git_repositories_project_id ON git_repositories(project_id);

-- One row per build attempt. Retries create a new row rather than mutating the
-- old one, so the history shows that a retry happened and what it produced.
CREATE TABLE IF NOT EXISTS builds (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID NOT NULL REFERENCES git_repositories(id) ON DELETE CASCADE,
    -- Human-facing sequence, per repository. Assigned inside the insert so
    -- concurrent triggers cannot both claim the same number.
    number        BIGINT NOT NULL,
    branch        TEXT NOT NULL,
    commit_sha    TEXT NOT NULL DEFAULT '',
    image_tag     TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled')),
    trigger_type  TEXT NOT NULL DEFAULT 'manual'
                  CHECK (trigger_type IN ('manual', 'webhook', 'retry')),
    triggered_by  TEXT NOT NULL DEFAULT '',
    -- Kubernetes Job backing this build; also where its logs are read from.
    job_name      TEXT NOT NULL DEFAULT '',
    error_message TEXT,
    -- Set when this build was created by retrying another.
    retry_of      UUID REFERENCES builds(id) ON DELETE SET NULL,
    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (repository_id, number)
);

CREATE INDEX IF NOT EXISTS idx_builds_repository_created
    ON builds(repository_id, created_at DESC);
-- Supports the reconciler that advances pending/running builds.
CREATE INDEX IF NOT EXISTS idx_builds_active
    ON builds(status) WHERE status IN ('pending', 'running');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS builds;
DROP TABLE IF EXISTS git_repositories;
-- +goose StatementEnd
