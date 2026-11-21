-- Tenant isolation as in seller_profiles.sql: row level security is the
-- guarantee, the tenant_id predicate is a second, explicit one. There is
-- no DELETE query, and the application role has no such grant.

-- name: CreateAsset :one
INSERT INTO assets (
    tenant_id, name, acquired_on, cost_basis_cents, method, asset_group, useful_life_months,
    expense_id, opening_book_value_cents, opening_book_value_year, imported
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetAsset :one
SELECT * FROM assets
WHERE id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- name: ListAssets :many
SELECT * FROM assets
WHERE tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
ORDER BY acquired_on, created_at, id;

-- UpdateAsset replaces the editable fields while the stored row's first
-- schedule year (the opening year of an imported asset, the year of
-- acquired_on otherwise) is not before open_year, the caller's current
-- year, and the asset is not disposed. No row back means not found or
-- refused; the repository tells the two apart. imported and the disposal
-- columns are never changed here.
--
-- name: UpdateAsset :one
UPDATE assets SET
    name = sqlc.arg('name'),
    acquired_on = sqlc.arg('acquired_on'),
    cost_basis_cents = sqlc.arg('cost_basis_cents'),
    method = sqlc.arg('method'),
    asset_group = sqlc.arg('asset_group'),
    useful_life_months = sqlc.narg('useful_life_months'),
    expense_id = sqlc.narg('expense_id'),
    opening_book_value_cents = sqlc.narg('opening_book_value_cents'),
    opening_book_value_year = sqlc.narg('opening_book_value_year'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
  AND disposed_on IS NULL
  AND COALESCE(opening_book_value_year, extract(year FROM acquired_on)::integer) >= sqlc.arg('open_year')::integer
RETURNING *;

-- RecordAssetDisposal sets the disposal once. No row back means not found
-- or already disposed.
--
-- name: RecordAssetDisposal :one
UPDATE assets SET
    disposed_on = sqlc.arg('disposed_on'),
    disposal_proceeds_cents = sqlc.narg('disposal_proceeds_cents'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
  AND disposed_on IS NULL
RETURNING *;
