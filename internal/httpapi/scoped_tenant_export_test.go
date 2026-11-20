package httpapi

import (
	"context"

	"github.com/google/uuid"
)

// ScopedTenant is the tenant inTenant opened the scope for, read back by
// the test fakes of the customers and catalogue repositories.
func ScopedTenant(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(scopedTenantKey{}).(uuid.UUID)

	return id, ok
}
