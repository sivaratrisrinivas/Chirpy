-- name: CreateChirp :one
INSERT INTO chirps (id, created_at, updated_at, body, user_id)
VALUES (
    gen_random_uuid(),
    NOW(),
    NOW(),
    $1,
    $2
)
RETURNING *;

-- name: ListChirpsAsc :many
-- Keyset pagination: rows strictly after (cursor_created_at, cursor_id).
SELECT * FROM chirps
WHERE (sqlc.narg('author_id')::uuid IS NULL OR user_id = sqlc.narg('author_id')::uuid)
  AND (sqlc.narg('cursor_created_at')::timestamp IS NULL
       OR (created_at, id) > (sqlc.narg('cursor_created_at')::timestamp, sqlc.narg('cursor_id')::uuid))
ORDER BY created_at ASC, id ASC
LIMIT sqlc.arg('row_limit');

-- name: ListChirpsDesc :many
SELECT * FROM chirps
WHERE (sqlc.narg('author_id')::uuid IS NULL OR user_id = sqlc.narg('author_id')::uuid)
  AND (sqlc.narg('cursor_created_at')::timestamp IS NULL
       OR (created_at, id) < (sqlc.narg('cursor_created_at')::timestamp, sqlc.narg('cursor_id')::uuid))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit');

-- name: GetChirp :one
SELECT * FROM chirps
WHERE id = $1;

-- name: DeleteChirpByOwner :execrows
DELETE FROM chirps
WHERE id = $1 AND user_id = $2;
