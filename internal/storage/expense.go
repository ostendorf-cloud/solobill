package storage

import (
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/category"
)

// BookingState is the lifecycle of an expense or an other income entry.
type BookingState string

const (
	// BookingStateDraft is editable and deletable.
	BookingStateDraft BookingState = "draft"
	// BookingStateBooked is immutable: a trigger refuses UPDATE and
	// DELETE. A mistake is fixed by a storno row plus a new row.
	BookingStateBooked BookingState = "booked"
)

// Expense is one Betriebsausgabe with its receipt. Issue #76 (I10a)
// creates the table.
//
// Amounts are integer cents. Under the Kleinunternehmer regime the VAT
// stays inside GrossCents: NetCents equals GrossCents and VATCents is 0,
// because there is no Vorsteuer deduction.
type Expense struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	State    BookingState
	// PaidOn is the Abfluss date section 11 Abs. 2 EStG counts by. A
	// draft may leave it nil; booking requires it.
	PaidOn *time.Time
	// ReceiptDate is the date printed on the receipt.
	ReceiptDate      *time.Time
	Supplier         string
	Description      string
	GrossCents       int64
	NetCents         int64
	VATRatePermyriad int32
	VATCents         int64
	// Category is a category.KindExpense key.
	Category category.Key
	// FileID is the receipt in files, nil until uploaded. Booking
	// without a receipt is allowed and flagged: see MissingReceipt.
	FileID *uuid.UUID
	// IsAsset marks an acquisition that goes into the asset register
	// (issue #78) instead of the expense lines. The EÜR report counts
	// only its depreciation, never the expense itself. Category is then
	// ignored by the report.
	IsAsset bool
	// StornoOfID is set, and only set, on a storno row. A storno row
	// carries the negated amounts of the row it reverses.
	StornoOfID *uuid.UUID
	// Imported marks an expense brought in by the historic import.
	Imported  bool
	BookedAt  *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MissingReceipt reports whether e is booked without a receipt. It is
// derived, never stored. A draft is never flagged, because its receipt
// may still be uploaded. A storno row copies the file of its original,
// so the storno of an original without a receipt is flagged too.
func (e Expense) MissingReceipt() bool {
	return e.State == BookingStateBooked && e.FileID == nil
}

// ExpenseFilter narrows ExpenseRepository.List. A nil field does not
// filter.
type ExpenseFilter struct {
	// Year filters on the calendar year of PaidOn.
	Year     *int
	State    *BookingState
	Category *category.Key
}
