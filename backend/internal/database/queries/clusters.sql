-- name: CreateCluster :one
INSERT INTO clusters (
    name, display_name, provider, status, kubeconfig_encrypted,
    server_url, kubernetes_version, node_count, is_active, last_error, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
RETURNING *;

-- name: GetCluster :one
SELECT * FROM clusters
WHERE id = $1
LIMIT 1;

-- name: GetClusterByName :one
SELECT * FROM clusters
WHERE name = $1
LIMIT 1;

-- name: GetActiveCluster :one
SELECT * FROM clusters
WHERE is_active = TRUE
LIMIT 1;

-- name: ListClusters :many
SELECT * FROM clusters
ORDER BY is_active DESC, created_at ASC;

-- name: CountClusters :one
SELECT COUNT(*) FROM clusters;

-- name: UpdateClusterStatus :one
UPDATE clusters
SET status     = $2,
    last_error = $3,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateClusterRuntime :one
UPDATE clusters
SET kubeconfig_encrypted = $2,
    server_url           = $3,
    kubernetes_version   = $4,
    node_count           = $5,
    status               = $6,
    last_error           = $7,
    updated_at           = NOW()
WHERE id = $1
RETURNING *;

-- name: ClearActiveCluster :exec
UPDATE clusters
SET is_active  = FALSE,
    updated_at = NOW()
WHERE is_active = TRUE;

-- name: SetClusterActive :one
UPDATE clusters
SET is_active  = TRUE,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: SetClusterProvider :one
-- Corrects a row whose provider was recorded wrongly. Only used to reclassify
-- a locally provisioned cluster that was seeded as 'imported', which left its
-- lifecycle inert: stop merely disconnected and delete only removed the row.
UPDATE clusters
SET provider   = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: SetClusterIdentity :one
-- Records which physical cluster answers behind this row. Written after a
-- successful connect, so a later mismatch is evidence the cluster was rebuilt.
UPDATE clusters
SET cluster_uid = $2,
    updated_at  = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteCluster :exec
DELETE FROM clusters
WHERE id = $1;
