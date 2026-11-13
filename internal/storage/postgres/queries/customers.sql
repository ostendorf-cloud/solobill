-- Tenant isolation here is two independent layers, matching
-- docs/concepts/solobill.md: every query below names tenant_id in its own
-- WHERE clause (or, for CreateCustomer, as a column value), AND row level
-- security (see db/migrations) enforces the same scope below these
-- queries regardless of what they ask for. Neither layer is trusted alone:
-- a missing or wrong WHERE clause here is still caught by the database's
-- policy, and the policy itself is still a backstop, not the only check,
-- so a bug in it does not immediately leak another tenant's rows either.
-- The transaction these queries run in has already been scoped to one
-- tenant via set_config('app.tenant_id', ...) by postgres.Store.WithTenant;
-- current_setting(..., true)::uuid reads that same value back.

-- name: CreateCustomer :one
INSERT INTO customers (tenant_id, name, email, vat_id, street, postal_code, city, country_code)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, tenant_id, name, email, vat_id, street, postal_code, city, country_code, deleted_at, created_at, updated_at;

-- GetCustomerByID deliberately does not filter on deleted_at: a later
-- document still has to resolve a deleted customer by id (see
-- internal/storage.CustomerRepository.FindByID), so a point lookup by id
-- must keep answering for an already soft-deleted row.
--
-- name: GetCustomerByID :one
SELECT id, tenant_id, name, email, vat_id, street, postal_code, city, country_code, deleted_at, created_at, updated_at
FROM customers
WHERE id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- ListCustomers filters deleted_at IS NULL, unlike GetCustomerByID: this
-- is the human-facing picker view (a document's "choose a customer"
-- step), which must only ever offer active customers. Ordered by name,
-- unlike ListAccounts' created_at, for the same reason: a human scanning
-- a picker expects it alphabetical, not in creation order.
--
-- name: ListCustomers :many
SELECT id, tenant_id, name, email, vat_id, street, postal_code, city, country_code, deleted_at, created_at, updated_at
FROM customers
WHERE deleted_at IS NULL AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
ORDER BY name;

-- UpdateCustomer is guarded by "AND deleted_at IS NULL": a soft-deleted
-- customer cannot be edited back to life through an ordinary update: see
-- internal/storage.CustomerRepository.Update for why a zero-row
-- RETURNING here maps to storage.ErrNotFound, not storage.ErrConflict.
--
-- name: UpdateCustomer :one
UPDATE customers
SET name = $2,
    email = $3,
    vat_id = $4,
    street = $5,
    postal_code = $6,
    city = $7,
    country_code = $8,
    updated_at = now()
WHERE id = $1 AND deleted_at IS NULL AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
RETURNING id, tenant_id, name, email, vat_id, street, postal_code, city, country_code, deleted_at, created_at, updated_at;

-- DeleteCustomer is the soft delete itself: an UPDATE, never a DELETE,
-- guarded by "AND deleted_at IS NULL" so a second call against an
-- already-deleted row affects zero rows rather than re-stamping
-- deleted_at. :execrows rather than :one: the caller only needs the
-- affected-row count to tell "deleted" from "already deleted or not
-- found" apart, not a RETURNING row it would have to scan and discard.
--
-- name: DeleteCustomer :execrows
UPDATE customers
SET deleted_at = now()
WHERE id = $1 AND deleted_at IS NULL AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;
