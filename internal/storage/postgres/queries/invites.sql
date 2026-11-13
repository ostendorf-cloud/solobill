-- Same reasoning as accounts.sql: tenant isolation here is two independent
-- layers, matching docs/concepts/solobill.md. Every query below names
-- tenant_id in its own WHERE clause (or, for CreateInvite, as a column
-- value) as the first, defense-in-depth layer; row level security (see
-- db/migrations) is the independent second layer, enforcing the same
-- scope below these queries regardless of what they ask for.

-- name: CreateInvite :one
INSERT INTO invites (tenant_id, email, role, token_hash, expires_at, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, tenant_id, email, role, token_hash, expires_at, used_at, created_by, created_at, totp_secret;

-- name: GetInviteByID :one
SELECT id, tenant_id, email, role, token_hash, expires_at, used_at, created_by, created_at, totp_secret
FROM invites
WHERE id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- name: GetInviteByTokenHash :one
SELECT id, tenant_id, email, role, token_hash, expires_at, used_at, created_by, created_at, totp_secret
FROM invites
WHERE token_hash = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

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
WHERE id = $1 AND used_at IS NULL AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
RETURNING id, tenant_id, email, role, token_hash, expires_at, used_at, created_by, created_at, totp_secret;

-- SetInviteTOTPSecret is guarded by "AND used_at IS NULL", the same way
-- MarkInviteUsed is: a secret can never be attached to an invite that has
-- already been redeemed. internal/storage/postgres maps a zero-row
-- RETURNING here to storage.ErrConflict too, exactly like MarkUsed.
--
-- name: SetInviteTOTPSecret :one
UPDATE invites
SET totp_secret = $2
WHERE id = $1 AND used_at IS NULL AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
RETURNING id, tenant_id, email, role, token_hash, expires_at, used_at, created_by, created_at, totp_secret;
