package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// OtherIncomeRepository persists other income. Every method must run
// inside a context TenantScope.WithTenant produced. The semantics of
// each method match ExpenseRepository. Implemented by issue #76 (I10a).
type OtherIncomeRepository interface {
	Create(ctx context.Context, income OtherIncome) (OtherIncome, error)
	FindByID(ctx context.Context, id uuid.UUID) (OtherIncome, error)
	List(ctx context.Context, filter OtherIncomeFilter) ([]OtherIncome, error)
	UpdateDraft(ctx context.Context, income OtherIncome) (OtherIncome, error)
	DeleteDraft(ctx context.Context, id uuid.UUID) error
	Book(ctx context.Context, id uuid.UUID, bookedAt time.Time) (OtherIncome, error)
	Storno(ctx context.Context, id uuid.UUID, receivedOn, bookedAt time.Time) (OtherIncome, error)
}
