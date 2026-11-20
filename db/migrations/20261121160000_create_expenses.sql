-- migrate:up

-- expenses holds every Betriebsausgabe with its receipt (issue #76;
-- docs/concepts/solobill.md, "EÜR mapping as data per tax year" and
-- "Receipt storage and GoBD"). paid_on is the Abfluss date section 11
-- Abs. 2 EStG counts by: the EÜR year of an expense is the calendar year
-- of paid_on, never of receipt_date.
--
-- A row is a draft until it is booked. A draft may be changed and deleted.
-- A booked row is immutable: expenses_block_booked_mutation refuses every
-- UPDATE and DELETE of it, for any role. A mistake in a booked row is
-- fixed by a storno row (storno_of_id names the original, the amounts are
-- negated) plus a new row (section 146 Abs. 4 AO: no change may hide the
-- original).
--
-- Amounts are integer cents, never a float. Under the Kleinunternehmer
-- regime the VAT stays inside gross_cents: net_cents equals gross_cents
-- and vat_cents is 0. The regime depends on paid_on and lives in
-- vat_regime_periods, so the application checks it; the table checks
-- what holds under both regimes.
CREATE TABLE expenses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    state text NOT NULL DEFAULT 'draft',
    paid_on date,
    receipt_date date,
    supplier text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '',
    gross_cents bigint NOT NULL,
    net_cents bigint NOT NULL,
    vat_rate_permyriad integer NOT NULL DEFAULT 0,
    vat_cents bigint NOT NULL DEFAULT 0,
    -- An EÜR line key of kind expense (internal/category), empty on a
    -- draft that has none yet. The set of keys lives in Go; the table
    -- checks the shape only.
    category text NOT NULL DEFAULT '',
    -- The receipt, a files row of the same tenant. NULL until uploaded.
    -- Booking without a receipt is allowed; the EÜR report flags it.
    file_id uuid,
    is_asset boolean NOT NULL DEFAULT false,
    storno_of_id uuid,
    imported boolean NOT NULL DEFAULT false,
    booked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT expenses_state_check CHECK (state IN ('draft', 'booked')),
    -- booked_at is set exactly when the row is booked.
    CONSTRAINT expenses_booked_at_consistent CHECK ((state = 'booked') = (booked_at IS NOT NULL)),
    -- A booked row always has the date its year comes from and a line key.
    CONSTRAINT expenses_booked_complete
        CHECK (state = 'draft' OR (paid_on IS NOT NULL AND category <> '')),
    CONSTRAINT expenses_category_shape
        CHECK (category = '' OR category ~ '^euer\.z[0-9]{1,3}(\.(nd|d))?$'),
    CONSTRAINT expenses_amounts_add_up CHECK (net_cents + vat_cents = gross_cents),
    CONSTRAINT expenses_vat_rate_range CHECK (vat_rate_permyriad BETWEEN 0 AND 10000),
    -- An entry is positive. Only a storno row carries negated amounts, and
    -- a storno row is always booked.
    CONSTRAINT expenses_amount_sign CHECK (
        (storno_of_id IS NULL AND gross_cents > 0 AND net_cents >= 0 AND vat_cents >= 0)
        OR (storno_of_id IS NOT NULL AND state = 'booked'
            AND gross_cents < 0 AND net_cents <= 0 AND vat_cents <= 0)
    ),
    -- 1,000,000,000.00 EUR. Far above any expense of a sole trader, and
    -- far below where a sum of rows could overflow a bigint.
    CONSTRAINT expenses_amount_bound CHECK (abs(gross_cents) <= 100000000000),
    CONSTRAINT expenses_supplier_length CHECK (char_length(supplier) <= 200),
    CONSTRAINT expenses_description_length CHECK (char_length(description) <= 1000),
    CONSTRAINT expenses_not_self_storno CHECK (storno_of_id IS NULL OR storno_of_id <> id),
    -- Composite foreign key target, see 20261113150832_create_customers.sql
    -- for why a single column reference would be a cross tenant existence
    -- oracle.
    CONSTRAINT expenses_tenant_id_id_unique UNIQUE (tenant_id, id),
    -- The receipt must be a file of the same tenant. files is written once
    -- (its grant and its trigger), so the hash a row was booked with
    -- cannot drift.
    CONSTRAINT expenses_file_fkey FOREIGN KEY (tenant_id, file_id)
        REFERENCES files (tenant_id, id),
    CONSTRAINT expenses_storno_fkey FOREIGN KEY (tenant_id, storno_of_id)
        REFERENCES expenses (tenant_id, id)
);

