package storage

import (
	"context"

	"github.com/google/uuid"
)

// FileRepository persists file metadata. Every method must run inside a
// context TenantScope.WithTenant produced. There is no Delete: a stored
// file is kept for the retention period. Implemented by issue #75 (I9).
type FileRepository interface {
	Create(ctx context.Context, file File) (File, error)
	FindByID(ctx context.Context, id uuid.UUID) (File, error)

	// FindBySHA256 returns every file of the tenant with this hash, for
	// the duplicate warning. An empty slice is not an error.
	FindBySHA256(ctx context.Context, sha256 string) ([]File, error)
}
