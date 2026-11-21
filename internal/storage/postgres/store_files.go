package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres/sqlcgen"
)

// Files returns the file metadata repository. Every method on it requires
// a context WithTenant produced; see storage.FileRepository.
func (s *Store) Files() storage.FileRepository {
	return &fileRepository{}
}

// TenantIDs returns every tenant id, for "solobill files verify", which
// scopes one pass per tenant. tenants carries no row level security, so
// this needs no tenant scope.
func (s *Store) TenantIDs(ctx context.Context) ([]uuid.UUID, error) {
	ids, err := sqlcgen.New(s.pool).ListTenantIDs(ctx)
	if err != nil {
		return nil, mapReadError(err)
	}

	return ids, nil
}

// ListFiles returns every file of the tenant ctx is scoped to, for "solobill
// files verify". It requires a context WithTenant produced.
func (s *Store) ListFiles(ctx context.Context) ([]storage.File, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := sqlcgen.New(tx).ListFiles(ctx)
	if err != nil {
		return nil, mapReadError(err)
	}

	return filesFromRows(rows), nil
}
