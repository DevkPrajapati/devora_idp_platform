-- name: CreateProject :one
INSERT INTO projects (slug, name, description, owner_id, owner_email, labels, status)
VALUES ($1, $2, $3, $4, $5, $6, 'active')
RETURNING *;

-- name: GetProjectBySlug :one
SELECT * FROM projects
WHERE slug = $1 AND status != 'deleted'
LIMIT 1;

-- name: GetProjectByID :one
SELECT * FROM projects
WHERE id = $1 AND status != 'deleted'
LIMIT 1;

-- name: ListProjects :many
SELECT * FROM projects
WHERE status != 'deleted'
ORDER BY created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: ListProjectsForMember :many
SELECT p.* FROM projects p
LEFT JOIN project_members m ON m.project_id = p.id
WHERE p.status != 'deleted'
  AND (p.owner_email = sqlc.arg('user_email') OR m.user_email = sqlc.arg('user_email'))
GROUP BY p.id
ORDER BY p.created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountProjects :one
SELECT COUNT(*) FROM projects WHERE status != 'deleted';

-- name: CountProjectsForMember :one
SELECT COUNT(DISTINCT p.id) FROM projects p
LEFT JOIN project_members m ON m.project_id = p.id
WHERE p.status != 'deleted'
  AND (p.owner_email = sqlc.arg('user_email') OR m.user_email = sqlc.arg('user_email'));

-- name: UpdateProject :one
UPDATE projects
SET name = $2,
    description = $3,
    labels = $4,
    updated_at = NOW()
WHERE slug = $1
RETURNING *;

-- name: DeleteProject :exec
UPDATE projects
SET status = 'deleted', updated_at = NOW()
WHERE slug = $1;

-- name: CountProjectMembers :one
SELECT COUNT(*) FROM project_members WHERE project_id = $1;

-- name: CountProjectNamespaces :one
SELECT COUNT(*) FROM namespaces
WHERE project_id = $1 AND status != 'deleted';

-- name: AddProjectMember :one
INSERT INTO project_members (project_id, user_id, user_email, role)
VALUES ($1, $2, $3, $4)
ON CONFLICT (project_id, user_email)
DO UPDATE SET role = EXCLUDED.role
RETURNING *;

-- name: RemoveProjectMember :exec
DELETE FROM project_members
WHERE project_id = $1 AND user_email = $2;

-- name: ListProjectMembers :many
SELECT * FROM project_members
WHERE project_id = $1
ORDER BY added_at ASC;

-- name: GetProjectMember :one
SELECT * FROM project_members
WHERE project_id = $1 AND user_email = $2
LIMIT 1;
