-- document_numbers' own tenant isolation follows the same two-layer
-- pattern as every other query file: tenant_id named explicitly here,
-- row level security enforcing the same scope underneath regardless.
--
-- The two queries below are always run together, in this order, inside
-- the single transaction that is issuing a document (see
-- internal/storage/postgres/numbering.go):
--
--   1. EnsureDocumentNumberRow, so a (tenant, type, number_year) counter
--      that has never been used yet has a row to allocate from at all.
--   2. IncrementDocumentNumber, the actual allocation and the lock
--      issue #15's own gaplessness guarantee rests on: last_number + 1,
--      written and returned in the same UPDATE ... RETURNING. An
--      UPDATE's WHERE clause takes a row lock as it matches a row, the
--      same FOR NO KEY UPDATE lock a separate SELECT ... FOR UPDATE
--      would have taken, and if a second, concurrent UPDATE reaches the
--      same row first, this one blocks until that transaction commits
--      or rolls back. On commit, Postgres re-evaluates this UPDATE's
--      SET expression against the row's now-current, just-committed
--      value before applying it (the EvalPlanQual mechanism READ
--      COMMITTED uses for exactly this), so last_number + 1 is computed
--      from the number actually last allocated, never a stale one. A
--      prior revision of this file ran a separate SELECT ... FOR UPDATE
--      before this UPDATE, under the belief that the UPDATE alone was
--      not already a lock-and-serialise point; it was removed because
--      it added a network round trip for a guarantee the UPDATE already
--      gives on its own. That guarantee depends on this statement
--      staying a single read-and-write UPDATE: a rewrite that reads
--      last_number in one statement and writes last_number + 1 in a
--      separate one would reintroduce exactly the race this comment
--      describes, since nothing would then hold the row locked between
--      the read and the write.
--
-- A transaction that allocates a number this way and then rolls back
-- (the issuing transaction failed for any other reason, or the test
-- that exercises this directly) leaves last_number exactly where step 1
-- found it: the UPDATE in step 2 never committed, so nothing else ever
-- observes the number it would have produced, and the next successful
-- call still gets that same, un-skipped number.

-- EnsureDocumentNumberRow deliberately reads tenant_id from
-- current_setting rather than taking it as a bound parameter, unlike
-- CreateCustomer and CreateCatalogueItem's own tenant_id column value:
-- there is no caller-supplied Go value for it to come from in the first
-- place (internal/storage.DocumentNumberRepository.AllocateNext takes no
-- tenant id; it is implicit in the TenantScope.WithTenant scope the
-- whole call already runs inside), and reading it the same way every
-- other query here reads it back for its WHERE clause keeps this
-- statement from being the one place in this file a caller could get
-- the tenant wrong.
--
-- name: EnsureDocumentNumberRow :exec
INSERT INTO document_numbers (tenant_id, type, number_year, last_number)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, $1, $2, 0)
ON CONFLICT (tenant_id, type, number_year) DO NOTHING;

-- name: IncrementDocumentNumber :one
UPDATE document_numbers
SET last_number = last_number + 1,
    updated_at = now()
WHERE tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    AND type = $1
    AND number_year = $2
RETURNING last_number;

-- GetDocumentNumber is a plain, unlocked read of the current counter:
-- used by tests asserting what the counter holds, and never by the
-- allocation path itself, which needs the lock IncrementDocumentNumber's
-- own UPDATE takes. A missing row (nothing ever issued yet under this
-- key) answers pgx.ErrNoRows, which the test helper reading this treats
-- the same as last_number = 0.
--
-- name: GetDocumentNumber :one
SELECT last_number
FROM document_numbers
WHERE tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    AND type = $1
    AND number_year = $2;
