-- Same reasoning as customers.sql and catalogue_items.sql: tenant
-- isolation here is two independent layers, matching
-- docs/concepts/solobill.md. Every query below names tenant_id in its
-- own WHERE clause (or, for CreateDocument, as a column value) as the
-- first, defense-in-depth layer; row level security (see
-- db/migrations) is the independent second layer, enforcing the same
-- scope below these queries regardless of what they ask for.
--
-- Nothing here updates an issued document: documents_block_issued_mutation
-- (see db/migrations' documents migration) refuses that outright, at the
-- database level, so there is no query here guarded by "AND state =
-- 'draft'" the way customers.sql guards against a soft-deleted row --
-- the guard already exists one layer down and does not need repeating
-- here. UpdateDocument and IssueDocument both still only ever run
-- against a draft in practice, because nothing in this package ever
-- calls either on a row it does not already know is a draft, read back
-- moments earlier in the same WithTenant scope.
--
-- Every query that returns a document names all of its columns in table
-- order, so sqlc maps the row to sqlcgen.Document.

-- name: CreateDocument :one
INSERT INTO documents (
    tenant_id, type, customer_id, corrects_document_id, vat_regime,
    service_date, due_date, discount_percent_permyriad, discount_amount_cents,
    source_offer_id, language, valid_until, service_date_to, payment_terms, notes,
    customer_reference, correction_base_id, correction_base_seq
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
RETURNING id, tenant_id, type, state, customer_id, corrects_document_id, vat_regime,
    service_date, due_date, discount_percent_permyriad, discount_amount_cents,
    number, number_year, imported, frozen_payload, frozen_payload_sha256, issued_at,
    created_at, updated_at, original_number, source_offer_id, language, valid_until,
    service_date_to, payment_terms, notes, customer_reference, correction_seq,
    display_number, correction_base_id, correction_base_seq;

-- name: GetDocumentByID :one
SELECT id, tenant_id, type, state, customer_id, corrects_document_id, vat_regime,
    service_date, due_date, discount_percent_permyriad, discount_amount_cents,
    number, number_year, imported, frozen_payload, frozen_payload_sha256, issued_at,
    created_at, updated_at, original_number, source_offer_id, language, valid_until,
    service_date_to, payment_terms, notes, customer_reference, correction_seq,
    display_number, correction_base_id, correction_base_seq
FROM documents
WHERE id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- ListDocuments is ordered by created_at descending, unlike
-- ListCustomers and ListCatalogueItems' alphabetical-by-name order:
-- there is no name here to sort by, and a human-facing document list is
-- expected to show the most recently drafted or issued document first,
-- the same ordering ListAccounts already uses for the same reason. The
-- id breaks a tie, so two rows created in the same microsecond keep one
-- order.
--
-- name: ListDocuments :many
SELECT id, tenant_id, type, state, customer_id, corrects_document_id, vat_regime,
    service_date, due_date, discount_percent_permyriad, discount_amount_cents,
    number, number_year, imported, frozen_payload, frozen_payload_sha256, issued_at,
    created_at, updated_at, original_number, source_offer_id, language, valid_until,
    service_date_to, payment_terms, notes, customer_reference, correction_seq,
    display_number, correction_base_id, correction_base_seq
FROM documents
WHERE tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
ORDER BY created_at DESC, id DESC;

-- UpdateDocument replaces a draft's own header fields (not its lines;
-- see document_lines.sql for ReplaceDocumentLines). It never changes the
-- type, the chain (corrects_document_id, correction_base_id and
-- correction_base_seq) or source_offer_id: those are fixed when the draft
-- is created. It carries no explicit "AND state = 'draft'" guard the way
-- customers.sql's UpdateCustomer guards against a soft-deleted row:
-- documents_block_issued_mutation already refuses this UPDATE outright
-- once the row is issued, so adding the same guard here would only ever
-- change which layer reports the rejection, not whether it happens. A
-- :one query against a row that does not exist in this tenant at all
-- still answers pgx.ErrNoRows the same way; see
-- internal/storage/postgres/documents.go for how that is distinguished
-- from the trigger's own rejection.
--
-- name: UpdateDocument :one
UPDATE documents
SET customer_id = $2,
    vat_regime = $3,
    service_date = $4,
    due_date = $5,
    discount_percent_permyriad = $6,
    discount_amount_cents = $7,
    language = $8,
    valid_until = $9,
    service_date_to = $10,
    payment_terms = $11,
    notes = $12,
    customer_reference = $13,
    updated_at = now()
WHERE id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
RETURNING id, tenant_id, type, state, customer_id, corrects_document_id, vat_regime,
    service_date, due_date, discount_percent_permyriad, discount_amount_cents,
    number, number_year, imported, frozen_payload, frozen_payload_sha256, issued_at,
    created_at, updated_at, original_number, source_offer_id, language, valid_until,
    service_date_to, payment_terms, notes, customer_reference, correction_seq,
    display_number, correction_base_id, correction_base_seq;

-- DeleteDraftDocument is a real DELETE, never a soft delete: unlike
-- customers and catalogue_items, a draft document that is abandoned has
-- nothing yet that references it in a way soft delete exists to protect
-- (see customers.sql's own reasoning for that contrast); an issued
-- document can never reach here at all, since
-- documents_block_issued_mutation refuses the DELETE itself before this
-- query's own WHERE clause gets a chance to matter. document_lines'
-- own ON DELETE CASCADE foreign key (see db/migrations) takes its lines
-- with it.
--
-- name: DeleteDraftDocument :execrows
DELETE FROM documents
WHERE id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- LockDocument reads one document and takes a FOR NO KEY UPDATE lock on
-- it for the rest of the transaction. Issuing runs it twice, always
-- before document_numbers' counter is touched:
--
--   - On the draft being issued, so nothing else can move it out from
--     under the allocation that follows (a second, concurrent issue of
--     the same draft waits here and then finds it issued). The caller
--     checks the state and the type of the row it reads back, so a
--     document that cannot be issued is refused before a number is ever
--     allocated: storage.ErrConflict on that path never depends on a
--     caller propagating an error to roll an allocation back.
--   - On the root of a correction's chain, the original invoice, so two
--     corrections of one chain are issued one after the other and each
--     sees the other's correction_seq. FOR NO KEY UPDATE conflicts with
--     itself but not with the KEY SHARE lock a foreign key check takes,
--     so creating a correction draft or a payment that references the
--     root is never blocked by it. Locking an issued row is no UPDATE:
--     documents_block_issued_mutation does not fire.
--
-- name: LockDocument :one
SELECT id, tenant_id, type, state, customer_id, corrects_document_id, vat_regime,
    service_date, due_date, discount_percent_permyriad, discount_amount_cents,
    number, number_year, imported, frozen_payload, frozen_payload_sha256, issued_at,
    created_at, updated_at, original_number, source_offer_id, language, valid_until,
    service_date_to, payment_terms, notes, customer_reference, correction_seq,
    display_number, correction_base_id, correction_base_seq
FROM documents
WHERE id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
FOR NO KEY UPDATE;

-- ListChainDocuments returns the issued documents of the chain rooted at
-- $1: the original invoice and every issued correction naming it. Under
-- READ COMMITTED each statement reads the latest committed rows, so run
-- after LockDocument on the root it sees every correction a concurrent
-- issue committed before this transaction got the lock.
--
-- name: ListChainDocuments :many
SELECT id, tenant_id, type, state, customer_id, corrects_document_id, vat_regime,
    service_date, due_date, discount_percent_permyriad, discount_amount_cents,
    number, number_year, imported, frozen_payload, frozen_payload_sha256, issued_at,
    created_at, updated_at, original_number, source_offer_id, language, valid_until,
    service_date_to, payment_terms, notes, customer_reference, correction_seq,
    display_number, correction_base_id, correction_base_seq
FROM documents
WHERE tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    AND state = 'issued'
    AND (id = $1 OR corrects_document_id = $1)
ORDER BY correction_seq NULLS FIRST;

-- LockSellerProfileForIssue reads the seller profile issuing freezes and
-- holds a FOR SHARE lock on it until the issuing transaction ends. A
-- concurrent PUT /api/v1/profile (an upsert, which needs the row
-- exclusively) waits until the issue commits or rolls back, so the
-- snapshot validated and frozen is one consistent row, and a profile
-- save never commits between the read and the issue. Concurrent issues
-- share the lock and never wait for each other here. The unique index on
-- documents.display_number stays the last guard behind the template
-- rules. Issue #68 owns the profile's own queries; this read lives here
-- because only issuing needs the lock.
--
-- name: LockSellerProfileForIssue :one
SELECT tenant_id, legal_name, street, address_line2, postal_code, city, country_code,
       tax_number, vat_id, kleinunternehmer_id, default_vat_regime, default_language,
       bank_name, iban, bic, email, phone, website, footer_text, number_formats,
       created_at, updated_at
FROM seller_profiles
WHERE tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
FOR SHARE;

-- IssueDocument is the one statement that ever moves a document from
-- draft to issued: it writes the number, number_year, issued_at, the
-- frozen payload and its hash, the display number and, for a correction,
-- its correction_seq in the same UPDATE, matching
-- documents_issued_fields_consistent,
-- documents_display_number_consistent and
-- documents_correction_seq_consistent. "AND state = 'draft'" here is not
-- redundant with documents_block_issued_mutation the way UpdateDocument's
-- guard would be: that trigger only ever rejects an UPDATE on a row that
-- is ALREADY issued; it has nothing to say about a row that is still
-- draft, which is exactly the row this statement targets. LockDocument
-- has already locked the row and the caller checked its state and type,
-- so the guard stays as defence in depth against a caller that runs this
-- statement without that order.
--
-- "AND type = $9" guards against a second, different mistake: the
-- caller names the type it allocated the number under, separately from
-- id. Without this guard, a docType that does not match this row's own
-- type would still succeed, silently issuing the document under a
-- counter that belongs to a different type; this makes that mismatch
-- fail instead, the same zero-row, storage.ErrConflict outcome as
-- issuing a non-draft.
--
-- name: IssueDocument :one
UPDATE documents
SET state = 'issued',
    number = $2,
    number_year = $3,
    issued_at = $4,
    frozen_payload = $5,
    frozen_payload_sha256 = $6,
    display_number = $7,
    correction_seq = $8,
    updated_at = now()
WHERE id = $1
    AND state = 'draft'
    AND type = $9
    AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
RETURNING id, tenant_id, type, state, customer_id, corrects_document_id, vat_regime,
    service_date, due_date, discount_percent_permyriad, discount_amount_cents,
    number, number_year, imported, frozen_payload, frozen_payload_sha256, issued_at,
    created_at, updated_at, original_number, source_offer_id, language, valid_until,
    service_date_to, payment_terms, notes, customer_reference, correction_seq,
    display_number, correction_base_id, correction_base_seq;
