-- internal/schema/queries/users.sql
-- sqlc reads these query files alongside schema.sql and compiles them
-- into type-safe Go functions in internal/db/sqlc/.
--
-- Rules:
--   - Every query needs a name annotation:  -- name: FuncName :one/:many/:exec
--   - Use $1, $2 ... for parameters (Postgres style)
--   - sqlc catches type mismatches between query params and schema.sql at compile time

-- name: GetUserByID :one
SELECT id, uuid, email, role, is_active, created_at, updated_at
FROM users
WHERE id = $1
  AND deleted_at IS NULL;

-- name: GetUserByEmail :one
SELECT id, uuid, email, role, is_active, created_at, updated_at
FROM users
WHERE email = $1
  AND deleted_at IS NULL;

-- name: ListUsers :many
SELECT id, uuid, email, role, is_active, created_at
FROM users
WHERE deleted_at IS NULL
ORDER BY created_at DESC
LIMIT  $1
OFFSET $2;

-- name: CreateUser :one
INSERT INTO users (email, password_hash, role)
VALUES ($1, $2, $3)
RETURNING id, uuid, email, role, is_active, created_at;

-- name: UpdateUserRole :one
UPDATE users
SET    role       = $2,
       updated_at = now()
WHERE  id         = $1
  AND  deleted_at IS NULL
RETURNING id, email, role, updated_at;

-- name: SoftDeleteUser :exec
UPDATE users
SET deleted_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: HardDeleteUser :exec
-- ⚠️  Only call this in tests or GDPR erasure flows.
DELETE FROM users WHERE id = $1;
