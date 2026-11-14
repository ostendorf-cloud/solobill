-- Same reasoning as documents.sql: tenant isolation here is two
-- independent layers, and document_lines_block_issued_document_mutation
-- (see db/migrations' document_lines migration) already refuses any
-- INSERT, UPDATE or DELETE against a line whose document is issued, at
-- the database level, so no query here repeats that guard itself.

-- name: CreateDocumentLine :one
INSERT INTO document_lines (
    tenant_id, document_id, position, catalogue_item_id, description,
    quantity_thousandths, unit_price_cents, unit, vat_rate_permyriad,
    line_discount_percent_permyriad, line_discount_amount_cents
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id, tenant_id, document_id, position, catalogue_item_id, description,
    quantity_thousandths, unit_price_cents, unit, vat_rate_permyriad,
    line_discount_percent_permyriad, line_discount_amount_cents, created_at, updated_at;

-- ListDocumentLines is ordered by position: the same column
-- document_lines_tenant_id_document_id_position_unique enforces
-- uniqueness over, and the same order internal/money's largest-remainder
-- allocation needs for its own tie-break rule (see
-- docs/concepts/solobill.md's Rounding section). Callers must never
-- re-sort this result by anything else.
--
-- name: ListDocumentLines :many
SELECT id, tenant_id, document_id, position, catalogue_item_id, description,
    quantity_thousandths, unit_price_cents, unit, vat_rate_permyriad,
    line_discount_percent_permyriad, line_discount_amount_cents, created_at, updated_at
FROM document_lines
WHERE document_id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
ORDER BY position;

-- DeleteDocumentLines removes every line of one document: the storage
-- layer's Update replaces a draft's lines wholesale (delete all, then
-- CreateDocumentLine for each of the caller's current lines), the same
-- full-column-replace contract CatalogueItemRepository.Update already
-- uses for a single row, applied here to a whole set of rows instead.
-- :execrows, not :one or :many, since the caller only needs to know
-- whether anything was actually removed, not read any of it back.
--
-- name: DeleteDocumentLines :execrows
DELETE FROM document_lines
WHERE document_id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;
