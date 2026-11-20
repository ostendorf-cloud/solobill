-- migrate:up

-- vat_regime_periods is the history of the VAT regime of a tenant (issue
-- #68) and the source of the regime on a date: the regime of the latest
-- effective period whose valid_from is on or before that date. A switch in
-- the middle of a year is one more row.
--
-- The table is append-only. The application role gets SELECT and INSERT
-- only (docker/postgres/grants/20261121100000_seller_profile.sql), so a
-- raw UPDATE or DELETE is refused by the grant itself. A wrong row is
-- corrected by a new row that names it in supersedes_id. A superseded row
-- no longer counts. The correcting row carries its own valid_from.
CREATE TABLE vat_regime_periods (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    valid_from date NOT NULL,
    regime text NOT NULL,
    -- The owner's own reason, free text.
    note text,
    supersedes_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT vat_regime_periods_regime_check CHECK (regime IN ('standard', 'kleinunternehmer')),
    CONSTRAINT vat_regime_periods_note_not_empty CHECK (note IS NULL OR note <> ''),
    CONSTRAINT vat_regime_periods_not_self_superseding CHECK (supersedes_id IS NULL OR supersedes_id <> id),
    -- Composite foreign key target, see
    -- 20261113150832_create_customers.sql for why a single column
    -- reference would be a cross tenant existence oracle.
    CONSTRAINT vat_regime_periods_tenant_id_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT vat_regime_periods_supersedes_fkey FOREIGN KEY (tenant_id, supersedes_id)
        REFERENCES vat_regime_periods (tenant_id, id)
);

-- A row is superseded at most once: no two rows name the same row.
CREATE UNIQUE INDEX vat_regime_periods_superseded_once
    ON vat_regime_periods (tenant_id, supersedes_id)
    WHERE supersedes_id IS NOT NULL;

CREATE INDEX vat_regime_periods_valid_from_idx ON vat_regime_periods (tenant_id, valid_from, created_at);

ALTER TABLE vat_regime_periods ENABLE ROW LEVEL SECURITY;
ALTER TABLE vat_regime_periods FORCE ROW LEVEL SECURITY;

CREATE POLICY vat_regime_periods_tenant_isolation ON vat_regime_periods
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- migrate:down

DROP TABLE vat_regime_periods;
