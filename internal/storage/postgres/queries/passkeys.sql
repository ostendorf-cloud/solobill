-- The storage side of govue's passkey.Store (issue #86): see
-- storage.PasskeyRepository. Tenant scoping is the same two layers as
-- accounts.sql.

-- name: LockPasskeyAccount :one
-- Locks the account row for the rest of the transaction. Add and Delete
-- both take it first, so a first registration racing another first
-- registration, or two deletes racing for the last passkey, are
-- serialised. Locking the credential rows instead would lock nothing for
-- an account that has none yet.
SELECT id
FROM accounts
WHERE id = sqlc.arg(id)
  AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
FOR UPDATE;

-- name: GetPasskeyUserHandle :one
SELECT handle
FROM passkey_user_handles
WHERE account_id = sqlc.arg(account_id)
  AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- name: CreatePasskeyUserHandle :exec
INSERT INTO passkey_user_handles (tenant_id, account_id, handle)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, sqlc.arg(account_id), sqlc.arg(handle));

-- name: DeletePasskeyUserHandle :exec
DELETE FROM passkey_user_handles
WHERE account_id = sqlc.arg(account_id)
  AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- name: CreatePasskeyCredential :exec
INSERT INTO passkey_credentials (credential_id, tenant_id, account_id, name, sign_count, data, created_at, last_used_at)
VALUES (
    sqlc.arg(credential_id),
    NULLIF(current_setting('app.tenant_id', true), '')::uuid,
    sqlc.arg(account_id),
    sqlc.arg(name),
    sqlc.arg(sign_count),
    sqlc.arg(data),
    sqlc.arg(created_at),
    sqlc.narg(last_used_at)
);

-- name: ListPasskeyCredentials :many
SELECT c.credential_id, c.account_id, h.handle, c.name, c.sign_count, c.data, c.created_at, c.last_used_at
FROM passkey_credentials c
JOIN passkey_user_handles h ON h.account_id = c.account_id AND h.tenant_id = c.tenant_id
WHERE c.account_id = sqlc.arg(account_id)
  AND c.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
ORDER BY c.created_at, c.seq;

-- name: CountPasskeyCredentials :one
SELECT count(*)
FROM passkey_credentials
WHERE account_id = sqlc.arg(account_id)
  AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- name: GetPasskeyAccountByUserHandle :one
-- Only an active account answers: a pending or disabled one must not log
-- in with a passkey either.
SELECT a.id, a.email
FROM passkey_user_handles h
JOIN accounts a ON a.id = h.account_id AND a.tenant_id = h.tenant_id
WHERE h.handle = sqlc.arg(handle)
  AND a.status = 'active'
  AND h.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- name: UpdatePasskeyCredential :execrows
UPDATE passkey_credentials
SET sign_count = sqlc.arg(sign_count),
    last_used_at = sqlc.narg(last_used_at),
    data = sqlc.arg(data)
WHERE credential_id = sqlc.arg(credential_id)
  AND account_id = sqlc.arg(account_id)
  AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
  AND sign_count = sqlc.arg(old_sign_count);

-- name: DeletePasskeyCredential :execrows
DELETE FROM passkey_credentials
WHERE credential_id = sqlc.arg(credential_id)
  AND account_id = sqlc.arg(account_id)
  AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;
