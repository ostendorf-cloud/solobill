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

-- name: CreateDocument :one
INSERT INTO documents (
    tenant_id, type, customer_id, corrects_document_id, vat_regime,
    service_date, due_date, discount_percent_permyriad, discount_amount_cents
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, tenant_id, type, state, customer_id, corrects_document_id, vat_regime,
    service_date, due_date, discount_percent_permyriad, discount_amount_cents,
    number, number_year, imported, frozen_payload, frozen_payload_sha256, issued_at,
    created_at, updated_at;

-- name: GetDocumentByID :one
SELECT id, tenant_id, type, state, customer_id, corrects_document_id, vat_regime,
    service_date, due_date, discount_percent_permyriad, discount_amount_cents,
    number, number_year, imported, frozen_payload, frozen_payload_sha256, issued_at,
    created_at, updated_at
FROM documents
WHERE id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- ListDocuments is ordered by created_at descending, unlike
-- ListCustomers and ListCatalogueItems' alphabetical-by-name order:
-- there is no name here to sort by, and a human-facing document list is
-- expected to show the most recently drafted or issued document first,
-- the same ordering ListAccounts already uses for the same reason.
--
-- name: ListDocuments :many
SELECT id, tenant_id, type, state, customer_id, corrects_document_id, vat_regime,
    service_date, due_date, discount_percent_permyriad, discount_amount_cents,
    number, number_year, imported, frozen_payload, frozen_payload_sha256, issued_at,
    created_at, updated_at
FROM documents
WHERE tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
ORDER BY created_at DESC;

-- UpdateDocument replaces a draft's own header fields (not its lines;
-- see document_lines.sql for ReplaceDocumentLines). It carries no
-- explicit "AND state = 'draft'" guard the way customers.sql's
-- UpdateCustomer guards against a soft-deleted row: documents_block_issued_mutation
-- already refuses this UPDATE outright once the row is issued, so adding
-- the same guard here would only ever change which layer reports the
-- rejection, not whether it happens. A :one query against a row that
-- does not exist in this tenant at all still answers pgx.ErrNoRows the
-- same way; see internal/storage/postgres/documents.go for how that is
-- distinguished from the trigger's own rejection.
--
-- name: UpdateDocument :one
UPDATE documents
SET customer_id = $2,
    vat_regime = $3,
    service_date = $4,
    due_date = $5,
    discount_percent_permyriad = $6,
    discount_amount_cents = $7,
    updated_at = now()
WHERE id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
RETURNING id, tenant_id, type, state, customer_id, corrects_document_id, vat_regime,
    service_date, due_date, discount_percent_permyriad, discount_amount_cents,
    number, number_year, imported, frozen_payload, frozen_payload_sha256, issued_at,
    created_at, updated_at;

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

-- LockDraftDocumentForIssue is documentRepository.Issue's first
-- statement, run before document_numbers' own counter is ever touched:
-- it verifies id resolves to a draft of type $2 in this tenant, and
-- takes a FOR NO KEY UPDATE lock on that row for the rest of the
-- transaction, so nothing else can move it out from under the
-- allocation that follows. Without this, Issue allocated a number first
-- and only found out it could not actually issue the document second
-- (IssueDocument's own "AND state = 'draft'" below answering zero rows):
-- storage.ErrConflict on that path then depended entirely on the
-- caller propagating the error and letting TenantScope.WithTenant roll
-- the transaction back, since WithTenant commits whenever the closure
-- returns nil; a caller that mapped ErrConflict to a response and
-- returned nil from that closure would commit the already-incremented
-- counter and permanently skip the number nobody ever issued. Locking
-- and verifying before allocating makes the ordering itself carry that
-- guarantee instead: Issue now returns storage.ErrConflict from this
-- statement's own zero rows before allocateNextDocumentNumber ever
-- runs, so there is nothing for a careless caller to leave half
-- committed.
--
-- A :one query with zero rows answers pgx.ErrNoRows;
-- internal/storage/postgres/documents.go maps that to
-- storage.ErrConflict here, not storage.ErrNotFound, since the row
-- usually does exist, just not in the state (or type) this call needed
-- it in; see IssueDocument's own comment below for why type is checked
-- here too, not only state.
--
-- name: LockDraftDocumentForIssue :one
SELECT 1 FROM documents
WHERE id = $1
    AND type = $2
    AND state = 'draft'
    AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
FOR NO KEY UPDATE;

-- IssueDocument is the one statement that ever moves a document from
-- draft to issued: it writes the allocated number together with
-- number_year, issued_at, the frozen payload and its hash in the same
-- UPDATE, matching documents_issued_fields_consistent's requirement that
-- all five land together. "AND state = 'draft'" here is not redundant
-- with documents_block_issued_mutation the way UpdateDocument's guard
-- would be: that trigger only ever rejects an UPDATE on a row that is
-- ALREADY issued; it has nothing to say about a row that is still draft,
-- which is exactly the row this statement targets. By the time this
-- statement runs, LockDraftDocumentForIssue above has already verified
-- and locked exactly this (id, type, state) combination for the rest of
-- the transaction, so this guard no longer has anything left to actually
-- catch in practice; it stays as defense in depth; for example against a
-- future caller that runs this statement directly, bypassing
-- documentRepository.Issue's own ordering. The guard is what originally
-- stopped two concurrent calls to Issue racing on the very same document
-- (as opposed to the very same counter, which document_numbers' own
-- locking already serialises): without it, a second IssueDocument on an
-- already-issued row would silently succeed a second time, each
-- allocating its own number, both written to the same document_id,
-- rather than failing; LockDraftDocumentForIssue's own FOR NO KEY UPDATE
-- now serialises that race one statement earlier instead.
--
-- "AND type = $7" guards against a second, different mistake: the
-- caller (documentRepository.Issue) already has to name docType itself,
-- separately from id, because document_numbers' counter has to be
-- allocated, under that exact type, before this statement can even run
-- (see internal/storage/postgres/numbering.go). Without this guard,
-- passing a docType that does not match this row's own, already-stored
-- type would still succeed, silently issuing the document under a
-- counter that belongs to a different type than the row itself claims
-- to be; this makes that mismatch fail instead, the same zero-row,
-- storage.ErrConflict outcome as issuing a non-draft.
--
-- name: IssueDocument :one
UPDATE documents
SET state = 'issued',
    number = $2,
    number_year = $3,
    issued_at = $4,
    frozen_payload = $5,
    frozen_payload_sha256 = $6,
    updated_at = now()
WHERE id = $1
    AND state = 'draft'
    AND type = $7
    AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
RETURNING id, tenant_id, type, state, customer_id, corrects_document_id, vat_regime,
    service_date, due_date, discount_percent_permyriad, discount_amount_cents,
    number, number_year, imported, frozen_payload, frozen_payload_sha256, issued_at,
    created_at, updated_at;
