-- Every query names the tenant in its WHERE clause or as a column value,
-- and row level security enforces the same scope below it; see
-- customers.sql for why both layers.
--
-- A booked row is never changed or deleted: every write below is guarded
-- by "state = 'draft'", and expenses_block_booked_mutation (see
-- db/migrations) refuses the write for any statement that is not.

-- name: CreateExpense :one
INSERT INTO expenses (
    tenant_id, paid_on, receipt_date, supplier, description, gross_cents, net_cents,
    vat_rate_permyriad, vat_cents, category, file_id, is_asset, imported
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING id, tenant_id, state, paid_on, receipt_date, supplier, description, gross_cents, net_cents,
    vat_rate_permyriad, vat_cents, category, file_id, is_asset, storno_of_id, imported, booked_at,
    created_at, updated_at;

-- name: GetExpenseByID :one
SELECT id, tenant_id, state, paid_on, receipt_date, supplier, description, gross_cents, net_cents,
    vat_rate_permyriad, vat_cents, category, file_id, is_asset, storno_of_id, imported, booked_at,
    created_at, updated_at
FROM expenses
WHERE id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- ListExpenses filters on the half open date range of one year of paid_on
-- when paid_from and paid_before are set, which also leaves out a draft
-- without paid_on. The order is stable: paid_on, then creation, then id,
-- drafts without paid_on last.
--
-- name: ListExpenses :many
SELECT id, tenant_id, state, paid_on, receipt_date, supplier, description, gross_cents, net_cents,
    vat_rate_permyriad, vat_cents, category, file_id, is_asset, storno_of_id, imported, booked_at,
    created_at, updated_at
FROM expenses
WHERE tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    AND (sqlc.narg(paid_from)::date IS NULL OR paid_on >= sqlc.narg(paid_from)::date)
    AND (sqlc.narg(paid_before)::date IS NULL OR paid_on < sqlc.narg(paid_before)::date)
    AND (sqlc.narg(state)::text IS NULL OR state = sqlc.narg(state)::text)
    AND (sqlc.narg(category)::text IS NULL OR category = sqlc.narg(category)::text)
    AND (sqlc.narg(is_asset)::boolean IS NULL OR is_asset = sqlc.narg(is_asset)::boolean)
ORDER BY paid_on NULLS LAST, created_at, id;

-- name: UpdateDraftExpense :one
UPDATE expenses
SET paid_on = $2,
    receipt_date = $3,
    supplier = $4,
    description = $5,
    gross_cents = $6,
    net_cents = $7,
    vat_rate_permyriad = $8,
    vat_cents = $9,
    category = $10,
    file_id = $11,
    is_asset = $12,
    updated_at = now()
WHERE id = $1 AND state = 'draft' AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
RETURNING id, tenant_id, state, paid_on, receipt_date, supplier, description, gross_cents, net_cents,
    vat_rate_permyriad, vat_cents, category, file_id, is_asset, storno_of_id, imported, booked_at,
    created_at, updated_at;

-- DeleteDraftExpense is a real DELETE: a draft has no legal weight yet.
-- A booked row never matches, and the trigger refuses it anyway.
--
-- name: DeleteDraftExpense :execrows
DELETE FROM expenses
WHERE id = $1 AND state = 'draft' AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- name: BookExpense :one
UPDATE expenses
SET state = 'booked',
    booked_at = $2,
    updated_at = now()
WHERE id = $1 AND state = 'draft' AND paid_on IS NOT NULL AND category <> ''
    AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
RETURNING id, tenant_id, state, paid_on, receipt_date, supplier, description, gross_cents, net_cents,
    vat_rate_permyriad, vat_cents, category, file_id, is_asset, storno_of_id, imported, booked_at,
    created_at, updated_at;

-- CreateExpenseStorno books the reversing row of a booked original that is
-- not a storno itself, copied from the original with negated amounts.
-- expenses_reversed_once refuses a second storno of the same original,
-- also under concurrency, and expenses_check_storno checks the copy again.
--
-- name: CreateExpenseStorno :one
INSERT INTO expenses (
    tenant_id, state, paid_on, receipt_date, supplier, description, gross_cents, net_cents,
    vat_rate_permyriad, vat_cents, category, file_id, is_asset, storno_of_id, booked_at
)
SELECT original.tenant_id, 'booked', sqlc.arg(paid_on)::date, original.receipt_date,
    original.supplier, original.description, -original.gross_cents, -original.net_cents,
    original.vat_rate_permyriad, -original.vat_cents, original.category, original.file_id,
    original.is_asset, original.id, sqlc.arg(booked_at)::timestamptz
FROM expenses AS original
WHERE original.id = sqlc.arg(original_id) AND original.state = 'booked' AND original.storno_of_id IS NULL
    AND original.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
RETURNING id, tenant_id, state, paid_on, receipt_date, supplier, description, gross_cents, net_cents,
    vat_rate_permyriad, vat_cents, category, file_id, is_asset, storno_of_id, imported, booked_at,
    created_at, updated_at;
