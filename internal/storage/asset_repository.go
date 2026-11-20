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

	// Update replaces an asset while its first schedule year is still
	// open: the calendar year of AcquiredOn (or OpeningBookValueYear for
	// an imported asset) has not ended at now. ErrConflict afterwards,
	// because a depreciation figure of a past year may already be in a
	// filed EÜR. A later change is a disposal, never an edit.
	Update(ctx context.Context, asset Asset, now time.Time) (Asset, error)

	// RecordDisposal sets DisposedOn and DisposalProceedsCents once.
	// ErrConflict when the asset is already disposed.
	RecordDisposal(ctx context.Context, id uuid.UUID, disposedOn time.Time, proceedsCents *int64) (Asset, error)
}
