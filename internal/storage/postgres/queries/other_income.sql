-- The queries of other_income match expenses.sql one to one; see there for
-- the tenant scope and the draft guard.

-- name: CreateOtherIncome :one
INSERT INTO other_income (
    tenant_id, received_on, description, gross_cents, net_cents, vat_rate_permyriad, vat_cents,
    category, file_id, imported
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, tenant_id, state, received_on, description, gross_cents, net_cents, vat_rate_permyriad,
    vat_cents, category, file_id, storno_of_id, imported, booked_at, created_at, updated_at;

-- name: GetOtherIncomeByID :one
SELECT id, tenant_id, state, received_on, description, gross_cents, net_cents, vat_rate_permyriad,
    vat_cents, category, file_id, storno_of_id, imported, booked_at, created_at, updated_at
FROM other_income
WHERE id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- name: ListOtherIncome :many
SELECT id, tenant_id, state, received_on, description, gross_cents, net_cents, vat_rate_permyriad,
    vat_cents, category, file_id, storno_of_id, imported, booked_at, created_at, updated_at
FROM other_income
WHERE tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    AND (sqlc.narg(received_from)::date IS NULL OR received_on >= sqlc.narg(received_from)::date)
    AND (sqlc.narg(received_before)::date IS NULL OR received_on < sqlc.narg(received_before)::date)
    AND (sqlc.narg(state)::text IS NULL OR state = sqlc.narg(state)::text)
    AND (sqlc.narg(category)::text IS NULL OR category = sqlc.narg(category)::text)
ORDER BY received_on NULLS LAST, created_at, id;

-- name: UpdateDraftOtherIncome :one
UPDATE other_income
SET received_on = $2,
    description = $3,
    gross_cents = $4,
    net_cents = $5,
    vat_rate_permyriad = $6,
    vat_cents = $7,
    category = $8,
    file_id = $9,
    updated_at = now()
WHERE id = $1 AND state = 'draft' AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
RETURNING id, tenant_id, state, received_on, description, gross_cents, net_cents, vat_rate_permyriad,
    vat_cents, category, file_id, storno_of_id, imported, booked_at, created_at, updated_at;

-- name: DeleteDraftOtherIncome :execrows
DELETE FROM other_income
WHERE id = $1 AND state = 'draft' AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- name: BookOtherIncome :one
UPDATE other_income
SET state = 'booked',
    booked_at = $2,
    updated_at = now()
WHERE id = $1 AND state = 'draft' AND received_on IS NOT NULL AND category <> ''
    AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
RETURNING id, tenant_id, state, received_on, description, gross_cents, net_cents, vat_rate_permyriad,
    vat_cents, category, file_id, storno_of_id, imported, booked_at, created_at, updated_at;

-- name: CreateOtherIncomeStorno :one
INSERT INTO other_income (
    tenant_id, state, received_on, description, gross_cents, net_cents, vat_rate_permyriad,
    vat_cents, category, file_id, storno_of_id, booked_at
)
SELECT original.tenant_id, 'booked', sqlc.arg(received_on)::date, original.description,
    -original.gross_cents, -original.net_cents, original.vat_rate_permyriad, -original.vat_cents,
    original.category, original.file_id, original.id, sqlc.arg(booked_at)::timestamptz
FROM other_income AS original
WHERE original.id = sqlc.arg(original_id) AND original.state = 'booked' AND original.storno_of_id IS NULL
    AND original.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
RETURNING id, tenant_id, state, received_on, description, gross_cents, net_cents, vat_rate_permyriad,
    vat_cents, category, file_id, storno_of_id, imported, booked_at, created_at, updated_at;
