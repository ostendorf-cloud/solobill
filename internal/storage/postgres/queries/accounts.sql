-- Tenant isolation here is two independent layers, matching
-- docs/concepts/solobill.md: every query below names tenant_id in its own
-- WHERE clause (or, for CreateAccount, as a column value), AND row level
-- security (see db/migrations) enforces the same scope below these
-- queries regardless of what they ask for. Neither layer is trusted alone:
-- a missing or wrong WHERE clause here is still caught by the database's
-- policy, and the policy itself is still a backstop, not the only check,
-- so a bug in it does not immediately leak another tenant's rows either.
-- The transaction these queries run in has already been scoped to one
-- tenant via set_config('app.tenant_id', ...) by postgres.Store.WithTenant;
-- current_setting(..., true)::uuid reads that same value back.

-- name: CreateAccount :one
INSERT INTO accounts (tenant_id, email, password_hash, totp_secret, totp_last_counter, oidc_subject, role, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, tenant_id, email, password_hash, totp_secret, totp_last_counter, oidc_subject, role, status, created_at, updated_at;

-- name: GetAccountByID :one
SELECT id, tenant_id, email, password_hash, totp_secret, totp_last_counter, oidc_subject, role, status, created_at, updated_at
FROM accounts
WHERE id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- name: GetAccountByEmail :one
SELECT id, tenant_id, email, password_hash, totp_secret, totp_last_counter, oidc_subject, role, status, created_at, updated_at
FROM accounts
WHERE lower(email) = lower(sqlc.arg(email)) AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- name: GetAccountByOIDCSubject :one
SELECT id, tenant_id, email, password_hash, totp_secret, totp_last_counter, oidc_subject, role, status, created_at, updated_at
FROM accounts
WHERE oidc_subject = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- name: ListAccounts :many
SELECT id, tenant_id, email, password_hash, totp_secret, totp_last_counter, oidc_subject, role, status, created_at, updated_at
FROM accounts
WHERE tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
ORDER BY created_at;

-- name: UpdateAccount :one
UPDATE accounts
SET password_hash = $2,
    totp_secret = $3,
    totp_last_counter = $4,
    oidc_subject = $5,
    role = $6,
    status = $7,
    updated_at = now()
WHERE id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
RETURNING id, tenant_id, email, password_hash, totp_secret, totp_last_counter, oidc_subject, role, status, created_at, updated_at;
