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
	// Create inserts a payment. ErrConflict when the document is not an
	// issued invoice or correction, or when a reversal names a payment
	// of another document.
	Create(ctx context.Context, payment Payment) (Payment, error)

	// ListByDocument returns a document's payments ordered by
	// ReceivedOn, then CreatedAt.
	ListByDocument(ctx context.Context, documentID uuid.UUID) ([]Payment, error)

	// ListReceivedBetween returns every payment with ReceivedOn in
	// [from, to], both inclusive, for the EÜR report.
	ListReceivedBetween(ctx context.Context, from, to time.Time) ([]Payment, error)
}
