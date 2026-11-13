-- Same reasoning as accounts.sql: every query here relies on the row
-- level security policy for its tenant isolation, not on a WHERE clause
-- repeating the tenant id.

-- name: CreateInvite :one
INSERT INTO invites (tenant_id, email, role, token_hash, expires_at, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, tenant_id, email, role, token_hash, expires_at, used_at, created_by, created_at;

-- name: GetInviteByID :one
SELECT id, tenant_id, email, role, token_hash, expires_at, used_at, created_by, created_at
FROM invites
WHERE id = $1;

-- name: GetInviteByTokenHash :one
SELECT id, tenant_id, email, role, token_hash, expires_at, used_at, created_by, created_at
FROM invites
WHERE token_hash = $1;

-- MarkInviteUsed is guarded by "AND used_at IS NULL" so that two concurrent
-- accept attempts for the same invite cannot both succeed: Postgres takes
-- the row lock for the first UPDATE that reaches it, and the second one
-- blocks on that lock, then re-evaluates the WHERE clause against the now
-- committed row and matches zero rows. A zero-row RETURNING answers
-- pgx.ErrNoRows, which internal/storage/postgres maps to storage.ErrConflict
-- rather than to the generic read-error path, so a caller can tell "already
-- used" apart from every other failure.
--
-- name: MarkInviteUsed :one
UPDATE invites
SET used_at = $2
WHERE id = $1 AND used_at IS NULL
RETURNING id, tenant_id, email, role, token_hash, expires_at, used_at, created_by, created_at;
