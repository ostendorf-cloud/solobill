-- Same reasoning as customers.sql: tenant isolation here is two
-- independent layers, matching docs/concepts/solobill.md. Every query
-- below names tenant_id in its own WHERE clause (or, for
-- CreateCatalogueItem, as a column value) as the first, defense-in-depth
-- layer; row level security (see db/migrations) is the independent
-- second layer, enforcing the same scope below these queries regardless
-- of what they ask for.

-- name: CreateCatalogueItem :one
INSERT INTO catalogue_items (tenant_id, name, description, sku, unit_price_cents)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, tenant_id, name, description, sku, unit_price_cents, deleted_at, created_at, updated_at;

-- GetCatalogueItemByID deliberately does not filter on deleted_at: a
-- later document still has to resolve a deleted catalogue item by id
-- (see internal/storage.CatalogueItemRepository.FindByID), so a point
-- lookup by id must keep answering for an already soft-deleted row.
--
-- name: GetCatalogueItemByID :one
SELECT id, tenant_id, name, description, sku, unit_price_cents, deleted_at, created_at, updated_at
FROM catalogue_items
WHERE id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- ListCatalogueItems filters deleted_at IS NULL, unlike
-- GetCatalogueItemByID: this is the human-facing picker view (a
-- document line's "choose an item" step), which must only ever offer
-- active items. Ordered by name, unlike ListAccounts' created_at, for
-- the same reason: a human scanning a picker expects it alphabetical,
-- not in creation order.
--
-- name: ListCatalogueItems :many
SELECT id, tenant_id, name, description, sku, unit_price_cents, deleted_at, created_at, updated_at
FROM catalogue_items
WHERE deleted_at IS NULL AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
ORDER BY name;

-- UpdateCatalogueItem is guarded by "AND deleted_at IS NULL": a
-- soft-deleted item cannot be edited back to life through an ordinary
-- update; see internal/storage.CatalogueItemRepository.Update for why a
-- zero-row RETURNING here maps to storage.ErrNotFound, not
-- storage.ErrConflict.
--
-- name: UpdateCatalogueItem :one
UPDATE catalogue_items
SET name = $2,
    description = $3,
    sku = $4,
    unit_price_cents = $5,
    updated_at = now()
WHERE id = $1 AND deleted_at IS NULL AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
RETURNING id, tenant_id, name, description, sku, unit_price_cents, deleted_at, created_at, updated_at;

-- DeleteCatalogueItem is the soft delete itself: an UPDATE, never a
-- DELETE, guarded by "AND deleted_at IS NULL" so a second call against
-- an already-deleted row affects zero rows rather than re-stamping
-- deleted_at. :execrows rather than :one: the caller only needs the
-- affected-row count to tell "deleted" from "already deleted or not
-- found" apart, not a RETURNING row it would have to scan and discard.
--
-- name: DeleteCatalogueItem :execrows
UPDATE catalogue_items
SET deleted_at = now()
WHERE id = $1 AND deleted_at IS NULL AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;