-- An expense is reversed at most once.
CREATE UNIQUE INDEX expenses_reversed_once
    ON expenses (tenant_id, storno_of_id)
    WHERE storno_of_id IS NOT NULL;

-- GET /api/v1/expenses filters by the year of paid_on and orders by it.
CREATE INDEX expenses_paid_on_idx ON expenses (tenant_id, paid_on, created_at, id);

-- A booked row is immutable. A draft may change, including the one
-- transition to booked. The error class integrity_constraint_violation
-- is what internal/storage/postgres maps to storage.ErrConflict, the same
-- as documents_block_issued_mutation. search_path is pinned for the same
-- reason as there.
CREATE FUNCTION expenses_block_booked_mutation() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
BEGIN
    IF OLD.state = 'booked' THEN
        RAISE EXCEPTION 'expense % is booked and is immutable; book a storno instead', OLD.id
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER expenses_block_booked_mutation
    BEFORE UPDATE OR DELETE ON expenses
    FOR EACH ROW
    EXECUTE FUNCTION expenses_block_booked_mutation();

-- A row trigger does not fire on TRUNCATE. The application role holds no
-- TRUNCATE privilege; this is defence in depth for every other role.
CREATE FUNCTION expenses_block_truncate() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
BEGIN
    RAISE EXCEPTION 'expenses are never truncated'
        USING ERRCODE = 'integrity_constraint_violation';
END;
$$;

CREATE TRIGGER expenses_block_truncate
    BEFORE TRUNCATE ON expenses
    FOR EACH STATEMENT
    EXECUTE FUNCTION expenses_block_truncate();

-- A storno row must reverse exactly one booked original that is not a
-- storno itself, with the negated amounts and the same rate, category and
-- asset flag. The application builds the row that way; this keeps a
-- direct INSERT from booking anything else under the storno label. The
-- lookup runs under the inserting session's row level security, so it
-- only ever sees the tenant's own rows.
CREATE FUNCTION expenses_check_storno() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
DECLARE
    original expenses%ROWTYPE;
BEGIN
    IF NEW.storno_of_id IS NULL THEN
        RETURN NEW;
    END IF;

    SELECT * INTO original
    FROM expenses
    WHERE tenant_id = NEW.tenant_id AND id = NEW.storno_of_id;

    IF NOT FOUND
        OR original.state <> 'booked'
        OR original.storno_of_id IS NOT NULL
        OR NEW.gross_cents <> -original.gross_cents
        OR NEW.net_cents <> -original.net_cents
        OR NEW.vat_cents <> -original.vat_cents
        OR NEW.vat_rate_permyriad <> original.vat_rate_permyriad
        OR NEW.category <> original.category
        OR NEW.is_asset <> original.is_asset
    THEN
        RAISE EXCEPTION 'storno row % does not reverse a booked expense', NEW.id
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER expenses_check_storno
    BEFORE INSERT ON expenses
    FOR EACH ROW
    EXECUTE FUNCTION expenses_check_storno();

-- ENABLE ALWAYS, see 20261113160000_create_documents.sql: a plain ENABLE
-- trigger is skipped under session_replication_role = replica.
ALTER TABLE expenses ENABLE ALWAYS TRIGGER expenses_block_booked_mutation;
ALTER TABLE expenses ENABLE ALWAYS TRIGGER expenses_block_truncate;
ALTER TABLE expenses ENABLE ALWAYS TRIGGER expenses_check_storno;

-- Row level security, the same policy shape as every other tenant scoped
-- table: see 20261112233249_create_accounts.sql for why ENABLE and FORCE
-- are both needed and why NULLIF folds a reverted setting to NULL.
ALTER TABLE expenses ENABLE ROW LEVEL SECURITY;
ALTER TABLE expenses FORCE ROW LEVEL SECURITY;

CREATE POLICY expenses_tenant_isolation ON expenses
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- migrate:down

DROP TABLE expenses;
DROP FUNCTION expenses_check_storno();
DROP FUNCTION expenses_block_truncate();
DROP FUNCTION expenses_block_booked_mutation();
