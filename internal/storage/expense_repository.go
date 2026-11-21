package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ExpenseRepository persists expenses. Every method must run inside a
// context TenantScope.WithTenant produced. Implemented by issue #76
// (I10a).
type ExpenseRepository interface {
	Create(ctx context.Context, expense Expense) (Expense, error)
	FindByID(ctx context.Context, id uuid.UUID) (Expense, error)
	List(ctx context.Context, filter ExpenseFilter) ([]Expense, error)

	// UpdateDraft replaces a draft. ErrConflict when it is booked.
	UpdateDraft(ctx context.Context, expense Expense) (Expense, error)

	// DeleteDraft removes a draft. ErrConflict when it is booked.
	DeleteDraft(ctx context.Context, id uuid.UUID) error

	// Book moves a draft to BookingStateBooked. ErrConflict when it is
	// already booked or PaidOn is nil.
	Book(ctx context.Context, id uuid.UUID, bookedAt time.Time) (Expense, error)

	// Storno creates and books the reversing row of a booked expense,
	// paid on paidOn. ErrConflict when the original is a draft, is
	// itself a storno or was already reversed.
	Storno(ctx context.Context, id uuid.UUID, paidOn, bookedAt time.Time) (Expense, error)
}
