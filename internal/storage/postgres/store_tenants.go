package postgres

import "github.com/ostendorf-cloud/solobill/internal/storage"

// Tenants returns the tenant repository. Its methods are not
// tenant-scoped; see storage.TenantRepository.
func (s *Store) Tenants() storage.TenantRepository {
	return &tenantRepository{pool: s.pool}
}
