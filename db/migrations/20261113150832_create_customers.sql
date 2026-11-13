-- migrate:up

-- customers holds the people and companies a tenant issues offers and
-- invoices to. docs/concepts/solobill.md makes customers owned here, with
-- a connected system (for example eventory) referencing one by id rather
-- than keeping its own copy that could drift; this table is that owning
-- record. A later migration's documents table references a row here by
-- (tenant_id, id) once offers and invoices exist.
--
-- Deliberately no customer-number column: nothing in the concept doc's
-- goals asks for one, the uuid id is already the identifier a connected
-- system references a customer by, and adding a human-facing customer
-- number later, if ever needed, is a cheap additive migration. Inventing
-- that column now would be a requirement nobody has stated.
CREATE TABLE customers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    name text NOT NULL,
    -- Every address and contact field below is nullable: a customer is
    -- routinely entered with only a name, before their email or full
    -- address is known, and completing that record often takes another
    -- conversation. Whether a document can actually be issued to an
    -- incomplete customer is a business-logic decision for a later step
    -- (the documents milestone), not something this storage layer decides
    -- by forcing the data to exist before it does.
    email text,
    -- USt-IdNr., the EU VAT identification number. No format CHECK: the
    -- format varies by EU member state (prefix, length, structure), and
    -- validating it is not this layer's job; a later step may validate it
    -- against the format for country_code, if one is even set.
    vat_id text,
    street text,
    postal_code text,
    city text,
    -- ISO 3166-1 alpha-2. Deliberately no DEFAULT: picking one (for
    -- example 'DE') would be a business-policy decision belonging to a
    -- later UI layer, not something this migration should bake in.
    country_code text,
    -- Soft delete only: a customer is never hard deleted, because an
    -- issued invoice's frozen payload will eventually reference a
    -- customer by id (the documents milestone, not yet built). Today a
    -- hard delete here would always succeed, since nothing references
    -- this table yet; the day a documents.customer_id foreign key is
    -- added, that same hard delete would start failing for a referenced
    -- row, purely because of a schema change elsewhere, not because
    -- anything about deleting a customer itself changed. Soft delete
    -- sidesteps that inconsistency entirely: deleting a customer always
    -- just hides it behind deleted_at, whether or not a document
    -- references it yet, so introducing that later foreign key needs no
    -- change to how delete behaves. FindByID (internal/storage) does not
    -- filter on this column, because a later document still has to
    -- resolve a deleted customer by id; List does, because it is a
    -- human-facing picker that should only ever offer active customers.
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT customers_name_not_empty CHECK (name <> ''),
    -- Every nullable text column above gets the same defense-in-depth
    -- CHECK as accounts.email and tenants.slug: NULL (not yet known) is a
    -- valid state, an empty string is not, so a caller that means "not
    -- set" has to actually send NULL rather than an empty string that
    -- would otherwise sit in the row looking like a set-but-blank value.
    CONSTRAINT customers_email_not_empty CHECK (email IS NULL OR email <> ''),
    CONSTRAINT customers_vat_id_not_empty CHECK (vat_id IS NULL OR vat_id <> ''),
    CONSTRAINT customers_street_not_empty CHECK (street IS NULL OR street <> ''),
    CONSTRAINT customers_postal_code_not_empty CHECK (postal_code IS NULL OR postal_code <> ''),
    CONSTRAINT customers_city_not_empty CHECK (city IS NULL OR city <> ''),
    CONSTRAINT customers_country_code_not_empty CHECK (country_code IS NULL OR country_code <> ''),
    -- Same purpose as accounts_tenant_id_id_unique: id alone is already a
    -- unique primary key, so this constraint adds no new uniqueness by
    -- itself; it exists only so that (tenant_id, id) together is a valid
    -- foreign key target. A later migration's documents.customer_id can
    -- then reference customers (tenant_id, id) instead of customers (id)
    -- alone, which matters because Postgres checks a foreign key's
    -- referential integrity as the table owner, bypassing row level
    -- security entirely; a single-column reference would let an insert's
    -- success or failure depend on whether the id exists in *any*
    -- tenant's customers, turning the column into a cross-tenant
    -- existence oracle even though nothing lets a caller choose it
    -- directly. See the invites migration for the fuller version of this
    -- reasoning.
    CONSTRAINT customers_tenant_id_id_unique UNIQUE (tenant_id, id)
);

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
ALTER TABLE customers ENABLE ROW LEVEL SECURITY;
ALTER TABLE customers FORCE ROW LEVEL SECURITY;

-- Same tenant-scoping mechanism as accounts, NULLIF included: see the
-- accounts migration for why current_setting(..., true) alone is not
-- enough on a connection set_config already scoped once this session.
-- "tenant_id = NULL" is never true, so an unscoped transaction, fresh
-- connection or reused, sees and can write zero rows here too.
CREATE POLICY customers_tenant_isolation ON customers
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- migrate:down

DROP TABLE customers;
