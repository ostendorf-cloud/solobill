-- migrate:up

-- catalogue_items holds the sole trader's price list: the goods and
-- services a document line can be drawn from. eventory is expected to
-- later price day-rate hire against this catalogue, and a human's own
-- existing price list is the other consumer, so this table has to carry
-- a precise, unambiguous current price and nothing more; per-document
-- concerns live elsewhere, see unit_price_cents below.
CREATE TABLE catalogue_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    name text NOT NULL,
    description text,
    -- Optional, user-supplied, never generated. docs/concepts/solobill.md
    -- has eventory pricing day-rate hire against this catalogue later; a
    -- human-assigned SKU gives it, and the sole trader's own existing
    -- price list, a stable mnemonic lookup key that does not depend on
    -- matching against name, without building any eventory-specific
    -- integration now. See catalogue_items_tenant_id_sku_lower_idx below
    -- for its uniqueness.
    sku text,
    -- Integer minor units (cents), not numeric and never float: a
    -- float's binary rounding error is the wrong representation for
    -- money, and a minor-unit integer makes "round to the nearest cent"
    -- the schema's own enforced granularity, rather than something every
    -- later piece of arithmetic has to separately remember to do. No
    -- currency column: multi-currency is an explicit non-goal in
    -- docs/concepts/solobill.md, so the minor unit here is implicitly
    -- EUR cents; a currency column would be a dimension this schema does
    -- not need yet. Per-line and per-document discounts, and per-rate
    -- VAT rounding, are a later migration's (document_lines) concern,
    -- not this table's: the rounding and discount-allocation rules
    -- themselves are now decided, in docs/concepts/solobill.md's
    -- Rounding section, so that later migration implements a written
    -- rule rather than inventing one on the spot. This table only has
    -- to hold a precise price for the current item, nothing about how
    -- that price is applied on a document.
    unit_price_cents bigint NOT NULL CHECK (unit_price_cents >= 0),
    -- A controlled, small set rather than free text: a unit is rendered
    -- directly on a tax-relevant document line ("3 x <unit>"), so
    -- consistency matters the same way the SKU column's
    -- case-insensitive uniqueness mattered for lookup consistency above;
    -- free text invites drift ("Stk" vs "piece" vs "pieces") that a
    -- later renderer would otherwise have to normalize itself. The three
    -- values cover every case this catalogue has today: "piece" is the
    -- common default for goods and flat-fee services, roughly 200
    -- existing catalogue items being imported among them; "hour" and
    -- "day" cover time-based billing, with "day" specifically what
    -- eventory's day-rate hire needs (see the table comment above).
    -- NOT NULL DEFAULT 'piece', not nullable: every existing or imported
    -- item then defaults sensibly with zero data loss, and a raw-SQL
    -- test INSERT that only cares about some other column can omit this
    -- one entirely without tripping an unrelated NOT NULL violation.
    -- Extending the CHECK's set later, should a fourth unit value turn
    -- out to be needed, is a one-line ALTER TABLE ... DROP CONSTRAINT
    -- ... ADD CONSTRAINT migration, the same cheap, additive class of
    -- change adding this column itself is, unlike a missing column,
    -- which would need a guess at backfilling roughly 200 rows.
    unit text NOT NULL DEFAULT 'piece' CHECK (unit IN ('piece', 'hour', 'day')),
    -- Soft delete only, same reasoning as customers.deleted_at: a
    -- catalogue item is never hard deleted, because an issued document's
    -- frozen payload will eventually reference one by id (the documents
    -- milestone). A hard delete that always succeeds today would start
    -- failing for a referenced row the day that foreign key exists,
    -- purely from a schema change elsewhere; soft delete is consistent
    -- from day one regardless of whether a reference exists yet.
    -- FindByID does not filter on this column (a later document still
    -- has to resolve a deleted item by id); List does, since it is a
    -- human-facing picker that should only ever offer active items.
    --
    -- This table carries no price history or versioning of its own: a
    -- document line copies the price onto itself (a later migration's
    -- job), and that copy, not anything here, is what keeps an issued
    -- document's totals frozen even after this table's price later
    -- changes. updated_at below is ordinary audit, not a version marker.
    -- Whether a line copies the price at draft time or at issue time is
    -- still an open question per the issue's own plan, and is not
    -- decided by this migration.
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT catalogue_items_name_not_empty CHECK (name <> ''),
    CONSTRAINT catalogue_items_description_not_empty CHECK (description IS NULL OR description <> ''),
    CONSTRAINT catalogue_items_sku_not_empty CHECK (sku IS NULL OR sku <> ''),
    -- Same purpose as accounts_tenant_id_id_unique and
    -- customers_tenant_id_id_unique: lets a later migration's
    -- documents.catalogue_item_id reference (tenant_id, id) as a
    -- composite foreign key, avoiding the cross-tenant existence-oracle
    -- problem a single-column reference would create (see the invites
    -- migration for the full reasoning).
    CONSTRAINT catalogue_items_tenant_id_id_unique UNIQUE (tenant_id, id)
);

-- Partial on both predicates: sku IS NOT NULL because most rows are
-- expected to have none (sku is optional), and deleted_at IS NULL
-- because a deleted item's SKU has to be reusable by a new item without
-- waiting for the old row to be purged, which never happens (soft
-- delete only). Case-insensitive (lower()) for the same reason
-- accounts_tenant_id_oidc_subject_idx and
-- accounts_tenant_id_email_lower_idx are: two SKUs differing only by
-- case would be indistinguishable to the human typing them in. A plain
-- UNIQUE table constraint cannot express the lower() expression or the
-- WHERE predicate, so this is a unique index instead. There is no
-- restore path today (nothing clears deleted_at); if one is ever added,
-- restoring a row whose SKU was since reused by a newer item will hit
-- this index's unique violation (23505) and correctly fail rather than
-- silently create a duplicate SKU.
CREATE UNIQUE INDEX catalogue_items_tenant_id_sku_lower_idx ON catalogue_items (tenant_id, lower(sku)) WHERE sku IS NOT NULL AND deleted_at IS NULL;

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
ALTER TABLE catalogue_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE catalogue_items FORCE ROW LEVEL SECURITY;

-- Same tenant-scoping mechanism as customers and accounts, NULLIF
-- included: see the accounts migration for why current_setting(...,
-- true) alone is not enough on a connection set_config already scoped
-- once this session. "tenant_id = NULL" is never true, so an unscoped
-- transaction, fresh connection or reused, sees and can write zero rows
-- here too.
CREATE POLICY catalogue_items_tenant_isolation ON catalogue_items
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- migrate:down

DROP TABLE catalogue_items;
