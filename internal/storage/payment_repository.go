package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// PaymentRepository persists payments. Every method must run inside a
// context TenantScope.WithTenant produced. There is no Update and no
// Delete: the table is append-only. Implemented by issue #71 (I5).
type PaymentRepository interface {
	// Create inserts a payment. ErrConflict when DocumentID is not an
	// issued invoice that is the root of its chain, or when a reversal
	// names a payment of another chain or one already reversed.
	Create(ctx context.Context, payment Payment) (Payment, error)

	// ListByChain returns the payments of the chain rooted at rootID,
	// ordered by ReceivedOn, then CreatedAt.
	ListByChain(ctx context.Context, rootID uuid.UUID) ([]Payment, error)

	// ListReceivedBetween returns every payment with ReceivedOn in
	// [from, to], both inclusive, for the EÜR report.
	ListReceivedBetween(ctx context.Context, from, to time.Time) ([]Payment, error)
}
