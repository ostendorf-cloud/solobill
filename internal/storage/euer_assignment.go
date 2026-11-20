package storage

import (
	"time"

	"github.com/google/uuid"
)

// EuerEntryKind names the table an EuerAssignment points into.
type EuerEntryKind string

const (
	// EuerEntryPayment is a row of payments.
	EuerEntryPayment EuerEntryKind = "payment"
	// EuerEntryExpense is a row of expenses.
	EuerEntryExpense EuerEntryKind = "expense"
	// EuerEntryOtherIncome is a row of other_income.
	EuerEntryOtherIncome EuerEntryKind = "other_income"
)

// EuerAssignment is the owner's decision which tax year a regularly
// recurring entry near the turn of the year belongs to (section 11 Abs.
// 1 Satz 2 and Abs. 2 Satz 2 EStG). The report lists entries from 22
// December to 10 January; without an assignment an entry counts in the
// year of its payment date. Issue #80 (I12) creates the table.
//
// The table is append-only. A changed decision is a new row naming the
// old one in SupersedesID.
type EuerAssignment struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	EntryKind EuerEntryKind
	EntryID   uuid.UUID
	// TaxYear is the year the entry counts in.
	TaxYear int32
	// Note is the owner's reason, for example "Miete Dezember".
	Note         *string
	SupersedesID *uuid.UUID
	CreatedBy    *uuid.UUID
	CreatedAt    time.Time
}
