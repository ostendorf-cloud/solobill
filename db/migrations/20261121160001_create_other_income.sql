-- migrate:up

-- other_income holds every Betriebseinnahme outside an invoice (issue
-- #76): a VAT refund of the Finanzamt, the sale or withdrawal of an asset,
-- private use. received_on is the Zufluss date section 11 Abs. 1 EStG
-- counts by: the EÜR year of an entry is the calendar year of received_on.
--
-- Same lifecycle, immutability and storno rule as expenses; see
-- 20261121160000_create_expenses.sql for the reasoning, which is not
-- repeated here.
CREATE TABLE other_income (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    state text NOT NULL DEFAULT 'draft',
    received_on date,
    description text NOT NULL DEFAULT '',
    gross_cents bigint NOT NULL,
    net_cents bigint NOT NULL,
    vat_rate_permyriad integer NOT NULL DEFAULT 0,
    vat_cents bigint NOT NULL DEFAULT 0,
    -- An EÜR line key of kind income (internal/category), empty on a
    -- draft that has none yet.
    category text NOT NULL DEFAULT '',
    file_id uuid,
    storno_of_id uuid,
    imported boolean NOT NULL DEFAULT false,
    booked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT other_income_state_check CHECK (state IN ('draft', 'booked')),
    CONSTRAINT other_income_booked_at_consistent CHECK ((state = 'booked') = (booked_at IS NOT NULL)),
    CONSTRAINT other_income_booked_complete
        CHECK (state = 'draft' OR (received_on IS NOT NULL AND category <> '')),
    CONSTRAINT other_income_category_shape
        CHECK (category = '' OR category ~ '^euer\.z[0-9]{1,3}(\.(nd|d))?$'),
    CONSTRAINT other_income_amounts_add_up CHECK (net_cents + vat_cents = gross_cents),
    CONSTRAINT other_income_vat_rate_range CHECK (vat_rate_permyriad BETWEEN 0 AND 10000),
    CONSTRAINT other_income_amount_sign CHECK (
        (storno_of_id IS NULL AND gross_cents > 0 AND net_cents >= 0 AND vat_cents >= 0)
        OR (storno_of_id IS NOT NULL AND state = 'booked'
            AND gross_cents < 0 AND net_cents <= 0 AND vat_cents <= 0)
    ),
    CONSTRAINT other_income_amount_bound CHECK (abs(gross_cents) <= 100000000000),
    CONSTRAINT other_income_description_length CHECK (char_length(description) <= 1000),
    CONSTRAINT other_income_not_self_storno CHECK (storno_of_id IS NULL OR storno_of_id <> id),
    CONSTRAINT other_income_tenant_id_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT other_income_file_fkey FOREIGN KEY (tenant_id, file_id)
        REFERENCES files (tenant_id, id),
    CONSTRAINT other_income_storno_fkey FOREIGN KEY (tenant_id, storno_of_id)
        REFERENCES other_income (tenant_id, id)
);

CREATE UNIQUE INDEX other_income_reversed_once
    ON other_income (tenant_id, storno_of_id)
    WHERE storno_of_id IS NOT NULL;

CREATE INDEX other_income_received_on_idx ON other_income (tenant_id, received_on, created_at, id);

CREATE FUNCTION other_income_block_booked_mutation() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
BEGIN
    IF OLD.state = 'booked' THEN
        RAISE EXCEPTION 'other income % is booked and is immutable; book a storno instead', OLD.id
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER other_income_block_booked_mutation
    BEFORE UPDATE OR DELETE ON other_income
    FOR EACH ROW
    EXECUTE FUNCTION other_income_block_booked_mutation();

CREATE FUNCTION other_income_block_truncate() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
BEGIN
    RAISE EXCEPTION 'other income is never truncated'
        USING ERRCODE = 'integrity_constraint_violation';
END;
$$;

CREATE TRIGGER other_income_block_truncate
    BEFORE TRUNCATE ON other_income
    FOR EACH STATEMENT
    EXECUTE FUNCTION other_income_block_truncate();

CREATE FUNCTION other_income_check_storno() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
DECLARE
    original other_income%ROWTYPE;
BEGIN
    IF NEW.storno_of_id IS NULL THEN
        RETURN NEW;
    END IF;

    SELECT * INTO original
    FROM other_income
    WHERE tenant_id = NEW.tenant_id AND id = NEW.storno_of_id;

    IF NOT FOUND
        OR original.state <> 'booked'
        OR original.storno_of_id IS NOT NULL
        OR NEW.gross_cents <> -original.gross_cents
        OR NEW.net_cents <> -original.net_cents
        OR NEW.vat_cents <> -original.vat_cents
        OR NEW.vat_rate_permyriad <> original.vat_rate_permyriad
        OR NEW.category <> original.category
    THEN
        RAISE EXCEPTION 'storno row % does not reverse booked other income', NEW.id
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER other_income_check_storno
    BEFORE INSERT ON other_income
    FOR EACH ROW
    EXECUTE FUNCTION other_income_check_storno();

ALTER TABLE other_income ENABLE ALWAYS TRIGGER other_income_block_booked_mutation;
ALTER TABLE other_income ENABLE ALWAYS TRIGGER other_income_block_truncate;
ALTER TABLE other_income ENABLE ALWAYS TRIGGER other_income_check_storno;

ALTER TABLE other_income ENABLE ROW LEVEL SECURITY;
ALTER TABLE other_income FORCE ROW LEVEL SECURITY;

CREATE POLICY other_income_tenant_isolation ON other_income
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- migrate:down

DROP TABLE other_income;
DROP FUNCTION other_income_check_storno();
DROP FUNCTION other_income_block_truncate();
DROP FUNCTION other_income_block_booked_mutation();
