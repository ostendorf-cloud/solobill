-- migrate:up

-- invites carries a local account invite: an owner invites by email, the
-- invite fixes the tenant and the role up front, and the invitee later
-- uses it, exactly once, to create their account row. The token itself is
-- a single-use secret and is never stored: only a hash of it is, the same
-- way a password reset token is handled, so a leaked database row alone
-- never lets anyone redeem an invite.
CREATE TABLE invites (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    email text NOT NULL,
    role text NOT NULL CHECK (role IN ('owner', 'member')),
    -- Already-computed hash of the invite token, never the token itself.
    -- Computing and verifying it is internal/auth's job, not this one's.
    token_hash text NOT NULL,
    expires_at timestamptz NOT NULL,
    -- NULL until redeemed; set exactly once, enforcing single use.
    used_at timestamptz,
    -- Nullable: a bootstrap or imported invite may have no inviting
    -- account on record. References accounts through the composite
    -- (tenant_id, created_by), not through id alone, and the FOREIGN KEY
    -- constraint below carries that: a plain `REFERENCES accounts (id)`
    -- would let this column name an account id without naming its tenant,
    -- and Postgres checks a foreign key's referential integrity as the
    -- table owner, bypassing row level security entirely (the same
    -- exemption CREATE POLICY's own documentation describes), so an insert
    -- against such a column would succeed or fail depending on whether the
    -- id exists in *any* tenant's accounts, not just this invite's own.
    -- That turns the column into an oracle for probing account ids across
    -- tenant boundaries, even though nothing today ever lets a caller
    -- choose this value (internal/httpapi always sets it from the
    -- authenticated session). Scoping the foreign key itself by tenant_id
    -- closes that off at the one place it can actually be closed: the
    -- referential-integrity check that skips row level security.
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- Nullable until the invitee's enrolment step (internal/auth's
    -- InviteManager.IssueTOTPSecret) generates one. The server owns this
    -- value end to end: it is generated here, never accepted from the
    -- client, and copied onto the new account's own totp_secret only once
    -- AcceptInvite confirms a code against it. Same shape and the same
    -- length check as accounts.totp_secret: see that migration.
    totp_secret text,
    CONSTRAINT invites_totp_secret_length CHECK (totp_secret IS NULL OR length(totp_secret) = 32),
    CONSTRAINT invites_email_not_empty CHECK (email <> ''),
    -- Composite, not a plain `created_by uuid REFERENCES accounts (id)`:
    -- see the doc comment on created_by above for why a single-column
    -- reference would be a cross-tenant existence oracle. Requires
    -- accounts.accounts_tenant_id_id_unique, the matching composite unique
    -- constraint added in that migration for exactly this purpose.
    CONSTRAINT invites_tenant_id_created_by_fkey FOREIGN KEY (tenant_id, created_by) REFERENCES accounts (tenant_id, id)
);

-- A token is drawn from a cryptographically random source (internal/auth's
-- job), so two different invites hashing to the same value is not a case
-- this schema expects to occur; the unique index exists as a defense in
-- depth, not as the actual uniqueness guarantee. Deliberately global
-- rather than per-tenant, matching the token's own global uniqueness.
CREATE UNIQUE INDEX invites_token_hash_idx ON invites (token_hash);

-- Same reasoning as accounts: ENABLE alone exempts the table's owning
-- role, which is ordinarily the same role that runs migrations and that
-- the application connects as, so FORCE is what actually makes the
-- policy binding. See the accounts migration for the full reasoning; it
-- applies here unchanged.
ALTER TABLE invites ENABLE ROW LEVEL SECURITY;
ALTER TABLE invites FORCE ROW LEVEL SECURITY;

-- Same tenant-scoping mechanism as accounts, NULLIF included: see the
-- accounts migration for why current_setting(..., true) alone is not
-- enough on a connection set_config already scoped once this session.
-- "tenant_id = NULL" is never true, so an unscoped transaction, fresh
-- connection or reused, sees and can write zero rows here too.
--
-- An invite's token is only ever known to its recipient, not to whichever
-- tenant the recipient's browser happens to be scoped to, so redeeming it
-- cannot start from a tenant-scoped lookup the way every other query on
-- this table does. The accept-invite link therefore has to carry the
-- tenant itself (for example as a slug resolved against tenants, which
-- carries no row level security, before the API ever looks at invites),
-- so that redemption still runs as an ordinary WithTenant-scoped lookup by
-- token hash, the same as any other query here. That wiring is
-- internal/auth's and internal/httpapi's job in a later step; this step
-- adds no policy exception for it, so there is no path, intentional or
-- accidental, by which invites in other tenants become visible.
CREATE POLICY invites_tenant_isolation ON invites
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- migrate:down

DROP TABLE invites;
