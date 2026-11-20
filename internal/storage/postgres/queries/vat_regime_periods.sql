-- Tenant isolation as in seller_profiles.sql. The table is append-only:
-- there is no UPDATE and no DELETE query, and the application role has no
-- such grant.

-- LockVATRegimePeriods serialises the writers of one tenant for the rest
-- of the transaction, so the "same valid_from" check in CreateVATRegimePeriod
-- cannot race another insert.
--
-- name: LockVATRegimePeriods :exec
SELECT pg_advisory_xact_lock(hashtextextended('vat_regime_periods:' || NULLIF(current_setting('app.tenant_id', true), ''), 0));

-- name: GetVATRegimePeriod :one
SELECT id, tenant_id, valid_from, regime, note, supersedes_id, created_at
FROM vat_regime_periods
WHERE id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- name: ListVATRegimePeriods :many
SELECT id, tenant_id, valid_from, regime, note, supersedes_id, created_at
FROM vat_regime_periods
WHERE tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
ORDER BY valid_from, created_at, id;

-- IsVATRegimePeriodSuperseded is true when any row names $1 in supersedes_id.
--
-- name: IsVATRegimePeriodSuperseded :one
SELECT EXISTS (
    SELECT 1 FROM vat_regime_periods
    WHERE supersedes_id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
);

-- CountEffectiveVATRegimePeriodsFrom counts effective (not superseded) period with
-- the given valid_from, other than the row the new period supersedes.
--
-- name: CountEffectiveVATRegimePeriodsFrom :one
SELECT count(*)
FROM vat_regime_periods p
WHERE p.valid_from = $1
  AND p.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
  AND NOT EXISTS (SELECT 1 FROM vat_regime_periods s WHERE s.supersedes_id = p.id AND s.tenant_id = p.tenant_id)
  AND p.id IS DISTINCT FROM sqlc.narg('superseded_id')::uuid;

-- name: CreateVATRegimePeriod :one
INSERT INTO vat_regime_periods (tenant_id, valid_from, regime, note, supersedes_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, tenant_id, valid_from, regime, note, supersedes_id, created_at;

-- RegimeOnVATDate is the latest effective period starting on or before $1.
--
-- name: RegimeOnVATDate :one
SELECT p.id, p.tenant_id, p.valid_from, p.regime, p.note, p.supersedes_id, p.created_at
FROM vat_regime_periods p
WHERE p.valid_from <= $1
  AND p.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
  AND NOT EXISTS (SELECT 1 FROM vat_regime_periods s WHERE s.supersedes_id = p.id AND s.tenant_id = p.tenant_id)
ORDER BY p.valid_from DESC, p.created_at DESC, p.id DESC
LIMIT 1;
