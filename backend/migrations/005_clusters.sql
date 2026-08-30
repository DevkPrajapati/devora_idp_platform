-- +goose Up
-- +goose StatementBegin

-- Cluster fleet: every Kubernetes API the platform can drive. Kubeconfig is
-- stored encrypted (AES-256-GCM) the same way registry passwords are, so a
-- database dump does not hand over cluster admin.
CREATE TABLE IF NOT EXISTS clusters (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                 TEXT NOT NULL UNIQUE
                         CHECK (name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$' AND length(name) <= 48),
    display_name         TEXT NOT NULL,
    -- kind and minikube are provisioned locally; imported is an existing kubeconfig.
    provider             TEXT NOT NULL
                         CHECK (provider IN ('kind', 'minikube', 'imported')),
    status               TEXT NOT NULL
                         CHECK (status IN ('provisioning', 'running', 'stopped', 'error', 'deleting')),
    kubeconfig_encrypted BYTEA,
    server_url           TEXT,
    kubernetes_version   TEXT,
    node_count           INTEGER NOT NULL DEFAULT 0,
    is_active            BOOLEAN NOT NULL DEFAULT FALSE,
    last_error           TEXT,
    created_by           TEXT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_clusters_status ON clusters (status);
CREATE INDEX IF NOT EXISTS idx_clusters_active ON clusters (is_active) WHERE is_active;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS clusters;
-- +goose StatementEnd
