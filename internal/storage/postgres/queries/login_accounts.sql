-- The account side of govue's account.Store (issue #86): see
-- storage.LoginAccountRepository. Tenant scoping is the same two layers as
-- accounts.sql: every statement names the scoped tenant in its WHERE
-- clause (or as the inserted value), and row level security enforces it
-- below.
--
-- COALESCE(totp_last_counter, 1): an account invited or seeded before #86
-- may have a NULL counter, and its secret is confirmed. Reading NULL as 1
-- keeps it logging in without a password reset (govue's account.Record doc
-- names this mapping). Counter 1 is the first minutes of 1970, so no live
-- code is refused. Only CreateLoginMember writes an explicit 0, an
-- unconfirmed secret.
--
-- Every UPDATE below compares and writes in one statement. Reading the row
-- and then writing it would let two racing logins both consume one code.

-- name: GetActiveLoginAccountByEmail :one
SELECT id, email, password_hash, totp_secret, COALESCE(totp_last_counter, 1)::bigint AS totp_counter
FROM accounts
WHERE lower(email) = lower(sqlc.arg(email))
  AND status = 'active'
  AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- name: CreateLoginMember :one
INSERT INTO accounts (tenant_id, email, password_hash, totp_secret, totp_last_counter, role, status)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, sqlc.arg(email), sqlc.narg(password_hash), sqlc.narg(totp_secret), 0, 'member', 'active')
RETURNING id, email, password_hash, totp_secret, COALESCE(totp_last_counter, 1)::bigint AS totp_counter;

-- name: AdvanceLoginTOTPCounter :execrows
UPDATE accounts
SET totp_last_counter = sqlc.arg(counter), updated_at = now()
WHERE id = sqlc.arg(id)
  AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
  AND COALESCE(totp_last_counter, 1) > 0
  AND COALESCE(totp_last_counter, 1) < sqlc.arg(counter);

-- name: ConfirmLoginTOTP :execrows
UPDATE accounts
SET totp_last_counter = sqlc.arg(counter), updated_at = now()
WHERE id = sqlc.arg(id)
  AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
  AND totp_secret = sqlc.arg(secret)
  AND totp_last_counter = 0
  AND sqlc.arg(counter)::bigint > 0;

-- name: ReplaceLoginTOTPSecret :execrows
UPDATE accounts
SET totp_secret = sqlc.arg(secret), updated_at = now()
WHERE id = sqlc.arg(id)
  AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
  AND totp_last_counter = 0;

-- name: SetLoginPasswordHash :execrows
UPDATE accounts
SET password_hash = sqlc.arg(new_hash), updated_at = now()
WHERE id = sqlc.arg(id)
  AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
  AND password_hash IS NOT NULL
  AND password_hash <> ''
  AND password_hash = sqlc.arg(old_hash);
