-- migrate:up

-- euer_assignments holds the owner's decision which tax year an entry near
-- the turn of the year belongs to (issue #80): a payment, a booked expense
-- or a booked other income entry dated from 22 December to 10 January.
-- Without a row an entry counts in the year of its own date.
--
-- NOT VERIFIED: the 10 day reading of section 11 Abs. 1 Satz 2 and Abs. 2
-- Satz 2 EStG comes from secondary sources only. solobill never decides
-- it. The window only limits which entries the owner may assign.
--
-- The table is append-only. A changed decision is a new row naming the
-- old one in supersedes_id. The application role gets SELECT and INSERT
-- only (docker/postgres/grants/20261121180000_euer_assignments.sql), and
-- euer_assignments_block_mutation and euer_assignments_block_truncate
-- refuse every change for every other role.
--
-- One effective row per entry, by two unique indexes and a foreign key:
--   - euer_assignments_first_once: an entry has at most one row that
--     supersedes nothing.
--   - euer_assignments_superseded_once: a row is superseded at most once.
--   - euer_assignments_supersedes_fkey: a row only supersedes a row of the
--     same tenant and the same entry.
-- So the rows of one entry form a single line, and its last row is the
-- one effective assignment. A second writer that read the same effective
-- row loses on euer_assignments_superseded_once with 23505.
CREATE TABLE euer_assignments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    entry_kind text NOT NULL,
    entry_id uuid NOT NULL,
    tax_year integer NOT NULL,
    -- The owner's own reason, free text, for example "Miete Dezember".
    note text,
    supersedes_id uuid,
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT euer_assignments_entry_kind_check CHECK (entry_kind IN ('payment', 'expense', 'other_income')),
    CONSTRAINT euer_assignments_tax_year_check CHECK (tax_year BETWEEN 2000 AND 9999),
    CONSTRAINT euer_assignments_note_not_empty CHECK (note IS NULL OR note <> ''),
    CONSTRAINT euer_assignments_note_length CHECK (note IS NULL OR char_length(note) <= 1000),
    CONSTRAINT euer_assignments_not_self_superseding CHECK (supersedes_id IS NULL OR supersedes_id <> id),
    -- Composite foreign key targets, see
    -- 20261113150832_create_customers.sql for why a single column
    -- reference would be a cross tenant existence oracle.
    CONSTRAINT euer_assignments_tenant_id_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT euer_assignments_entry_row_unique UNIQUE (tenant_id, entry_kind, entry_id, id),
    CONSTRAINT euer_assignments_supersedes_fkey FOREIGN KEY (tenant_id, entry_kind, entry_id, supersedes_id)
        REFERENCES euer_assignments (tenant_id, entry_kind, entry_id, id),
    CONSTRAINT euer_assignments_created_by_fkey FOREIGN KEY (tenant_id, created_by)
        REFERENCES accounts (tenant_id, id)
);

CREATE UNIQUE INDEX euer_assignments_first_once
    ON euer_assignments (tenant_id, entry_kind, entry_id)
    WHERE supersedes_id IS NULL;

CREATE UNIQUE INDEX euer_assignments_superseded_once
    ON euer_assignments (tenant_id, supersedes_id)
    WHERE supersedes_id IS NOT NULL;

CREATE INDEX euer_assignments_tax_year_idx ON euer_assignments (tenant_id, tax_year);

CREATE FUNCTION euer_assignments_block_mutation() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
BEGIN
    RAISE EXCEPTION 'euer assignment % is append-only; record a new row naming it in supersedes_id', OLD.id
        USING ERRCODE = 'integrity_constraint_violation';
END;
$$;

CREATE TRIGGER euer_assignments_block_mutation
    BEFORE UPDATE OR DELETE ON euer_assignments
    FOR EACH ROW
    EXECUTE FUNCTION euer_assignments_block_mutation();

-- A row trigger does not fire on TRUNCATE. The application role holds no
-- TRUNCATE privilege; this is defence in depth for every other role.
CREATE FUNCTION euer_assignments_block_truncate() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
BEGIN
    RAISE EXCEPTION 'euer assignments are never truncated'
        USING ERRCODE = 'integrity_constraint_violation';
