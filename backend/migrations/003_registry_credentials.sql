-- +goose Up
-- +goose StatementBegin

-- Registry credentials are stored per project and materialised as a
-- kubernetes.io/dockerconfigjson Secret in every namespace the project owns.
-- The password never lands here in the clear: password_encrypted holds an
-- AES-256-GCM envelope produced by internal/pkg/secretbox, so a database dump
-- or a replica leak does not hand over registry access.
CREATE TABLE IF NOT EXISTS registry_credentials (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id         UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    -- Short handle chosen by the user, e.g. "dockerhub". Also forms the
    -- Kubernetes Secret name, hence the RFC 1123 label constraint.
    name               TEXT NOT NULL
                       CHECK (name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$' AND length(name) <= 48),
    registry_url       TEXT NOT NULL,
    username           TEXT NOT NULL,
    password_encrypted BYTEA NOT NULL,
    email              TEXT,
    created_by         TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, name)
);

CREATE INDEX IF NOT EXISTS idx_registry_credentials_project_id
    ON registry_credentials(project_id);

-- namespaces.project_id was added in 002 but never populated, leaving no path
-- from a deployment's namespace back to the project that owns its credentials.
-- Index it so the per-deployment lookup stays cheap.
CREATE INDEX IF NOT EXISTS idx_namespaces_project_id_active
    ON namespaces(project_id) WHERE status != 'deleted';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_namespaces_project_id_active;
DROP TABLE IF EXISTS registry_credentials;
-- +goose StatementEnd
