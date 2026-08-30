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
    project_id,
    cluster_uid
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
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

-- name: SetNamespaceClusterUID :exec
-- Backfills provenance for a namespace confirmed to exist in the live cluster.
-- Rows created before identity tracking, and rows adopted after a reconnect,
-- both land here.
UPDATE namespaces
SET cluster_uid = $2, updated_at = NOW()
WHERE name = $1 AND status != 'deleted';

-- name: OrphanNamespacesFromOtherClusters :many
-- Retires namespace records belonging to a cluster that no longer exists.
--
-- Called when a reconnect proves the cluster behind a fleet row was rebuilt:
-- every namespace provisioned into the old cluster died with it.
UPDATE namespaces
SET status = 'deleted', updated_at = NOW()
WHERE status != 'deleted'
    AND cluster_uid IS NOT NULL
    AND cluster_uid != sqlc.arg('cluster_uid')
RETURNING name;

-- name: RetireNamespacesOfCluster :many
-- Retires every namespace that was adopted onto a specific cluster. Used when
-- that cluster is deleted, so a later recreation does not inherit its records.
UPDATE namespaces
SET status = 'deleted', updated_at = NOW()
WHERE status != 'deleted'
    AND cluster_uid = sqlc.arg('cluster_uid')
RETURNING name;

-- name: RetireUnprovenancedNamespaces :many
-- Retires namespace rows that were never fingerprinted. Safe only when the
-- caller already knows the live cluster is new (rebuild or last-cluster delete):
-- those rows cannot be proven to belong to it, and leaving them is how deleted
-- namespaces kept appearing after a recreate.
UPDATE namespaces
SET status = 'deleted', updated_at = NOW()
WHERE status != 'deleted'
    AND cluster_uid IS NULL
RETURNING name;
