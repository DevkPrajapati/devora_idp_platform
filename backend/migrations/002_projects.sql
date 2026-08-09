-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS projects (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    description TEXT,
    owner_id    TEXT NOT NULL,
    owner_email TEXT NOT NULL,
    labels      JSONB NOT NULL DEFAULT '{}'::jsonb,
    status      TEXT NOT NULL DEFAULT 'active'
                CHECK (status IN ('active', 'archived', 'deleted')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_projects_owner_id ON projects(owner_id);
CREATE INDEX IF NOT EXISTS idx_projects_status ON projects(status);

CREATE TABLE IF NOT EXISTS project_members (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL,
    user_email TEXT NOT NULL,
    role       TEXT NOT NULL CHECK (role IN ('developer', 'viewer')),
    added_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, user_email)
);

CREATE INDEX IF NOT EXISTS idx_project_members_user_email ON project_members(user_email);

ALTER TABLE namespaces
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_namespaces_project_id ON namespaces(project_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE namespaces DROP COLUMN IF EXISTS project_id;
DROP TABLE IF EXISTS project_members;
DROP TABLE IF EXISTS projects;
-- +goose StatementEnd
