-- files rows are written once: there is no UPDATE and no DELETE here, and
-- the application role holds no such privilege (see
-- docker/postgres/grants/20261121150000_files.sql). Every query names the
-- tenant in its WHERE clause or as a column value, and row level security
-- enforces the same scope below it; see customers.sql for why both layers.

-- name: CreateFile :one
INSERT INTO files (
    id, tenant_id, purpose, object_key, object_version_id, sha256, size_bytes,
    content_type, original_filename, uploaded_by, uploaded_at, retain_until
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING id, tenant_id, purpose, object_key, object_version_id, sha256, size_bytes,
    content_type, original_filename, uploaded_by, uploaded_at, retain_until;

-- name: GetFileByID :one
SELECT id, tenant_id, purpose, object_key, object_version_id, sha256, size_bytes,
    content_type, original_filename, uploaded_by, uploaded_at, retain_until
FROM files
WHERE id = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- name: ListFilesBySHA256 :many
SELECT id, tenant_id, purpose, object_key, object_version_id, sha256, size_bytes,
    content_type, original_filename, uploaded_by, uploaded_at, retain_until
FROM files
WHERE sha256 = $1 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
ORDER BY uploaded_at, id;

-- ListFiles backs "solobill files verify", which walks every file of one
-- tenant at a time.
--
-- name: ListFiles :many
SELECT id, tenant_id, purpose, object_key, object_version_id, sha256, size_bytes,
    content_type, original_filename, uploaded_by, uploaded_at, retain_until
FROM files
WHERE tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
ORDER BY uploaded_at, id;

-- ListTenantIDs is the tenant list "solobill files verify" scopes each
-- pass to. tenants carries no row level security (see its migration), so
-- this reads every tenant id and nothing else.
--
-- name: ListTenantIDs :many
SELECT id FROM tenants ORDER BY id;
