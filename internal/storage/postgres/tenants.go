package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres/sqlcgen"
)

// tenantRepository satisfies storage.TenantRepository. It runs directly
// against the pool, never against a WithTenant-scoped transaction:
// tenants carries no row level security to scope by, see db/migrations.
type tenantRepository struct {
	pool *pgxpool.Pool
}

// Create implements storage.TenantRepository.
func (r *tenantRepository) Create(ctx context.Context, tenant storage.Tenant) (storage.Tenant, error) {
	row, err := sqlcgen.New(r.pool).CreateTenant(ctx, sqlcgen.CreateTenantParams{
		Slug: tenant.Slug,
		Name: tenant.Name,
	})
	if err != nil {
		return storage.Tenant{}, mapWriteError(err)
	}

	return tenantFromRow(row), nil
}

// FindByID implements storage.TenantRepository.
func (r *tenantRepository) FindByID(ctx context.Context, id uuid.UUID) (storage.Tenant, error) {
	row, err := sqlcgen.New(r.pool).GetTenantByID(ctx, id)
	if err != nil {
		return storage.Tenant{}, mapReadError(err)
	}

	return tenantFromRow(row), nil
}

// FindBySlug implements storage.TenantRepository.
func (r *tenantRepository) FindBySlug(ctx context.Context, slug string) (storage.Tenant, error) {
	row, err := sqlcgen.New(r.pool).GetTenantBySlug(ctx, slug)
	if err != nil {
		return storage.Tenant{}, mapReadError(err)
	}

	return tenantFromRow(row), nil
}

func tenantFromRow(row sqlcgen.Tenant) storage.Tenant {
	return storage.Tenant{
		ID:        row.ID,
		Slug:      row.Slug,
		Name:      row.Name,
		CreatedAt: timeFromTimestamptz(row.CreatedAt),
		UpdatedAt: timeFromTimestamptz(row.UpdatedAt),
	}
}
