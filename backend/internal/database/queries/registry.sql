-- name: UpsertRegistryCredential :one
INSERT INTO registry_credentials (
    project_id, name, registry_url, username, password_encrypted, email, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (project_id, name) DO UPDATE
SET registry_url       = EXCLUDED.registry_url,
    username           = EXCLUDED.username,
    password_encrypted = EXCLUDED.password_encrypted,
    email              = EXCLUDED.email,
    updated_at         = NOW()
RETURNING *;

-- name: GetRegistryCredential :one
SELECT * FROM registry_credentials
WHERE project_id = $1 AND name = $2
LIMIT 1;

-- name: ListRegistryCredentials :many
SELECT * FROM registry_credentials
WHERE project_id = $1
ORDER BY name ASC;

-- name: DeleteRegistryCredential :exec
DELETE FROM registry_credentials
WHERE project_id = $1 AND name = $2;
