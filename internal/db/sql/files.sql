-- name: GetFile :one
SELECT *
FROM files
WHERE id = ? LIMIT 1;

-- name: GetFileByPathAndSession :one
SELECT *
FROM files
WHERE path = ? AND session_id = ?
ORDER BY version DESC, created_at DESC
LIMIT 1;

-- name: ListFilesBySession :many
SELECT *
FROM files
WHERE session_id = ?
ORDER BY version ASC, created_at ASC;

-- name: ListFilesByPath :many
SELECT *
FROM files
WHERE path = ?
ORDER BY version DESC, created_at DESC;

-- name: GetMaxFileVersionByPath :one
SELECT version
FROM files
WHERE path = ?
ORDER BY version DESC, created_at DESC
LIMIT 1;

-- name: CreateFile :one
INSERT INTO files (
    id,
    session_id,
    path,
    content,
    version,
    created_at,
    updated_at
) VALUES (
    ?, ?, ?, ?, ?, strftime('%s', 'now'), strftime('%s', 'now')
)
RETURNING *;

-- name: DeleteFile :exec
DELETE FROM files
WHERE id = ?;

-- name: DeleteSessionFiles :exec
DELETE FROM files
WHERE session_id = ?;

-- name: ListLatestSessionFiles :many
SELECT f.*
FROM files f
INNER JOIN (
    SELECT f2.path, MAX(f2.version) as max_version, MAX(f2.created_at) as max_created_at
    FROM files f2
    WHERE f2.session_id = @session_id
    GROUP BY f2.path
) latest ON f.path = latest.path AND f.version = latest.max_version AND f.created_at = latest.max_created_at
WHERE f.session_id = @session_id
ORDER BY f.path;

-- name: ListNewFiles :many
SELECT *
FROM files
WHERE is_new = 1
ORDER BY version DESC, created_at DESC;
