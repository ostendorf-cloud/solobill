package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ostendorf-cloud/solobill/internal/category"
	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres/sqlcgen"
)

// expenseRepository satisfies storage.ExpenseRepository. Every method
// requires a context storage.TenantScope.WithTenant produced; see
// txFromContext.
//
// Immutability of a booked row is the job of db/migrations'
// expenses_block_booked_mutation trigger. The draft guards in
// queries/expenses.sql make a write against a booked row match nothing;
// the trigger refuses the write for any statement that does not carry
// them. Both end in storage.ErrConflict.
type expenseRepository struct{}

// Create implements storage.ExpenseRepository. The row is always created
// as a draft; State and the storno fields of expense are ignored.
func (r *expenseRepository) Create(ctx context.Context, expense storage.Expense) (storage.Expense, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Expense{}, err
	}

	row, err := sqlcgen.New(tx).CreateExpense(ctx, sqlcgen.CreateExpenseParams{
		TenantID:         expense.TenantID,
		PaidOn:           ptrToDate(expense.PaidOn),
		ReceiptDate:      ptrToDate(expense.ReceiptDate),
		Supplier:         expense.Supplier,
		Description:      expense.Description,
		GrossCents:       expense.GrossCents,
		NetCents:         expense.NetCents,
		VatRatePermyriad: expense.VATRatePermyriad,
		VatCents:         expense.VATCents,
		Category:         string(expense.Category),
		FileID:           ptrToUUID(expense.FileID),
		IsAsset:          expense.IsAsset,
		Imported:         expense.Imported,
	})
	if err != nil {
		return storage.Expense{}, mapLedgerWriteError(err)
	}

	return expenseFromRow(row), nil
}

// FindByID implements storage.ExpenseRepository.
func (r *expenseRepository) FindByID(ctx context.Context, id uuid.UUID) (storage.Expense, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Expense{}, err
	}

	row, err := sqlcgen.New(tx).GetExpenseByID(ctx, id)
	if err != nil {
		return storage.Expense{}, mapReadError(err)
	}

	return expenseFromRow(row), nil
}

// List implements storage.ExpenseRepository: ordered by paid_on, then
// creation, then id, drafts without paid_on last.
func (r *expenseRepository) List(ctx context.Context, filter storage.ExpenseFilter) ([]storage.Expense, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return nil, err
	}

	params := sqlcgen.ListExpensesParams{}
	if filter.Year != nil {
		params.PaidFrom, params.PaidBefore = yearRange(*filter.Year)
	}
	if filter.State != nil {
		params.State = pgtype.Text{String: string(*filter.State), Valid: true}
	}
	if filter.Category != nil {
		params.Category = pgtype.Text{String: string(*filter.Category), Valid: true}
	}

	rows, err := sqlcgen.New(tx).ListExpenses(ctx, params)
	if err != nil {
		return nil, mapReadError(err)
	}

	out := make([]storage.Expense, len(rows))
	for i, row := range rows {
		out[i] = expenseFromRow(row)
	}

	return out, nil
}

// UpdateDraft implements storage.ExpenseRepository.
func (r *expenseRepository) UpdateDraft(ctx context.Context, expense storage.Expense) (storage.Expense, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Expense{}, err
	}

	q := sqlcgen.New(tx)
	row, err := q.UpdateDraftExpense(ctx, sqlcgen.UpdateDraftExpenseParams{
		ID:               expense.ID,
		PaidOn:           ptrToDate(expense.PaidOn),
		ReceiptDate:      ptrToDate(expense.ReceiptDate),
		Supplier:         expense.Supplier,
		Description:      expense.Description,
		GrossCents:       expense.GrossCents,
		NetCents:         expense.NetCents,
		VatRatePermyriad: expense.VATRatePermyriad,
		VatCents:         expense.VATCents,
		Category:         string(expense.Category),
		FileID:           ptrToUUID(expense.FileID),
		IsAsset:          expense.IsAsset,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.Expense{}, existingExpenseConflict(ctx, q, expense.ID)
	}
	if err != nil {
		return storage.Expense{}, mapLedgerWriteError(err)
	}

	return expenseFromRow(row), nil
}

