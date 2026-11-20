-- migrate:up

-- Passkeys (WebAuthn credentials) of a local account, for govue's passkey
-- package (issue #86). govue owns no schema: internal/auth.PasskeyStore
-- adapts these two tables to passkey.Store, and passkeytest.Run proves the
-- adapter against a real PostgreSQL in the integration job.
--
-- Issue #86 runs before the year end milestone and has no row in the
-- "Migration timestamp reservation" table of docs/concepts/solobill.md. Its
-- versions sit below the first reserved range (20261121100000), so the
-- tables of the milestone can reference these.
--
-- No change to accounts itself. The adoption of govue's account package
-- needs no data migration: the password hashes are already Argon2id PHC
-- strings with govue's parameters, and the TOTP secrets are already base32
-- of 160 bits. accounts.totp_last_counter keeps its meaning, with one
-- addition the adapter applies in its queries rather than here: a NULL
-- counter, which every account invited or seeded before #86 may have,
-- reads as 1, a confirmed secret, through COALESCE(totp_last_counter, 1).
-- An explicit 0 is an unconfirmed secret, which only govue's Store.Create
-- writes. See internal/storage/postgres/queries/login_accounts.sql.

-- passkey_user_handles holds the one random WebAuthn user handle of an
-- account while it holds at least one passkey. govue's passkey.Store
-- contract: an account has exactly one handle while it holds credentials,
-- a handle belongs to one account, and the handle goes with the last
-- credential. One row per account (the primary key) and a unique handle
-- across every tenant (passkey_user_handles_handle_key) are both enforced
-- here, not only in Go.
--
-- It is a table of its own instead of a column on accounts: the
-- integration tests decode "SELECT * FROM accounts" into a fixed struct by
-- column name, and a handle is passkey state, not account state.
CREATE TABLE passkey_user_handles (
    tenant_id uuid NOT NULL,
    account_id uuid PRIMARY KEY,
    -- WebAuthn allows at most 64 bytes. govue generates 32 random bytes.
    handle bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT passkey_user_handles_handle_key UNIQUE (handle),
    CONSTRAINT passkey_user_handles_handle_length CHECK (length(handle) BETWEEN 1 AND 64),
    -- The composite target accounts_tenant_id_id_unique exists for exactly
    -- this: the row cannot point at an account of another tenant.
    CONSTRAINT passkey_user_handles_account_fkey FOREIGN KEY (tenant_id, account_id)
        REFERENCES accounts (tenant_id, id) ON DELETE CASCADE
);

-- passkey_credentials holds one row per registered passkey. The
-- credential id is the authenticator's own and unique across every
-- account and tenant (the primary key): passkey.Store.AddCredential
-- answers ErrConflict for a taken id whoever holds it.
CREATE TABLE passkey_credentials (
    credential_id bytea PRIMARY KEY,
    tenant_id uuid NOT NULL,
    account_id uuid NOT NULL,
    name text NOT NULL DEFAULT '',
    -- The authenticator's signature counter. A uint32 in WebAuthn, so a
    -- bigint holds every value.
    sign_count bigint NOT NULL DEFAULT 0,
    -- The rest of the credential, public key included, in a form only
    -- govue's passkey package reads. No secret: the private key never
    -- leaves the authenticator.
    data bytea NOT NULL,
    created_at timestamptz NOT NULL,
    last_used_at timestamptz,
    -- Insertion order, the tie breaker of "oldest first" for two passkeys
    -- registered within the same microsecond.
    seq bigint GENERATED ALWAYS AS IDENTITY,
    CONSTRAINT passkey_credentials_id_length CHECK (length(credential_id) BETWEEN 1 AND 1023),
    CONSTRAINT passkey_credentials_sign_count_range CHECK (sign_count BETWEEN 0 AND 4294967295),
    CONSTRAINT passkey_credentials_account_fkey FOREIGN KEY (tenant_id, account_id)
        REFERENCES accounts (tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX passkey_credentials_account_idx ON passkey_credentials (tenant_id, account_id, created_at, seq);

-- Row level security, the same policy shape as every other tenant scoped
-- table: see 20261112233249_create_accounts.sql for why ENABLE and FORCE
-- are both needed and why NULLIF folds a reverted setting to NULL.
ALTER TABLE passkey_user_handles ENABLE ROW LEVEL SECURITY;
ALTER TABLE passkey_user_handles FORCE ROW LEVEL SECURITY;

CREATE POLICY passkey_user_handles_tenant_isolation ON passkey_user_handles
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE passkey_credentials ENABLE ROW LEVEL SECURITY;
ALTER TABLE passkey_credentials FORCE ROW LEVEL SECURITY;

CREATE POLICY passkey_credentials_tenant_isolation ON passkey_credentials
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- migrate:down

DROP TABLE passkey_credentials;
DROP TABLE passkey_user_handles;
