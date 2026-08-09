-- name: CreateAuditLog :one
INSERT INTO audit_logs (
    user_id,
    user_email,
    action,
    namespace,
    resource,
    resource_type,
    result,
    details,
    ip_address
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
) RETURNING *;

-- name: ListAuditLogs :many
SELECT *
FROM audit_logs
WHERE
    (sqlc.narg('namespace')::text IS NULL OR namespace = sqlc.narg('namespace'))
    AND (sqlc.narg('user_id')::text IS NULL OR user_id = sqlc.narg('user_id'))
    AND (sqlc.narg('action')::text IS NULL OR action = sqlc.narg('action'))
ORDER BY created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountAuditLogs :one
SELECT COUNT(*)
FROM audit_logs
WHERE
    (sqlc.narg('namespace')::text IS NULL OR namespace = sqlc.narg('namespace'))
    AND (sqlc.narg('user_id')::text IS NULL OR user_id = sqlc.narg('user_id'))
    AND (sqlc.narg('action')::text IS NULL OR action = sqlc.narg('action'));
