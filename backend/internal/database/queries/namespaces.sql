-- name: CreateNamespace :one
INSERT INTO namespaces (
    name,
    display_name,
    description,
    owner_id,
    owner_email,
    labels,
    annotations,
    status,
    project_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
) RETURNING *;

-- name: SetNamespaceProject :one
-- Attaches an existing namespace to a project so credential and config
-- lookups can resolve namespace -> project.
UPDATE namespaces
SET project_id = $2, updated_at = NOW()
WHERE name = $1 AND status != 'deleted'
RETURNING *;

-- name: ListNamespacesByProject :many
SELECT * FROM namespaces
WHERE project_id = $1 AND status != 'deleted'
ORDER BY name ASC;

-- name: GetNamespaceByName :one
SELECT * FROM namespaces
WHERE name = $1 AND status != 'deleted'
LIMIT 1;

-- name: ListNamespaces :many
SELECT *
FROM namespaces
WHERE status != 'deleted'
    AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
    AND (sqlc.narg('owner_email')::text IS NULL OR owner_email = sqlc.narg('owner_email'))
ORDER BY created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountNamespaces :one
SELECT COUNT(*)
FROM namespaces
WHERE status != 'deleted'
    AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
    AND (sqlc.narg('owner_email')::text IS NULL OR owner_email = sqlc.narg('owner_email'));

-- name: UpdateNamespaceStatus :one
UPDATE namespaces
SET status = $2, updated_at = NOW()
WHERE name = $1
RETURNING *;

-- name: DeleteNamespace :exec
UPDATE namespaces
SET status = 'deleted', updated_at = NOW()
WHERE name = $1;