END;
$$;

CREATE TRIGGER euer_assignments_block_truncate
    BEFORE TRUNCATE ON euer_assignments
    FOR EACH STATEMENT
    EXECUTE FUNCTION euer_assignments_block_truncate();

-- euer_assignments_check_insert keeps a direct INSERT from storing what
-- the application never would:
--   - The entry must exist in the tenant: a payment that is no reversal
--     (a reversal follows the payment it reverses), a booked expense that
--     is no asset expense (the EÜR never counts it), or a booked other
--     income entry. Otherwise 23503, like a refused foreign key.
--   - Its date (received_on or paid_on) must lie from 22 December to 10
--     January, and tax_year must be the year of that date or the
--     neighbouring year: the next one for a December date, the previous
--     one for a January date. Otherwise 23514, like a refused CHECK.
--
-- The lookups run under the inserting session's row level security, so a
-- row of another tenant is simply not found. This trigger runs before the
-- row level security WITH CHECK of the policy below, so an insert naming
-- another tenant is refused here first, with 23503.
CREATE FUNCTION euer_assignments_check_insert() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
DECLARE
    entry_date date;
BEGIN
    IF NEW.entry_kind = 'payment' THEN
        SELECT received_on INTO entry_date
        FROM payments
        WHERE tenant_id = NEW.tenant_id AND id = NEW.entry_id AND reverses_payment_id IS NULL;
    ELSIF NEW.entry_kind = 'expense' THEN
        SELECT paid_on INTO entry_date
        FROM expenses
        WHERE tenant_id = NEW.tenant_id AND id = NEW.entry_id AND state = 'booked' AND NOT is_asset;
    ELSIF NEW.entry_kind = 'other_income' THEN
        SELECT received_on INTO entry_date
        FROM other_income
        WHERE tenant_id = NEW.tenant_id AND id = NEW.entry_id AND state = 'booked';
    END IF;

    IF entry_date IS NULL THEN
        RAISE EXCEPTION 'euer assignment % names no assignable % %', NEW.id, NEW.entry_kind, NEW.entry_id
            USING ERRCODE = 'foreign_key_violation';
    END IF;

    IF NOT (
        (extract(month FROM entry_date) = 12 AND extract(day FROM entry_date) >= 22
            AND NEW.tax_year IN (extract(year FROM entry_date)::integer, extract(year FROM entry_date)::integer + 1))
        OR (extract(month FROM entry_date) = 1 AND extract(day FROM entry_date) <= 10
            AND NEW.tax_year IN (extract(year FROM entry_date)::integer, extract(year FROM entry_date)::integer - 1))
    ) THEN
        RAISE EXCEPTION 'euer assignment %: % to tax year % lies outside the turn of the year', NEW.id, entry_date, NEW.tax_year
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER euer_assignments_check_insert
    BEFORE INSERT ON euer_assignments
    FOR EACH ROW
    EXECUTE FUNCTION euer_assignments_check_insert();

-- ENABLE ALWAYS, see 20261113160000_create_documents.sql: a plain ENABLE
-- trigger is skipped under session_replication_role = replica.
ALTER TABLE euer_assignments ENABLE ALWAYS TRIGGER euer_assignments_block_mutation;
ALTER TABLE euer_assignments ENABLE ALWAYS TRIGGER euer_assignments_block_truncate;
ALTER TABLE euer_assignments ENABLE ALWAYS TRIGGER euer_assignments_check_insert;

-- Row level security, the same policy shape as every other tenant scoped
-- table: see 20261112233249_create_accounts.sql for why ENABLE and FORCE
-- are both needed and why NULLIF folds a reverted setting to NULL.
ALTER TABLE euer_assignments ENABLE ROW LEVEL SECURITY;
ALTER TABLE euer_assignments FORCE ROW LEVEL SECURITY;

CREATE POLICY euer_assignments_tenant_isolation ON euer_assignments
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- migrate:down

DROP TABLE euer_assignments;
DROP FUNCTION euer_assignments_check_insert();
DROP FUNCTION euer_assignments_block_truncate();
DROP FUNCTION euer_assignments_block_mutation();
