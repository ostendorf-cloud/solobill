-- migrate:up

-- assets is the asset register (issue #78; docs/concepts/solobill.md,
-- "Depreciation scope"). A row holds what the yearly AfA schedule is
-- computed from. The schedule itself is never stored: internal/afa
-- computes it from these columns on every read.
--
-- There is no degressive method, on purpose: assets_method_check names
-- the four methods of the owner's decision and nothing else.
--
-- Immutability: an asset is edited only while its first schedule year is
-- open (the calendar year of acquired_on, or opening_book_value_year for
-- an imported asset). The application enforces that in the UPDATE itself
-- (UpdateAsset in internal/storage/postgres/queries/assets.sql), against
-- the clock the caller passes, so it stays testable. A disposal is set
-- once (RecordAssetDisposal only matches a row without one). The
-- application role has no DELETE and no TRUNCATE grant
-- (docker/postgres/grants/20261121170000_assets.sql). No trigger is added:
-- the contract (docs/concepts/solobill.md, "Migration timestamp
-- reservation") plans none for this table, unlike payments and expenses.
CREATE TABLE assets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    name text NOT NULL,
    acquired_on date NOT NULL,
    -- Net of deductible Vorsteuer under the standard regime, gross for a
    -- Kleinunternehmer (internal/afa.CostBasis; that reading is not
    -- verified with a Steuerberater).
    cost_basis_cents bigint NOT NULL,
    method text NOT NULL,
    -- "group" is a reserved word in SQL.
    asset_group text NOT NULL,
    useful_life_months integer,
    -- The booked expense with is_asset set the asset came from, NULL for
    -- an asset entered without one. assets_expense_fkey keeps it inside
    -- the tenant. That it is booked, has is_asset set and is not a storno
    -- row is the application's check (checkAssetExpense in
    -- internal/httpapi/routes_assets.go): a CHECK cannot read another
    -- table.
    expense_id uuid,
    -- An imported asset's book value at 1 January of the opening year.
    opening_book_value_cents bigint,
    opening_book_value_year integer,
    disposed_on date,
    disposal_proceeds_cents bigint,
    imported boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT assets_name_check CHECK (name <> '' AND char_length(name) <= 200),
    CONSTRAINT assets_cost_basis_positive CHECK (cost_basis_cents > 0),
    CONSTRAINT assets_method_check CHECK (method IN ('linear', 'gwg', 'sammelposten', 'immediate')),
    CONSTRAINT assets_group_check CHECK (asset_group IN ('intangible', 'office_equipment', 'other_movable')),
    -- A useful life belongs to the linear method and only to it.
    CONSTRAINT assets_useful_life_linear_only CHECK (
        (method = 'linear') = (useful_life_months IS NOT NULL)
    ),
    CONSTRAINT assets_useful_life_range CHECK (useful_life_months BETWEEN 1 AND 600),
    -- The thresholds of section 6 Abs. 2 and 2a EStG, the same bounds
    -- internal/afa.Validate checks.
    CONSTRAINT assets_immediate_limit CHECK (method <> 'immediate' OR cost_basis_cents <= 25000),
    CONSTRAINT assets_gwg_limit CHECK (method <> 'gwg' OR cost_basis_cents <= 80000),
    CONSTRAINT assets_sammelposten_range CHECK (
        method <> 'sammelposten' OR (cost_basis_cents > 25000 AND cost_basis_cents <= 100000)
    ),
    CONSTRAINT assets_opening_together CHECK (
        (opening_book_value_cents IS NULL) = (opening_book_value_year IS NULL)
    ),
    CONSTRAINT assets_opening_value_range CHECK (
        opening_book_value_cents BETWEEN 0 AND cost_basis_cents
    ),
    CONSTRAINT assets_opening_year_after_acquisition CHECK (
        opening_book_value_year > extract(year FROM acquired_on)
    ),
    CONSTRAINT assets_disposed_after_acquisition CHECK (disposed_on >= acquired_on),
    CONSTRAINT assets_proceeds_need_disposal CHECK (
        disposal_proceeds_cents IS NULL OR (disposed_on IS NOT NULL AND disposal_proceeds_cents >= 0)
    ),
    -- Composite foreign key target, see
    -- 20261113150832_create_customers.sql for why a single column
    -- reference would be a cross tenant existence oracle.
    CONSTRAINT assets_tenant_id_id_unique UNIQUE (tenant_id, id),
    -- Composite, so an asset cannot name an expense of another tenant.
    -- expenses_tenant_id_id_unique is the target. A booked expense is
    -- never deleted (its trigger), so the reference cannot dangle.
    CONSTRAINT assets_expense_fkey FOREIGN KEY (tenant_id, expense_id)
        REFERENCES expenses (tenant_id, id)
);

-- One expense becomes at most one asset, or its cost would be
-- depreciated twice. unique_violation (23505) maps to ErrConflict.
CREATE UNIQUE INDEX assets_expense_once ON assets (tenant_id, expense_id) WHERE expense_id IS NOT NULL;

CREATE INDEX assets_acquired_on_idx ON assets (tenant_id, acquired_on);

-- Row level security, the same policy shape as every other tenant scoped
-- table: see 20261112233249_create_accounts.sql for why ENABLE and FORCE
-- are both needed and why NULLIF folds a reverted setting to NULL.
ALTER TABLE assets ENABLE ROW LEVEL SECURITY;
ALTER TABLE assets FORCE ROW LEVEL SECURITY;

CREATE POLICY assets_tenant_isolation ON assets
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- migrate:down

DROP TABLE assets;
