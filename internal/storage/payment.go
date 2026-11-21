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

// Payment is money received for, or refunded on, an invoice chain. The
// table is append-only: a trigger blocks UPDATE and DELETE. Issue #71
// (I5) creates the table.
//
// A payment belongs to the chain, not to one document of it:
// DocumentID is always the original invoice, the root every correction
// references. The open amount is the effective document's grand total
// minus the sum of the chain's payments.
//
// Two kinds of negative rows exist. A reversal undoes a recording
// mistake: it names the reversed row in ReversesPaymentID and carries
// its negated amount. A refund is money paid back to the customer, for
// example after a correction lowered the total: a negative amount
// without ReversesPaymentID, dated the day the money left.
//
// ReceivedOn is the Zufluss or Abfluss date section 11 EStG counts by,
// so it is the figure the EÜR report (issue #80) uses, never the
// invoice date. The report uses payments only, never document totals.
type Payment struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	// DocumentID is the chain root: the original invoice.
	DocumentID uuid.UUID
	ReceivedOn time.Time
	// AmountCents is gross. Negative for a refund or a reversal.
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
