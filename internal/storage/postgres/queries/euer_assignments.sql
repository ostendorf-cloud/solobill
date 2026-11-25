-- Tenant isolation as in seller_profiles.sql. The table is append-only:
-- there is no UPDATE and no DELETE query, and the application role has no
-- such grant.

-- LockEuerAssignments serialises the writers of one tenant for the rest of
-- the transaction, so the effective row CreateEuerAssignment's caller read
-- is still the effective row when it inserts.
--
-- name: LockEuerAssignments :exec
SELECT pg_advisory_xact_lock(hashtextextended('euer_assignments:' || NULLIF(current_setting('app.tenant_id', true), ''), 0));

-- EffectiveEuerAssignmentOfEntry is the row of the entry no other row
-- supersedes.
--
-- name: EffectiveEuerAssignmentOfEntry :one
SELECT a.id, a.tenant_id, a.entry_kind, a.entry_id, a.tax_year, a.note, a.supersedes_id, a.created_by, a.created_at
FROM euer_assignments a
WHERE a.entry_kind = $1 AND a.entry_id = $2
  AND a.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
  AND NOT EXISTS (SELECT 1 FROM euer_assignments s WHERE s.supersedes_id = a.id AND s.tenant_id = a.tenant_id)
ORDER BY a.created_at DESC, a.id DESC
LIMIT 1;

-- name: CreateEuerAssignment :one
INSERT INTO euer_assignments (tenant_id, entry_kind, entry_id, tax_year, note, supersedes_id, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, tenant_id, entry_kind, entry_id, tax_year, note, supersedes_id, created_by, created_at;

-- name: ListEffectiveEuerAssignments :many
SELECT a.id, a.tenant_id, a.entry_kind, a.entry_id, a.tax_year, a.note, a.supersedes_id, a.created_by, a.created_at
FROM euer_assignments a
WHERE a.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
  AND NOT EXISTS (SELECT 1 FROM euer_assignments s WHERE s.supersedes_id = a.id AND s.tenant_id = a.tenant_id)
ORDER BY a.created_at, a.id;
