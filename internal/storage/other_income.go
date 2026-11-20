package storage

import (
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/category"
)

// OtherIncome is a Betriebseinnahme outside an invoice: a VAT refund of
// the Finanzamt, the sale of an asset, private use. Same lifecycle and
// immutability as Expense. Issue #76 (I10a) creates the table.
type OtherIncome struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	State    BookingState
	// ReceivedOn is the Zufluss date. Booking requires it.
	ReceivedOn       *time.Time
	Description      string
	GrossCents       int64
	NetCents         int64
	VATRatePermyriad int32
	VATCents         int64
	// Category is a category.KindIncome key.
	Category   category.Key
	FileID     *uuid.UUID
	StornoOfID *uuid.UUID
	Imported   bool
	BookedAt   *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// OtherIncomeFilter narrows OtherIncomeRepository.List. A nil field does
// not filter.
type OtherIncomeFilter struct {
	// Year filters on the calendar year of ReceivedOn.
	Year     *int
	State    *BookingState
	Category *category.Key
}
