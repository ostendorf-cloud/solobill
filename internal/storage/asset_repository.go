package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AssetRepository persists the asset register. Every method must run
// inside a context TenantScope.WithTenant produced. Implemented by issue
// #78 (I11a).
type AssetRepository interface {
	Create(ctx context.Context, asset Asset) (Asset, error)
	FindByID(ctx context.Context, id uuid.UUID) (Asset, error)

	// List returns every asset, disposed ones included, ordered by
	// AcquiredOn.
	List(ctx context.Context) ([]Asset, error)

	// Update replaces an asset. ErrConflict once a tax year containing
	// its depreciation has been reported as final (issue #80 decides
	// when that is).
	Update(ctx context.Context, asset Asset) (Asset, error)

	// RecordDisposal sets DisposedOn and DisposalProceedsCents once.
	// ErrConflict when the asset is already disposed.
	RecordDisposal(ctx context.Context, id uuid.UUID, disposedOn time.Time, proceedsCents *int64) (Asset, error)
}