// DeleteDraft implements storage.ExpenseRepository.
func (r *expenseRepository) DeleteDraft(ctx context.Context, id uuid.UUID) error {
	tx, err := txFromContext(ctx)
	if err != nil {
		return err
	}

	q := sqlcgen.New(tx)
	affected, err := q.DeleteDraftExpense(ctx, id)
	if err != nil {
		return mapLedgerWriteError(err)
	}
	if affected == 0 {
		return existingExpenseConflict(ctx, q, id)
	}

	return nil
}

// Book implements storage.ExpenseRepository.
func (r *expenseRepository) Book(ctx context.Context, id uuid.UUID, bookedAt time.Time) (storage.Expense, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Expense{}, err
	}

	q := sqlcgen.New(tx)
	row, err := q.BookExpense(ctx, sqlcgen.BookExpenseParams{ID: id, BookedAt: toTimestamptz(bookedAt)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.Expense{}, existingExpenseConflict(ctx, q, id)
	}
	if err != nil {
		return storage.Expense{}, mapLedgerWriteError(err)
	}

	return expenseFromRow(row), nil
}

// Storno implements storage.ExpenseRepository. A second storno of the
// same original fails on expenses_reversed_once, which mapWriteError maps
// to storage.ErrConflict.
func (r *expenseRepository) Storno(ctx context.Context, id uuid.UUID, paidOn, bookedAt time.Time) (storage.Expense, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Expense{}, err
	}

	q := sqlcgen.New(tx)
	row, err := q.CreateExpenseStorno(ctx, sqlcgen.CreateExpenseStornoParams{
		PaidOn:     pgtype.Date{Time: paidOn, Valid: true},
		BookedAt:   toTimestamptz(bookedAt),
		OriginalID: id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.Expense{}, existingExpenseConflict(ctx, q, id)
	}
	if err != nil {
		return storage.Expense{}, mapLedgerWriteError(err)
	}

	return expenseFromRow(row), nil
}

// existingExpenseConflict tells the two causes of a guarded write that
// matched no row apart: the row exists in the tenant (wrong state,
// storage.ErrConflict) or it does not (storage.ErrNotFound).
func existingExpenseConflict(ctx context.Context, q *sqlcgen.Queries, id uuid.UUID) error {
	if _, err := q.GetExpenseByID(ctx, id); err != nil {
		return mapReadError(err)
	}

	return storage.ErrConflict
}

// yearRange is the half open date range of one calendar year: the first
// day of year and the first day of the next.
func yearRange(year int) (from, before pgtype.Date) {
	start := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)

	return pgtype.Date{Time: start, Valid: true}, pgtype.Date{Time: start.AddDate(1, 0, 0), Valid: true}
}

func expenseFromRow(row sqlcgen.Expense) storage.Expense {
	return storage.Expense{
		ID:               row.ID,
		TenantID:         row.TenantID,
		State:            storage.BookingState(row.State),
		PaidOn:           dateToPtr(row.PaidOn),
		ReceiptDate:      dateToPtr(row.ReceiptDate),
		Supplier:         row.Supplier,
		Description:      row.Description,
		GrossCents:       row.GrossCents,
		NetCents:         row.NetCents,
		VATRatePermyriad: row.VatRatePermyriad,
		VATCents:         row.VatCents,
		Category:         category.Key(row.Category),
		FileID:           uuidToPtr(row.FileID),
		IsAsset:          row.IsAsset,
		StornoOfID:       uuidToPtr(row.StornoOfID),
		Imported:         row.Imported,
		BookedAt:         timeToPtr(row.BookedAt),
		CreatedAt:        timeFromTimestamptz(row.CreatedAt),
		UpdatedAt:        timeFromTimestamptz(row.UpdatedAt),
	}
}
