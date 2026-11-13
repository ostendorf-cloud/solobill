-- migrate:up

-- documents is the append-only ledger docs/concepts/solobill.md's Approach
-- section describes: every offer, invoice and correction this tenant ever
-- issues is a row here. This step (issue #15, step 2) only ever writes
-- type 'invoice' and 'correction': 'offer' is a later milestone on the
-- same model, so it is deliberately left out of the type CHECK below
-- rather than added speculatively; adding it later is the same cheap,
-- additive DROP CONSTRAINT/ADD CONSTRAINT change catalogue_items.unit's
-- own comment already describes for the same reason.
--
-- A correction is its own row, referencing the original via
-- corrects_document_id, never a mutation of the original: see
-- documents_block_issued_mutation below for how that is actually
-- enforced, and documents_correction_requires_original for how the type
-- and the reference are kept consistent with each other.
CREATE TABLE documents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    type text NOT NULL,
    -- draft is the only state a document is ever created in; issued is
    -- the one transition this schema allows afterward, and it is the
    -- last one: nothing here models a "cancelled" or "sent" state, since
    -- nothing in this step's scope (storage only, no HTTP, no mailing)
    -- needs one yet, and a later step can add one as its own additive
    -- CHECK change if it turns out to be needed.
    --
    -- "A state transition is a new row" (docs/concepts/solobill.md) is
    -- implemented at the *document* level, not by a transitions table of
    -- its own: draft to issued is the one in-place UPDATE this table ever
    -- allows (see documents_block_issued_mutation), and the only
    -- transition an already-issued document can ever go through again is
    -- answered by creating a wholly new document row (a correction) that
    -- references it, never by writing a second state onto the same row.
    -- A dedicated append-only state-transition table was considered and
    -- rejected: with exactly one allowed in-place transition (draft to
    -- issued) and every transition after that being a new document
    -- entirely, a transitions table would carry at most one or two rows
    -- per document ever, forever, for no reporting or audit value a
    -- trigger-guarded column plus this table's own created_at/updated_at
    -- does not already give for free.
    state text NOT NULL DEFAULT 'draft',
    -- Composite, not a plain `customer_id uuid REFERENCES customers (id)`:
    -- see the invites migration for why a single-column reference across
    -- a row-level-security boundary is a cross-tenant existence oracle.
    -- Requires customers.customers_tenant_id_id_unique.
    customer_id uuid NOT NULL,
    -- Set only on a type = 'correction' row, naming the document it
    -- corrects. Self-referential, same composite shape as customer_id
    -- above, and requires documents_tenant_id_id_unique below (defined on
    -- this same table, which is fine: the UNIQUE constraint a FOREIGN KEY
    -- targets only has to exist by the time the FOREIGN KEY itself is
    -- declared, and it is, a few lines up in this same CREATE TABLE).
    corrects_document_id uuid,
    -- Both VAT regimes from docs/concepts/solobill.md's goals: standard
    -- taxation, and Kleinunternehmer under section 19 UStG, where every
    -- line's VAT rate ends up 0 but the regime itself is still recorded,
    -- since the wording on the printed document differs by regime even
    -- when the tax figure does not.
    vat_regime text NOT NULL,
    -- Leistungsdatum/Lieferdatum: German VAT law (§14 UStG) requires the
    -- date of supply on an invoice, separate from the invoice's own date
    -- (issued_at below serves as the printed invoice date once set).
    -- Nullable: a draft is edited before this is necessarily known yet.
    service_date date,
    -- Nullable for the same reason: a draft may not have a due date
    -- decided until closer to issuing.
    due_date date,
    -- Document-level discount: a percentage, an absolute amount, or
    -- neither, never both at once (documents_discount_mutually_exclusive
    -- below). Permyriad (parts per 10,000: 1500 = 15.00%), not a
    -- Postgres numeric and never a float, the same reasoning
    -- catalogue_items.unit_price_cents already gives for cents: a scaled
    -- integer is the schema's own enforced granularity, two extra digits
    -- of precision beyond a plain percent, matching the rate precision
    -- document_lines.vat_rate_permyriad needs for consistency. See
    -- docs/concepts/solobill.md's Rounding section for how this discount
    -- is actually allocated; that allocation is internal/money's job
    -- (issue #15 step 2's calculation package), not this migration's.
    discount_percent_permyriad integer,
    discount_amount_cents bigint,
    -- The number this document was actually issued under, within its own
    -- type and number_year: the per-tenant, per-type, per-year counter in
    -- document_numbers is what allocates this for an ordinarily issued
    -- document, inside the issuing transaction (see
    -- internal/storage/postgres's numbering code). number_year is its own
    -- column, not derived from issued_at at read time, because
    -- imported (below) sets it independently of the counter's definition
    -- of "year": docs/concepts/solobill.md requires an imported historical
    -- document to carry the number it was issued under, outside the
    -- counter, and that number's year is a fact about the original
    -- document, not something this schema should recompute from a
    -- possibly-approximate import timestamp.
    number bigint,
    number_year integer,
    -- true for a document imported from before this tool existed
    -- (docs/concepts/solobill.md: "Documents from 2023 onwards are
    -- imported, keeping the numbers they were issued under"), carrying a
    -- number set directly rather than allocated by document_numbers.
    -- documents_number_year_number_unique below still applies to an
    -- imported row exactly as it does to a counter-issued one: two
    -- different documents, imported or not, can never carry the same
    -- (type, number_year, number), which is the schema-level half of "an
    -- import can never shift or reuse a number a customer already
    -- holds". The other half, refusing an import whose number the
    -- counter might later also try to issue, is the import milestone's
    -- own business logic (not yet built; this column only has to make
    -- that later logic possible, per issue #15 step 2's own scope).
    imported boolean NOT NULL DEFAULT false,
    -- The complete, self-contained snapshot an issued document is
    -- rendered from, forever: every input internal/money's calculation
    -- needs (customer details, every line's quantity/price/unit/VAT
    -- rate/discount, the document-level discount, every computed total)
    -- copied in at issue time, so a later change to customers or
    -- catalogue_items, or a later change to internal/money's own rounding
    -- code, can never alter what an already-issued document reads back
    -- as.
    --
    -- bytea, not jsonb: this column's whole purpose is reproducing the
    -- document exactly as it was issued, years later, for an audit or an
    -- e-invoice. frozen_payload_sha256 below has to be reproducible from
    -- this row for that purpose to mean anything, and Postgres does not
    -- make that possible for jsonb, because a jsonb column never stores
    -- the bytes it is given: it parses and re-serializes them into a
    -- decomposed binary form that drops insignificant whitespace,
    -- reorders object keys, discards a duplicate key, and normalizes
    -- numbers through numeric. A jsonb column read back is Postgres' own
    -- re-encoding, not the input, so a hash computed over the submitted
    -- bytes could never be recomputed from the stored row again, which
    -- defeats the one guarantee this column exists for. bytea has no
    -- such step: what is written is byte-for-byte what is read back.
    -- The cost is that this table cannot query into the payload's own
    -- fields with a jsonb operator; nothing in this schema or in
    -- internal/storage/postgres does that today, the payload's shape is
    -- a Go-side concern a later rendering step's contract will define
    -- (not yet built, and not this migration's), and a later feature
    -- that genuinely needs to query into it can add its own
    -- derived jsonb column then, decided and reviewed against an actual
    -- query, rather than this migration keeping jsonb speculatively and
    -- giving up the one guarantee that matters more. Set exactly once,
    -- together with frozen_payload_sha256, issued_at, number and
    -- number_year: see documents_issued_fields_consistent.
    frozen_payload bytea,
    -- Hex-encoded SHA-256 of frozen_payload's own bytes, exactly as
    -- stored: computed in Go over the exact byte slice written to this
    -- row, not re-derived from a separate canonical re-encoding, so
    -- verifying it later (docs/concepts/solobill.md: "every figure in a
    -- report can be traced back to the documents it came from") is
    -- "hash these bytes and compare", with no second encoding step that
    -- could itself drift from what was actually persisted. With
    -- documents_frozen_payload_sha256_matches below in place, this is
    -- deliberately a statement about internal consistency only: a pure
    -- function of frozen_payload's own stored bytes, so it proves this
    -- hash is the one this payload actually hashes to, never that the
    -- payload itself is what was actually submitted at issue time (a
    -- caller that freezes and hashes the wrong bytes satisfies this
    -- column pair and nothing here would ever detect it). bytea above is
    -- still what makes "hash these bytes and compare" possible at all,
    -- rather than impossible the way jsonb's own re-encoding would make
    -- it; see documents_frozen_payload_sha256_matches's own comment for
    -- why internal consistency, not provenance, is as far as a CHECK can
    -- reach.
    --
    -- Nothing in this step's own scope (storage and calculation only, no
    -- HTTP, no rendering) actually renders a document, so nothing here
    -- computes this column yet: the caller of
    -- internal/storage.DocumentRepository.Issue passes both
    -- frozen_payload and this column already computed, and that caller
    -- does not exist until a later milestone builds the rendering step.
    -- Leaving the two columns free to disagree until then would make the
    -- guarantee this column exists for only as strong as that
    -- not-yet-written caller, and a problem only discoverable once real
    -- rows exist, at which point fixing it is a migration over live data
    -- rather than a line in a migration nothing has run yet.
    -- documents_frozen_payload_sha256_matches below closes that: it is
    -- cheaper to make the relationship structural now, with Postgres'
    -- own built-in sha256(bytea) (core since Postgres 14, no extension),
    -- than to add it once this table is no longer empty. It is deliberately
    -- a statement about internal consistency only ("this hash is the one
    -- this payload actually hashes to"), not about where the hash comes
    -- from: the rendering step that eventually calls Issue still has to
    -- compute it from the exact bytes it is about to freeze, this CHECK
    -- only stops that step (or anything else running as solobill_app)
    -- from ever getting it wrong undetected.
    frozen_payload_sha256 text,
    issued_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT documents_type_check CHECK (type IN ('invoice', 'correction')),
    CONSTRAINT documents_state_check CHECK (state IN ('draft', 'issued')),
    CONSTRAINT documents_vat_regime_check CHECK (vat_regime IN ('standard', 'kleinunternehmer')),
    -- A correction always names what it corrects, and nothing else ever
    -- does: the two columns are kept in lockstep so corrects_document_id
    -- can never be set on an ordinary invoice, nor left unset on a
    -- correction.
    CONSTRAINT documents_correction_requires_original CHECK (
        (type = 'correction') = (corrects_document_id IS NOT NULL)
    ),
    -- Every column issuing fills in is filled in together, or not at
    -- all: a draft has none of them, an issued document (ordinary or
    -- imported) has all of them. This is the schema-level half of
    -- immutability's "issued means frozen"; documents_block_issued_mutation
    -- below is the half that stops them from ever being touched again
    -- once set.
    CONSTRAINT documents_issued_fields_consistent CHECK (
        (state = 'issued') = (
            number IS NOT NULL
            AND number_year IS NOT NULL
            AND issued_at IS NOT NULL
            AND frozen_payload IS NOT NULL
            AND frozen_payload_sha256 IS NOT NULL
        )
    ),
    CONSTRAINT documents_discount_mutually_exclusive CHECK (
        discount_percent_permyriad IS NULL OR discount_amount_cents IS NULL
    ),
    CONSTRAINT documents_discount_percent_range CHECK (
        discount_percent_permyriad IS NULL
        OR (discount_percent_permyriad >= 0 AND discount_percent_permyriad <= 10000)
    ),
    CONSTRAINT documents_discount_amount_nonneg CHECK (
        discount_amount_cents IS NULL OR discount_amount_cents >= 0
    ),
    CONSTRAINT documents_number_nonneg CHECK (number IS NULL OR number > 0),
    -- frozen_payload_sha256's own comment above explains why this is
    -- structural rather than left to a caller's own discipline: a NULL
    -- frozen_payload (every draft) trivially passes, and
    -- documents_issued_fields_consistent above already guarantees that
    -- whenever frozen_payload is set, frozen_payload_sha256 is too, so
    -- this never compares a hash against a NULL. encode(..., 'hex')
    -- lower-cases, matching this column's own documented lowercase
    -- convention.
    CONSTRAINT documents_frozen_payload_sha256_matches CHECK (
        frozen_payload IS NULL
        OR frozen_payload_sha256 = encode(sha256(frozen_payload), 'hex')
    ),
    -- Same purpose as customers_tenant_id_id_unique and
    -- catalogue_items_tenant_id_id_unique: lets document_lines and this
    -- same table's own corrects_document_id reference (tenant_id, id) as
    -- a composite foreign key, avoiding the cross-tenant existence-oracle
    -- problem a single-column reference would create (see the invites
    -- migration for the full reasoning).
    CONSTRAINT documents_tenant_id_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT documents_customer_id_fkey FOREIGN KEY (tenant_id, customer_id) REFERENCES customers (tenant_id, id),
    CONSTRAINT documents_corrects_document_id_fkey FOREIGN KEY (tenant_id, corrects_document_id) REFERENCES documents (tenant_id, id),
    -- The database-level backstop for gapless, never-reused numbering:
    -- document_numbers' own counter, allocated by a single
    -- UPDATE ... RETURNING, is what actually allocates a number without
    -- a gap under concurrency (see
    -- internal/storage/postgres's numbering code and its own tests), but
    -- this constraint is what makes a second row ever carrying the same
    -- (type, number_year, number) impossible outright, including through
    -- a future bug in that allocation code or through the as-yet-unbuilt
    -- import path writing a number the counter already issued. NULL
    -- numbers (every draft) do not collide with each other or with a real
    -- one: Postgres treats NULL as distinct from any other value,
    -- including another NULL, in a UNIQUE constraint.
    CONSTRAINT documents_number_year_number_unique UNIQUE (tenant_id, type, number_year, number)
);

-- Row level security is the actual multi-tenancy guarantee; see the
-- customers and catalogue_items migrations for the full reasoning, which
-- applies here unchanged.
ALTER TABLE documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE documents FORCE ROW LEVEL SECURITY;

CREATE POLICY documents_tenant_isolation ON documents
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Immutability (docs/concepts/solobill.md: "An issued invoice is never
-- modified") is enforced here, as a trigger, rather than only as a
-- repository method that refuses: a repository method can always be
-- bypassed by raw SQL run through the same, otherwise-trusted connection
-- (a migration, a maintenance script, a future handler written by someone
-- who never reads internal/storage's doc comments), and row level
-- security's own policy cannot express "reject depending on this row's
-- other column values" the way a CHECK or a trigger can. A CHECK alone
-- cannot do it either: a CHECK only ever sees the NEW row, never OLD, so
-- it cannot tell "this row is becoming issued" (allowed) apart from "this
-- row already was issued" (must be refused) on its own. A trigger is the
-- one mechanism in Postgres that can compare OLD against the operation
-- being attempted, so it is the only one of the four options (trigger,
-- policy, constraint, or a combination) that can express this rule at
-- all.
--
-- FORCE ROW LEVEL SECURITY above has no bearing on this trigger: it
-- only ever changes whose rows a row-level-security policy applies to
-- (without it, the table owner is exempt from the policy; with it, the
-- owner is scoped by the policy too), and a trigger is a wholly
-- separate mechanism a policy cannot touch either way. What actually
-- keeps this trigger from being sidestepped is that the role running
-- application traffic is not this table's owner and is NOSUPERUSER:
-- disabling a trigger (ALTER TABLE ... DISABLE TRIGGER) or skipping it
-- (SET session_replication_role = 'replica') both require owning the
-- table or being a superuser, neither of which that role may ever be.
--
-- That holds under docker-compose.yaml's own local-dev setup, where
-- solobill_app (the "solobill" service's role) never runs db/migrations
-- and so never owns tenants, accounts, invites, documents or
-- document_lines; solobill_super, the role that does own them, only
-- ever runs db/migrations itself, never application traffic.
--
-- It does NOT hold under charts/solobill/README.md's own default
-- deployment shape: by default, the chart's migration Job and its
-- Deployment authenticate as the exact same role (config.databaseUser),
-- which therefore owns every table db/migrations creates, this one
-- included, and so CAN run ALTER TABLE documents DISABLE TRIGGER
-- documents_block_issued_mutation and then mutate or delete an issued
-- invoice outright. See README.md's "One role, or two" section for the
-- two-role split (migrationJob.databaseUser set to a role distinct from
-- config.databaseUser) that restores the guarantee this comment
-- describes, and for why provisioning that split is the operator's own
-- responsibility, not something this migration or that chart can
-- enforce from here.
--
-- Deliberately blocks every UPDATE and DELETE once OLD.state = 'issued',
-- with no column-by-column exception: nothing in this step's scope (no
-- HTTP, no mailing, no PDF) has a legitimate reason to touch an issued
-- row at all, so there is no "allowed" column to carve out yet. A later
-- feature that legitimately needs to record something against an issued
-- document (for example a send attempt) is a new, explicit exception to
-- this trigger, decided and reviewed then, not something this migration
-- should anticipate and leave a gap for now.
-- SET search_path pins this function's own name resolution to
-- pg_catalog and public, regardless of whatever search_path the
-- calling session happens to have: Postgres 15+ already revokes CREATE
-- on public from PUBLIC, so a session-supplied search_path hijacking an
-- unqualified name in this function is not a live risk today, but
-- setting it costs nothing and is the standard hardening for a
-- SECURITY DEFINER-adjacent function (this one is not SECURITY
-- DEFINER, but the same search_path pinning is cheap defense in depth
-- regardless).
CREATE FUNCTION documents_block_issued_mutation() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
BEGIN
    IF OLD.state = 'issued' THEN
        RAISE EXCEPTION 'document % is issued and is immutable; create a correction instead', OLD.id
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER documents_block_issued_mutation
    BEFORE UPDATE OR DELETE ON documents
    FOR EACH ROW
    EXECUTE FUNCTION documents_block_issued_mutation();

-- ENABLE ALWAYS, not the implicit default ENABLE: a plain ENABLE
-- trigger is skipped when session_replication_role is set to
-- 'replica' (the setting a logical-replication subscriber runs under),
-- which would otherwise quietly turn this trigger off for any session
-- in that mode. Setting session_replication_role at all requires
-- superuser, which solobill_app never is, so this is defence in depth
-- rather than closing a real hole today; it costs nothing to set.
ALTER TABLE documents ENABLE ALWAYS TRIGGER documents_block_issued_mutation;

-- migrate:down

DROP TABLE documents;
DROP FUNCTION documents_block_issued_mutation();
