-- migrate:up

-- tenants is the root of multi-tenancy: every other tenant-scoped table
-- carries a tenant_id referencing this one. It deliberately carries no
-- row level security policy of its own, because it has no tenant_id
-- column to scope by: a row here IS a tenant, not something owned by one.
--
-- A self-referential policy such as "id = current_setting('app.tenant_id')"
-- would also break the one flow that has to run before a tenant is known:
-- resolving a human-readable slug (from a login URL or an invite link) to
-- the tenant id that is then passed to set_config for every later,
-- tenant-scoped query. Restricting which tenants can be enumerated or
-- resolved by slug is an application-layer concern for a later step, not
-- a database-layer one.
CREATE TABLE tenants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Human-readable identifier used in URLs and invite links, for example
    -- to resolve "acme" to a tenant id before any tenant scope is set.
    slug text NOT NULL,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tenants_slug_not_empty CHECK (slug <> ''),
    CONSTRAINT tenants_name_not_empty CHECK (name <> '')
);

CREATE UNIQUE INDEX tenants_slug_idx ON tenants (slug);

-- migrate:down

DROP TABLE tenants;
