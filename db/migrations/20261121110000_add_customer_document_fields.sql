-- migrate:up

-- Issue #69 adds three columns to customers. The table, its row level
-- security policy (ENABLE and FORCE) and the soft delete column
-- deleted_at already exist, see 20261113150832_create_customers.sql, so
-- this migration creates no table and needs no new grant: the existing
-- SELECT, INSERT and UPDATE grant covers the new columns.

-- The language this customer's documents are rendered in by default.
-- German and English documents are both due by the end of the year.
-- NOT NULL DEFAULT 'de', so every existing customer reads back as German
-- with no backfill, and an insert that omits the column gets German too.
ALTER TABLE customers
    ADD COLUMN document_language text NOT NULL DEFAULT 'de',
    ADD CONSTRAINT customers_document_language_check CHECK (document_language IN ('de', 'en'));

-- The owner's own customer number, printed on documents. Nullable: a
-- customer without a number is valid. NULL means not set, an empty
-- string is not a value, the same rule every other nullable text column
-- of this table follows.
ALTER TABLE customers
    ADD COLUMN number text,
    ADD CONSTRAINT customers_number_not_empty CHECK (number IS NULL OR number <> '');

-- An optional second address line, with the same NULL-or-not-empty rule.
ALTER TABLE customers
    ADD COLUMN address_line2 text,
    ADD CONSTRAINT customers_address_line2_not_empty CHECK (address_line2 IS NULL OR address_line2 <> '');

-- A customer number identifies one customer, so it is unique per tenant
-- among active customers. Partial on deleted_at IS NULL for the same
-- reason catalogue_items' sku index is: a deleted customer's number has
-- to be reusable. Case-insensitive, because two numbers differing only
-- by case look identical to the human typing them in. A violation maps
-- to storage.ErrConflict and answers 409.
CREATE UNIQUE INDEX customers_tenant_id_number_lower_idx ON customers (tenant_id, lower(number)) WHERE number IS NOT NULL AND deleted_at IS NULL;

-- migrate:down

DROP INDEX customers_tenant_id_number_lower_idx;
ALTER TABLE customers DROP COLUMN address_line2;
ALTER TABLE customers DROP COLUMN number;
ALTER TABLE customers DROP COLUMN document_language;
