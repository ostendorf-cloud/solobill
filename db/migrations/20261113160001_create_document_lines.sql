-- migrate:up

-- document_lines holds one billable line per document: a quantity, a
-- unit price and a unit copied from catalogue_items at the moment the
-- line is added to a draft, a VAT rate, and the line's own discount.
-- Nothing here stores a computed total: a line's net amount, its
-- discount in cents, its share of the document-level discount and its
-- VAT are all internal/money's job (issue #15 step 2's calculation
-- package) to compute from these inputs, at render time for a draft and
-- once, frozen into documents.frozen_payload, at issue time for an
-- issued document. Storing a redundant computed column here would be a
-- second place those figures could drift from what internal/money
-- actually produces.
CREATE TABLE document_lines (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    -- Composite, requiring documents_tenant_id_id_unique, the same
    -- cross-tenant-existence-oracle reasoning as every other composite
    -- foreign key in this schema; see the invites migration. ON DELETE
    -- CASCADE: a draft document's lines belong to it and nothing else,
    -- so deleting a still-mutable draft removes its lines with it rather
    -- than leaving them orphaned. This can never fire against an issued
    -- document: document_lines_block_issued_document_mutation below
    -- already refuses to delete a line whose document is issued, and
    -- documents_block_issued_mutation (see the documents migration)
    -- refuses to delete the document itself once issued, so the DELETE
    -- that would trigger this cascade never reaches an issued row in the
    -- first place.
    document_id uuid NOT NULL,
    -- Display and calculation order within the document: the
    -- document-level discount's largest-remainder allocation
    -- (docs/concepts/solobill.md's Rounding section) breaks a tied
    -- fractional remainder "by line order", which has to be a fact the
    -- schema records, not something inferred from created_at (two lines
    -- added in the same statement would tie) or from row order (a SELECT
    -- with no ORDER BY makes no ordering guarantee at all).
    position integer NOT NULL,
    -- Nullable: a line can be drawn from the catalogue or be an ad hoc
    -- entry with no catalogue item behind it at all. Composite foreign
    -- key, same reasoning as document_id above, requiring
    -- catalogue_items_tenant_id_id_unique. Kept even though price and
    -- unit are copied onto this row and nothing here needs it for
    -- referential integrity: it is still useful for reporting ("which
    -- catalogue items actually sold, and how often") without having to
    -- match on description text, which free text and a later rename
    -- both make unreliable. ON DELETE is deliberately left at its
    -- default (NO ACTION): catalogue_items never hard deletes (see its
    -- own migration), so this foreign key's delete behavior never
    -- actually has to run in practice.
    catalogue_item_id uuid,
    -- Copied at the moment the line is added to a draft, from
    -- catalogue_items.name plus catalogue_items.description when drawn
    -- from the catalogue, or typed directly for an ad hoc line; never
    -- re-read from catalogue_items afterward. This is what keeps a
    -- line's own wording stable even after the catalogue item it came
    -- from is later renamed or soft deleted, the same reason
    -- unit_price_cents and unit below are copied rather than joined.
    description text NOT NULL,
    -- Scaled integer, three decimal places (2500 = 2.5), never a
    -- Postgres numeric and never a float: docs/concepts/solobill.md's
    -- Rounding section requires a fractional quantity (hours, days) to
    -- be carried at sub-cent precision without ever touching a float,
    -- and a scaled integer is the same enforced-granularity mechanism
    -- catalogue_items.unit_price_cents already uses for money, applied
    -- here to quantity instead. Three decimal places comfortably covers
    -- a fraction of an hour (quarter-hour billing, 0.25h = 250) without
    -- the awkward binary fractions a float would introduce.
    quantity_thousandths bigint NOT NULL,
    -- Copied from catalogue_items.unit_price_cents at the moment the
    -- line is added to a draft (see the table comment above for why a
    -- copy, not a join). Per issue #15's plan, copying at draft time
    -- rather than at issue time is the deliberate choice: a catalogue
    -- price change between adding a line and issuing the document must
    -- never silently move an already-drafted line's total, which copying
    -- at issue time would risk; a human editing a draft already expects
    -- to see, and can deliberately refresh, the price they are about to
    -- bill.
    unit_price_cents bigint NOT NULL,
    -- Copied from catalogue_items.unit at the same moment, same
    -- reasoning, and the same three-value domain: see catalogue_items'
    -- own migration for why "piece", "hour" and "day" are the complete
    -- set today.
    unit text NOT NULL DEFAULT 'piece',
    -- Permyriad (parts per 10,000: 1900 = 19.00%), the same scaled-integer
    -- reasoning as documents.discount_percent_permyriad: never a
    -- Postgres numeric, never a float. 0 covers both a genuinely
    -- zero-rated line and Kleinunternehmer (every line is 0 under that
    -- regime; see documents.vat_regime), so internal/money needs no
    -- separate "no VAT" case, just a rate of zero like any other.
    vat_rate_permyriad integer NOT NULL,
    -- This line's own discount: a percentage, an absolute amount, or
    -- neither, never both (document_lines_discount_mutually_exclusive
    -- below), entirely independent of documents' own document-level
    -- discount columns.
    line_discount_percent_permyriad integer,
    line_discount_amount_cents bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT document_lines_description_not_empty CHECK (description <> ''),
    CONSTRAINT document_lines_quantity_positive CHECK (quantity_thousandths > 0),
    CONSTRAINT document_lines_unit_price_nonneg CHECK (unit_price_cents >= 0),
    CONSTRAINT document_lines_unit_check CHECK (unit IN ('piece', 'hour', 'day')),
    CONSTRAINT document_lines_vat_rate_range CHECK (vat_rate_permyriad >= 0 AND vat_rate_permyriad <= 10000),
    CONSTRAINT document_lines_discount_mutually_exclusive CHECK (
        line_discount_percent_permyriad IS NULL OR line_discount_amount_cents IS NULL
    ),
    CONSTRAINT document_lines_discount_percent_range CHECK (
        line_discount_percent_permyriad IS NULL
        OR (line_discount_percent_permyriad >= 0 AND line_discount_percent_permyriad <= 10000)
    ),
    CONSTRAINT document_lines_discount_amount_nonneg CHECK (
        line_discount_amount_cents IS NULL OR line_discount_amount_cents >= 0
    ),
    CONSTRAINT document_lines_tenant_id_document_id_position_unique UNIQUE (tenant_id, document_id, position),
    CONSTRAINT document_lines_document_id_fkey FOREIGN KEY (tenant_id, document_id) REFERENCES documents (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT document_lines_catalogue_item_id_fkey FOREIGN KEY (tenant_id, catalogue_item_id) REFERENCES catalogue_items (tenant_id, id)
);

-- Row level security is the actual multi-tenancy guarantee; see the
-- customers and catalogue_items migrations for the full reasoning, which
-- applies here unchanged.
ALTER TABLE document_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE document_lines FORCE ROW LEVEL SECURITY;

CREATE POLICY document_lines_tenant_isolation ON document_lines
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- The same immutability guarantee as documents_block_issued_mutation,
-- extended to a line: an issued document's lines must be exactly as
-- frozen as the document row itself, since internal/money would
-- otherwise recompute a different total from a since-edited line while
-- documents.frozen_payload still claims the old one. A document's own
-- trigger cannot reach this table (a trigger only ever fires on the
-- table it is attached to), so this is a second trigger, not a
-- duplicate of the first: it looks up its own document's current state
-- and applies the identical rule to INSERT (no new line can be added to
-- an already-issued document either), UPDATE and DELETE.
--
-- The lookup runs inside whatever transaction fired this trigger, so it
-- is subject to that transaction's own row level security scope exactly
-- like any other read; it needs no SECURITY DEFINER escalation, since
-- the line being touched already belongs to the same tenant whose scope
-- the transaction is set_config'd to. If this document is itself being
-- deleted in the same statement (a draft's ON DELETE CASCADE above
-- removing its lines), the DELETE arm's own lookup (old_document_id,
-- below) runs after the document row is already gone from this
-- transaction's view, so it finds no row, old_state stays NULL, and
-- "NULL = 'issued'" is never true: the cascade is allowed to proceed.
-- That is safe only because documents_block_issued_mutation already
-- refused the delete outright, before the cascade ever starts, for any
-- document that was actually issued; this trigger here never has to
-- tell "the document is gone because it was draft and is being deleted"
-- apart from "the document is gone because it is issued", because the
-- second case can never reach a DELETE on documents at all.
--
-- On UPDATE, both the row's old and new parent are looked up and
-- refused on, not only the new one: see the function body's own comment
-- on old_document_id/new_document_id for the bug this closes (moving a
-- line off an issued document by UPDATEing document_id used to check
-- only the parent the line was moving onto). Two SELECTs, not one IN
-- (...) query, so that an UPDATE leaving document_id unchanged still
-- only ever takes one FOR SHARE lock, the same as before this fix.
--
-- FOR SHARE on each SELECT, not a plain read: under READ COMMITTED, a
-- plain SELECT reads its own transaction's snapshot, so a transaction
-- inserting a line could read 'draft' from a snapshot taken just before
-- a concurrent Issue commits 'issued', and both would then commit,
-- leaving an issued document with a line that landed after it was
-- issued and that documents.frozen_payload never accounts for. FOR
-- SHARE closes that: IssueDocument's own UPDATE (queries/documents.sql)
-- takes a FOR NO KEY UPDATE lock on the documents row it writes (an
-- ordinary UPDATE that touches no key column), which conflicts with FOR
-- SHARE, so this lookup blocks until that UPDATE's transaction commits
-- or rolls back and only then reads the row for itself, serialising the
-- two. It does not serialise two concurrent line inserts on the same
-- still-draft document against each other: FOR SHARE held by one does
-- not conflict with FOR SHARE held by another, so both proceed. It also
-- cannot deadlock against the issuing transaction's own lock order:
-- every method in this package that locks a documents row (Update,
-- Issue) does so before it ever touches document_lines in the same
-- transaction, so no transaction ever waits for a document_lines-driven
-- lock on documents while already holding a lock document_lines itself
-- needs. A transaction creating a correction does hold a lock on a
-- second documents row at the same time (its own newly inserted row,
-- plus the KEY SHARE lock documents_corrects_document_id_fkey takes on
-- the original), so "never holds a lock on more than one documents row
-- at a time" overstates it; it is still harmless, since KEY SHARE
-- conflicts with neither FOR SHARE nor FOR NO KEY UPDATE, so it cannot
-- be the other half of an AB-BA cycle with this trigger's own lookup.
CREATE FUNCTION document_lines_block_issued_document_mutation() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
DECLARE
    old_document_id uuid;
    new_document_id uuid;
    old_state text;
    new_state text;
BEGIN
    -- OLD and NEW are only ever assigned by Postgres for the operations
    -- that actually carry them (OLD for UPDATE/DELETE, NEW for
    -- INSERT/UPDATE); reading a field off the other one is a runtime
    -- error in plpgsql, not simply NULL, so TG_OP gates every access
    -- below instead of a COALESCE across both. A prior revision of this
    -- function used target_document_id := COALESCE(NEW.document_id,
    -- OLD.document_id), which, because document_id is NOT NULL, always
    -- resolved to NEW.document_id on an UPDATE and never consulted
    -- OLD.document_id at all: UPDATE document_lines SET document_id =
    -- <a draft's id> WHERE id = <a line of an issued document> then
    -- checked only the draft it was moving the line onto, found it not
    -- issued, and let the move through, silently dropping a line from
    -- an issued document with no complaint from either trigger (this
    -- one is on a different table than documents_block_issued_mutation,
    -- and the composite foreign key only requires the same tenant, not
    -- the same state).
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        old_document_id := OLD.document_id;
    END IF;

    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        new_document_id := NEW.document_id;
    END IF;

    -- The row's new parent (its only parent for INSERT, its unchanged or
    -- newly-assigned parent for UPDATE): refused if that document is
    -- issued, exactly like the INSERT-onto-an-issued-document case this
    -- trigger already covered.
    IF new_document_id IS NOT NULL THEN
        SELECT state INTO new_state FROM documents WHERE id = new_document_id FOR SHARE;

        IF new_state = 'issued' THEN
            RAISE EXCEPTION 'document % is issued; its lines are immutable', new_document_id
                USING ERRCODE = 'integrity_constraint_violation';
        END IF;
    END IF;

    -- The row's old parent (its only parent for DELETE; its parent
    -- before the UPDATE when document_id actually changes): refused if
    -- THAT document is issued too, which is the fix itself. Skipped when
    -- old_document_id equals new_document_id (an ordinary UPDATE that
    -- leaves document_id alone): the SELECT above already covered that
    -- same row, and re-checking it would only take a second, redundant
    -- FOR SHARE lock on it.
    IF old_document_id IS NOT NULL AND old_document_id IS DISTINCT FROM new_document_id THEN
        SELECT state INTO old_state FROM documents WHERE id = old_document_id FOR SHARE;

        IF old_state = 'issued' THEN
            RAISE EXCEPTION 'document % is issued; its lines are immutable', old_document_id
                USING ERRCODE = 'integrity_constraint_violation';
        END IF;
    END IF;

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;

    RETURN NEW;
