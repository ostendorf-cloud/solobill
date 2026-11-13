-- Every query below runs inside a transaction a prior set_config call has
-- already scoped to one tenant (see postgres.Store.WithTenant). None of
-- them names tenant_id in a WHERE clause: that is deliberate, to prove
-- the point of this table's row level security policy rather than work
-- around it. A missing or wrong WHERE clause here cannot leak another
-- tenant's rows, because the database enforces the scope below these
-- queries regardless of what they ask for.

-- name: CreateAccount :one
INSERT INTO accounts (tenant_id, email, password_hash, totp_secret, role, status)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, tenant_id, email, password_hash, totp_secret, role, status, created_at, updated_at;

-- name: GetAccountByID :one
SELECT id, tenant_id, email, password_hash, totp_secret, role, status, created_at, updated_at
FROM accounts
WHERE id = $1;

-- name: GetAccountByEmail :one
SELECT id, tenant_id, email, password_hash, totp_secret, role, status, created_at, updated_at
FROM accounts
WHERE lower(email) = lower(sqlc.arg(email));

-- name: ListAccounts :many
SELECT id, tenant_id, email, password_hash, totp_secret, role, status, created_at, updated_at
FROM accounts
ORDER BY created_at;

-- name: UpdateAccount :one
UPDATE accounts
SET password_hash = $2,
    totp_secret = $3,
    role = $4,
    status = $5,
    updated_at = now()
WHERE id = $1
RETURNING id, tenant_id, email, password_hash, totp_secret, role, status, created_at, updated_at;
