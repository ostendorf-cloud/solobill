package storage

import (
	"context"
	"time"
)

// SellerProfileRepository persists the one seller profile of a tenant.
// Every method must run inside a context TenantScope.WithTenant
// produced; see ErrNoTenantScope. Implemented by issue #68 (I2).
type SellerProfileRepository interface {
	// Get returns the tenant's profile, or ErrNotFound when none was
	// saved yet.
	Get(ctx context.Context) (SellerProfile, error)

	// Put creates or replaces the tenant's profile as a whole.
	Put(ctx context.Context, profile SellerProfile) (SellerProfile, error)
}

// VATRegimePeriodRepository persists the VAT regime history of a tenant.
// Every method must run inside a context TenantScope.WithTenant
// produced. Implemented by issue #68 (I2).
type VATRegimePeriodRepository interface {
	// List returns every period ordered by ValidFrom ascending.
	List(ctx context.Context) ([]VATRegimePeriod, error)

	// Create adds a period. ErrConflict when a period with the same
	// ValidFrom exists.
	Create(ctx context.Context, period VATRegimePeriod) (VATRegimePeriod, error)

	// RegimeOn returns the period that applies on date, or ErrNotFound
	// when date lies before the first period.
	RegimeOn(ctx context.Context, date time.Time) (VATRegimePeriod, error)
}
