-- name: CreateProject :one
INSERT INTO projects (id, name, update_protocol, created_at)
VALUES ($1, $2, $3, current_timestamp)
RETURNING *;

-- name: GetProjectById :one
SELECT * FROM projects WHERE id = $1;
