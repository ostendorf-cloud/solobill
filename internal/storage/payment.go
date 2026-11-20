package storage

import (
	"time"

	"github.com/google/uuid"
)

// PaymentMethod is how a payment arrived.
type PaymentMethod string

const (
	// PaymentMethodBankTransfer is a bank transfer.
	PaymentMethodBankTransfer PaymentMethod = "bank_transfer"
	// PaymentMethodCash is cash.
	PaymentMethodCash PaymentMethod = "cash"
	// PaymentMethodCard is a card payment.
	PaymentMethodCard PaymentMethod = "card"
	// PaymentMethodPayPal is a PayPal payment.
	PaymentMethodPayPal PaymentMethod = "paypal"
	// PaymentMethodOther is anything else, explained in Note.
	PaymentMethodOther PaymentMethod = "other"
)

// Payment is money received against an issued invoice or correction.
// The table is append-only: a trigger blocks UPDATE and DELETE. A
// mistake is undone by a new row with a negative AmountCents that names
// the row it reverses. Issue #71 (I5) creates the table.
//
// ReceivedOn is the Zufluss date section 11 Abs. 1 EStG counts by, so it
// is the figure the EÜR report (issue #80) uses, never the invoice date.
type Payment struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	DocumentID uuid.UUID
	ReceivedOn time.Time
	// AmountCents is gross. Negative only for a reversal.
	AmountCents int64
	Method      PaymentMethod
	Note        *string
	// ReversesPaymentID is set, and only set, on a reversal row.
	ReversesPaymentID *uuid.UUID
	// Imported marks a payment brought in by the historic import
	// (issue #81) for an imported 2026 invoice.
	Imported  bool
	CreatedAt time.Time
}