END;
$$;

-- This trigger's UPDATE arm is unreachable for solobill_app specifically
-- under docker-compose.yaml's own local-dev setup and under
-- charts/solobill's own default deployment shape (see
-- charts/solobill/README.md's "One role, or two"): "grant-app-role"
-- grants document_lines only SELECT, INSERT and DELETE, never UPDATE
-- (no query ever updates a line in place; see that grant's own
-- comment), so a raw UPDATE is already refused by the privilege check
-- before this trigger ever runs there, and
-- document_immutability_integration_test.go's own UPDATE subtest
-- against the restricted test role asserts exactly that (42501, not
-- this trigger's 23000).
--
-- It is NOT unreachable in general: the chart only refuses to render
-- when migrationJob.databaseUser is left unset or set equal to
-- config.databaseUser (templates/deployment.yaml's own fail guards); it
-- has no way to stop an operator from separately granting
-- config.databaseUser ownership of this table some other way after
-- install (directly, or through membership in the owning role), despite
-- having satisfied that render-time contract. A role that owns this
-- table holds every privilege on it, UPDATE included, with no GRANT
-- needed, which is exactly the case Blocker 1's own fix (see the
-- function body above) landed in: a raw UPDATE moving a line between
-- documents is exactly what an owning role can still do, and this
-- trigger, not a grant, is the only thing left to refuse it. A second
-- integration subtest, run as a role that owns this table
-- (document_immutability_integration_test.go's "moving an issued
-- document's line onto a draft" case), exercises that arm directly
-- rather than leaving it uncovered.
CREATE TRIGGER document_lines_block_issued_document_mutation
    BEFORE INSERT OR UPDATE OR DELETE ON document_lines
    FOR EACH ROW
    EXECUTE FUNCTION document_lines_block_issued_document_mutation();

-- Same reasoning as the documents migration's own ENABLE ALWAYS: a
-- plain ENABLE trigger is skipped under session_replication_role =
-- 'replica', which requires superuser and so is not reachable by
-- solobill_app, but setting this costs nothing.
ALTER TABLE document_lines ENABLE ALWAYS TRIGGER document_lines_block_issued_document_mutation;

-- migrate:down

DROP TABLE document_lines;
DROP FUNCTION document_lines_block_issued_document_mutation();
