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

// otherIncomeRepository satisfies storage.OtherIncomeRepository, with the
// same draft guards and the same error mapping as expenseRepository.
type otherIncomeRepository struct{}

// Create implements storage.OtherIncomeRepository. The row is always
// created as a draft.
func (r *otherIncomeRepository) Create(ctx context.Context, income storage.OtherIncome) (storage.OtherIncome, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.OtherIncome{}, err
	}

	row, err := sqlcgen.New(tx).CreateOtherIncome(ctx, sqlcgen.CreateOtherIncomeParams{
		TenantID:         income.TenantID,
		ReceivedOn:       ptrToDate(income.ReceivedOn),
		Description:      income.Description,
		GrossCents:       income.GrossCents,
		NetCents:         income.NetCents,
		VatRatePermyriad: income.VATRatePermyriad,
		VatCents:         income.VATCents,
		Category:         string(income.Category),
		FileID:           ptrToUUID(income.FileID),
		Imported:         income.Imported,
	})
	if err != nil {
		return storage.OtherIncome{}, mapDocumentWriteError(err)
	}

	return otherIncomeFromRow(row), nil
}

// FindByID implements storage.OtherIncomeRepository.
func (r *otherIncomeRepository) FindByID(ctx context.Context, id uuid.UUID) (storage.OtherIncome, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.OtherIncome{}, err
	}

	row, err := sqlcgen.New(tx).GetOtherIncomeByID(ctx, id)
	if err != nil {
		return storage.OtherIncome{}, mapReadError(err)
	}

	return otherIncomeFromRow(row), nil
}

// List implements storage.OtherIncomeRepository: ordered by received_on,
// then creation, then id, drafts without received_on last.
func (r *otherIncomeRepository) List(ctx context.Context, filter storage.OtherIncomeFilter) ([]storage.OtherIncome, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return nil, err
	}

	params := sqlcgen.ListOtherIncomeParams{}
	if filter.Year != nil {
		params.ReceivedFrom, params.ReceivedBefore = yearRange(*filter.Year)
	}
	if filter.State != nil {
		params.State = pgtype.Text{String: string(*filter.State), Valid: true}
	}
	if filter.Category != nil {
		params.Category = pgtype.Text{String: string(*filter.Category), Valid: true}
	}

	rows, err := sqlcgen.New(tx).ListOtherIncome(ctx, params)
	if err != nil {
		return nil, mapReadError(err)
	}

	out := make([]storage.OtherIncome, len(rows))
	for i, row := range rows {
		out[i] = otherIncomeFromRow(row)
	}

	return out, nil
}

// UpdateDraft implements storage.OtherIncomeRepository.
func (r *otherIncomeRepository) UpdateDraft(ctx context.Context, income storage.OtherIncome) (storage.OtherIncome, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.OtherIncome{}, err
	}

	q := sqlcgen.New(tx)
	row, err := q.UpdateDraftOtherIncome(ctx, sqlcgen.UpdateDraftOtherIncomeParams{
		ID:               income.ID,
		ReceivedOn:       ptrToDate(income.ReceivedOn),
		Description:      income.Description,
		GrossCents:       income.GrossCents,
		NetCents:         income.NetCents,
		VatRatePermyriad: income.VATRatePermyriad,
		VatCents:         income.VATCents,
		Category:         string(income.Category),
		FileID:           ptrToUUID(income.FileID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.OtherIncome{}, existingOtherIncomeConflict(ctx, q, income.ID)
	}
	if err != nil {
		return storage.OtherIncome{}, mapDocumentWriteError(err)
	}

	return otherIncomeFromRow(row), nil
}

// DeleteDraft implements storage.OtherIncomeRepository.
func (r *otherIncomeRepository) DeleteDraft(ctx context.Context, id uuid.UUID) error {
	tx, err := txFromContext(ctx)
	if err != nil {
		return err
	}

	q := sqlcgen.New(tx)
	affected, err := q.DeleteDraftOtherIncome(ctx, id)
	if err != nil {
		return mapDocumentWriteError(err)
	}
	if affected == 0 {
		return existingOtherIncomeConflict(ctx, q, id)
	}

	return nil
}

// Book implements storage.OtherIncomeRepository.
func (r *otherIncomeRepository) Book(ctx context.Context, id uuid.UUID, bookedAt time.Time) (storage.OtherIncome, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.OtherIncome{}, err
	}

	q := sqlcgen.New(tx)
	row, err := q.BookOtherIncome(ctx, sqlcgen.BookOtherIncomeParams{ID: id, BookedAt: toTimestamptz(bookedAt)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.OtherIncome{}, existingOtherIncomeConflict(ctx, q, id)
	}
	if err != nil {
		return storage.OtherIncome{}, mapDocumentWriteError(err)
	}

	return otherIncomeFromRow(row), nil
}

// Storno implements storage.OtherIncomeRepository.
func (r *otherIncomeRepository) Storno(ctx context.Context, id uuid.UUID, receivedOn, bookedAt time.Time) (storage.OtherIncome, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.OtherIncome{}, err
	}

	q := sqlcgen.New(tx)
	row, err := q.CreateOtherIncomeStorno(ctx, sqlcgen.CreateOtherIncomeStornoParams{
		ReceivedOn: pgtype.Date{Time: receivedOn, Valid: true},
		BookedAt:   toTimestamptz(bookedAt),
		OriginalID: id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.OtherIncome{}, existingOtherIncomeConflict(ctx, q, id)
	}
	if err != nil {
		return storage.OtherIncome{}, mapDocumentWriteError(err)
	}

	return otherIncomeFromRow(row), nil
}

// existingOtherIncomeConflict is existingExpenseConflict for other income.
func existingOtherIncomeConflict(ctx context.Context, q *sqlcgen.Queries, id uuid.UUID) error {
	if _, err := q.GetOtherIncomeByID(ctx, id); err != nil {
		return mapReadError(err)
	}

	return storage.ErrConflict
}

func otherIncomeFromRow(row sqlcgen.OtherIncome) storage.OtherIncome {
	return storage.OtherIncome{
		ID:               row.ID,
		TenantID:         row.TenantID,
		State:            storage.BookingState(row.State),
		ReceivedOn:       dateToPtr(row.ReceivedOn),
		Description:      row.Description,
		GrossCents:       row.GrossCents,
		NetCents:         row.NetCents,
		VATRatePermyriad: row.VatRatePermyriad,
		VATCents:         row.VatCents,
		Category:         category.Key(row.Category),
		FileID:           uuidToPtr(row.FileID),
		StornoOfID:       uuidToPtr(row.StornoOfID),
		Imported:         row.Imported,
		BookedAt:         timeToPtr(row.BookedAt),
		CreatedAt:        timeFromTimestamptz(row.CreatedAt),
		UpdatedAt:        timeFromTimestamptz(row.UpdatedAt),
	}
}
