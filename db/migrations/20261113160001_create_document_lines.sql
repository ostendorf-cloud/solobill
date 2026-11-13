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
-- removing its lines), the lookup runs after the document row is
-- already gone from this transaction's view, so it finds no row,
-- parent_state stays NULL, and "NULL = 'issued'" is never true: the
-- cascade is allowed to proceed. That is safe only because
-- documents_block_issued_mutation already refused the delete outright,
-- before the cascade ever starts, for any document that was actually
-- issued; this trigger here never has to tell "the document is gone
-- because it was draft and is being deleted" apart from "the document
-- is gone because it is issued", because the second case can never
-- reach a DELETE on documents at all.
CREATE FUNCTION document_lines_block_issued_document_mutation() RETURNS trigger
    LANGUAGE plpgsql AS $$
DECLARE
    parent_state text;
    target_document_id uuid;
BEGIN
    target_document_id := COALESCE(NEW.document_id, OLD.document_id);

    SELECT state INTO parent_state FROM documents WHERE id = target_document_id;

    IF parent_state = 'issued' THEN
        RAISE EXCEPTION 'document % is issued; its lines are immutable', target_document_id
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER document_lines_block_issued_document_mutation
    BEFORE INSERT OR UPDATE OR DELETE ON document_lines
    FOR EACH ROW
    EXECUTE FUNCTION document_lines_block_issued_document_mutation();

-- migrate:down

DROP TABLE document_lines;
DROP FUNCTION document_lines_block_issued_document_mutation();
