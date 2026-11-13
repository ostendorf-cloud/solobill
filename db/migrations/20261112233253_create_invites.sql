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
    -- account on record.
    created_by uuid REFERENCES accounts (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT invites_email_not_empty CHECK (email <> '')
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

-- Same tenant-scoping mechanism as accounts: current_setting(..., true)
-- is NULL when no transaction-local app.tenant_id was ever set, and
-- "tenant_id = NULL" is never true, so an unscoped transaction sees and
-- can write zero rows here too.
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
    USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);

-- migrate:down

DROP TABLE invites;
