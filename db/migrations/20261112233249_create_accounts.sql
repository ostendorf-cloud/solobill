-- migrate:up

-- accounts holds local (non-OIDC) logins. An invitee exists as a row here,
-- in status 'pending' with no password and no TOTP secret yet, before they
-- ever sign in; internal/auth (a later step) moves it to 'active' once both
-- are set, or to 'disabled' when access is revoked.
CREATE TABLE accounts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    email text NOT NULL,
    -- Nullable: an invited account has no password until the invitee
    -- accepts the invite and sets one. Holds an already-computed argon2id
    -- hash string; hashing itself is internal/auth's job, not this one's.
    password_hash text,
    -- Nullable until the invitee enrolls TOTP. Stored as the base32 secret
    -- text a TOTP library issues and verifies against directly (RFC 4648 /
    -- the otpauth URI convention), not raw bytes: it saves the adapter a
    -- bytea<->base32 round trip on every single login check, the common
    -- case, at the cost of one on enrollment, the rare case. Generating and
    -- verifying TOTP codes is internal/auth's job, not this one's. The
    -- length check is defense in depth: the only writer is always a
    -- server-generated value (auth.GenerateTOTPSecret, 20 bytes base32,
    -- no padding, which is always 32 characters), never anything a client
    -- supplies directly.
    totp_secret text,
    -- Nullable: set once a login succeeds with a valid TOTP code, to the
    -- RFC 6238 counter that code matched, so a later login can refuse a
    -- replay of the exact same code (internal/auth.LocalAuthenticator.Login
    -- and InviteManager.AcceptInvite both update this). RFC 6238 Section
    -- 5.2 requires a verifier to reject a second use of an already
    -- consumed OTP.
    totp_last_counter bigint,
    -- Nullable: set when this account is provisioned for an OIDC login,
    -- to the identity provider's stable subject claim (never the email or
    -- username, both of which can be reassigned to somebody else; see
    -- govue's oidc.Identity doc comment). internal/auth.ResolveOIDCIdentity
    -- matches on this, not on email. Provisioning it onto an account is a
    -- later step's job (see internal/auth's oidc.go); every local account
    -- has it NULL.
    oidc_subject text,
    CONSTRAINT accounts_totp_secret_length CHECK (totp_secret IS NULL OR length(totp_secret) = 32),
    -- Deliberately not constrained to a fixed set yet: the full role model
    -- belongs to internal/auth (a later step). 'owner' and 'member' are the
    -- two roles the invite flow already needs: an owner can invite, a
    -- member cannot.
    role text NOT NULL CHECK (role IN ('owner', 'member')),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active', 'disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT accounts_email_not_empty CHECK (email <> ''),
    -- id alone already is a unique primary key, so this constraint adds no
    -- new uniqueness; it exists only so that (tenant_id, id) together is a
    -- valid foreign key target. invites.created_by, two migrations down,
    -- references accounts through exactly that composite, tenant_id
    -- included, so the foreign key itself cannot point at an account in a
    -- different tenant than the invite row doing the pointing.
    CONSTRAINT accounts_tenant_id_id_unique UNIQUE (tenant_id, id)
);

-- Email uniqueness is scoped per tenant, not global: two different tenants
-- may have a member with the same email address, and that must not
-- collide. Case-insensitive (lower()) because email comparison is
-- conventionally case-insensitive and two accounts differing only by
-- case would otherwise be indistinguishable to a human and to most mail
-- systems. A plain UNIQUE table constraint cannot express the lower()
-- expression, so this is a unique index instead.
CREATE UNIQUE INDEX accounts_tenant_id_email_lower_idx ON accounts (tenant_id, lower(email));

-- An OIDC subject is only ever unique within one identity provider's
-- namespace, which this schema models as "within one tenant", the same
-- scoping accounts_tenant_id_email_lower_idx already uses. Partial (WHERE
-- oidc_subject IS NOT NULL) because most accounts, every local one, have
-- it NULL, and NULL <> NULL under a plain unique index would not collide
-- anyway, so the predicate only keeps the index itself smaller.
CREATE UNIQUE INDEX accounts_tenant_id_oidc_subject_idx ON accounts (tenant_id, oidc_subject) WHERE oidc_subject IS NOT NULL;

-- Row level security is the actual multi-tenancy guarantee: every query
-- against this table, no matter how it is written, is filtered by the
-- tenant the current transaction has set. ENABLE alone is not enough,
-- because it does not apply to the table's own owner, and the owner is
-- exactly the role that runs migrations and, in most deployments, the
-- same role the application connects as too. FORCE ROW LEVEL SECURITY
-- makes the policy apply to the owning role as well, so owning the table
-- grants no exemption from it. The one role that is still exempt
-- regardless of FORCE is a Postgres superuser (or any role with the
-- BYPASSRLS attribute): whichever role runs the isolation test, and
-- whichever role the application connects as in production, must be
-- neither.
ALTER TABLE accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE accounts FORCE ROW LEVEL SECURITY;

-- The tenant is read back from a transaction-local setting, set once per
-- transaction via set_config('app.tenant_id', $1, true) (the trailing true
-- makes it transaction-local: SET LOCAL semantics, reachable with a bound
-- parameter, which a literal "SET LOCAL app.tenant_id = $1" is not, since
-- SET does not accept query parameters). A single policy with no FOR
-- clause applies to every command (SELECT, INSERT, UPDATE, DELETE): USING
-- filters what an existing row must satisfy to be read, written to or
-- deleted; WITH CHECK filters what a new or changed row must satisfy to be
-- written at all, so a row can never be inserted or moved into a tenant
-- other than the one currently scoped.
--
-- current_setting(..., true) (the second, "missing_ok" argument) returns
-- NULL instead of raising an error when app.tenant_id has never been set
-- in this session at all. That covers a fresh connection used outside of
-- WithTenant, but not a reused one: set_config(..., true) makes a custom
-- parameter transaction-local only in the sense that its *value* reverts
-- at COMMIT or ROLLBACK, not in the sense that the parameter stops
-- existing. The first set in a session turns app.tenant_id into a
-- session-level placeholder GUC (text-typed, reset value ''), and a
-- placeholder, once created, lives for the rest of the session; COMMIT
-- and ROLLBACK both revert it to that reset value, the empty string, not
-- to "unset" (postgresql.org/message-id/CA+Q86ij0KDCB0G45G509-8q0DNR611gcKG-sSM83GA1EBL7boA@mail.gmail.com).
-- So on a connection that WithTenant already scoped once and then handed
-- back to the pool, a later, unscoped transaction sees current_setting
-- return '', not NULL, and ''::uuid raises 22P02 instead of denying
-- access. NULLIF folds that reverted empty string back to NULL before
-- the cast, so both "never set in this session" and "set earlier, now
-- reverted" reach the same NULL, and by ordinary SQL three-valued logic
-- "tenant_id = NULL" evaluates to NULL, which a USING/WITH CHECK clause
-- treats as false, never as true: an unscoped transaction, fresh
-- connection or reused, sees zero rows and can write none, rather than
-- every row or a hard error. That is the deliberately chosen safe
-- default.
CREATE POLICY accounts_tenant_isolation ON accounts
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- migrate:down

DROP TABLE accounts;
