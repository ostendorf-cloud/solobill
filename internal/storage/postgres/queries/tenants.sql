-- Tenants carry no row level security: a tenant is the scope, not
-- something scoped. These queries run against the pool directly, never
-- inside a WithTenant transaction.

-- name: CreateTenant :one
INSERT INTO tenants (slug, name)
VALUES ($1, $2)
RETURNING id, slug, name, created_at, updated_at;

-- name: GetTenantByID :one
SELECT id, slug, name, created_at, updated_at
FROM tenants
WHERE id = $1;

-- name: GetTenantBySlug :one
SELECT id, slug, name, created_at, updated_at
FROM tenants
WHERE slug = $1;
