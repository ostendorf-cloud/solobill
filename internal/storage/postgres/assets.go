package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ostendorf-cloud/solobill/internal/docissue"
	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres/sqlcgen"
)

// assetRepository satisfies storage.AssetRepository. Every method
// requires a context storage.TenantScope.WithTenant produced; see
// txFromContext.
type assetRepository struct{}

// Create implements storage.AssetRepository. A second asset for the same
// expense is storage.ErrConflict (assets_expense_once).
func (r *assetRepository) Create(ctx context.Context, a storage.Asset) (storage.Asset, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Asset{}, err
	}

	row, err := sqlcgen.New(tx).CreateAsset(ctx, sqlcgen.CreateAssetParams{
		TenantID:              a.TenantID,
		Name:                  a.Name,
		AcquiredOn:            pgtype.Date{Time: a.AcquiredOn, Valid: true},
		CostBasisCents:        a.CostBasisCents,
		Method:                string(a.Method),
		AssetGroup:            string(a.Group),
		UsefulLifeMonths:      ptrToInt4(a.UsefulLifeMonths),
		ExpenseID:             ptrToUUID(a.ExpenseID),
		OpeningBookValueCents: ptrToInt8(a.OpeningBookValueCents),
		OpeningBookValueYear:  ptrToInt4(a.OpeningBookValueYear),
		Imported:              a.Imported,
	})
	if err != nil {
		return storage.Asset{}, mapWriteError(err)
	}

	return assetFromRow(row), nil
}

// FindByID implements storage.AssetRepository.
func (r *assetRepository) FindByID(ctx context.Context, id uuid.UUID) (storage.Asset, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Asset{}, err
	}

	row, err := sqlcgen.New(tx).GetAsset(ctx, id)
	if err != nil {
		return storage.Asset{}, mapReadError(err)
	}

	return assetFromRow(row), nil
}

// List implements storage.AssetRepository.
func (r *assetRepository) List(ctx context.Context) ([]storage.Asset, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := sqlcgen.New(tx).ListAssets(ctx)
	if err != nil {
		return nil, mapReadError(err)
	}

	assets := make([]storage.Asset, len(rows))
	for i, row := range rows {
		assets[i] = assetFromRow(row)
	}

	return assets, nil
}

// Update implements storage.AssetRepository. Both the stored row and the
// replacement must have a first schedule year that has not ended at now:
// moving an open asset into a closed year would change a figure of a
// year that may already be filed. A disposed asset is not edited either.
// Every refusal is storage.ErrConflict, a missing row storage.ErrNotFound.
//
// The year is the German calendar year of now (docissue.IssueDate), the
// year an EÜR is filed for. The replacement is checked here, before the
// UPDATE is sent; the stored row is checked by the UPDATE's own WHERE.
func (r *assetRepository) Update(ctx context.Context, a storage.Asset, now time.Time) (storage.Asset, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Asset{}, err
	}

	q := sqlcgen.New(tx)
	openYear := docissue.IssueDate(now).Year()

	if firstScheduleYear(a) < openYear {
		if _, err := q.GetAsset(ctx, a.ID); err != nil {
			return storage.Asset{}, mapReadError(err)
		}

		return storage.Asset{}, storage.ErrConflict
	}

	row, err := q.UpdateAsset(ctx, sqlcgen.UpdateAssetParams{
		Name:                  a.Name,
		AcquiredOn:            pgtype.Date{Time: a.AcquiredOn, Valid: true},
		CostBasisCents:        a.CostBasisCents,
		Method:                string(a.Method),
		AssetGroup:            string(a.Group),
		UsefulLifeMonths:      ptrToInt4(a.UsefulLifeMonths),
		ExpenseID:             ptrToUUID(a.ExpenseID),
		OpeningBookValueCents: ptrToInt8(a.OpeningBookValueCents),
		OpeningBookValueYear:  ptrToInt4(a.OpeningBookValueYear),
		ID:                    a.ID,
		OpenYear:              int32(openYear),
	})
	if err == nil {
		return assetFromRow(row), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return storage.Asset{}, mapWriteError(err)
	}

	return storage.Asset{}, r.missingOrConflict(ctx, q, a.ID)
}

// firstScheduleYear mirrors afa.FirstScheduleYear on the stored shape:
// the opening year of an imported asset, the acquisition year otherwise.
func firstScheduleYear(a storage.Asset) int {
	if a.OpeningBookValueYear != nil {
		return int(*a.OpeningBookValueYear)
	}

	return a.AcquiredOn.Year()
}

// RecordDisposal implements storage.AssetRepository.
func (r *assetRepository) RecordDisposal(ctx context.Context, id uuid.UUID, disposedOn time.Time, proceedsCents *int64) (storage.Asset, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Asset{}, err
	}

	q := sqlcgen.New(tx)

	row, err := q.RecordAssetDisposal(ctx, sqlcgen.RecordAssetDisposalParams{
		DisposedOn:            pgtype.Date{Time: disposedOn, Valid: true},
		DisposalProceedsCents: ptrToInt8(proceedsCents),
		ID:                    id,
	})
	if err == nil {
		return assetFromRow(row), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return storage.Asset{}, mapWriteError(err)
	}

	return storage.Asset{}, r.missingOrConflict(ctx, q, id)
}

// missingOrConflict tells why a guarded UPDATE matched no row: the row
// is not there (storage.ErrNotFound) or its state refused the change
// (storage.ErrConflict).
func (r *assetRepository) missingOrConflict(ctx context.Context, q *sqlcgen.Queries, id uuid.UUID) error {
	if _, err := q.GetAsset(ctx, id); err != nil {
		return mapReadError(err)
	}

	return storage.ErrConflict
}

func assetFromRow(row sqlcgen.Asset) storage.Asset {
	return storage.Asset{
		ID:                    row.ID,
		TenantID:              row.TenantID,
		Name:                  row.Name,
		AcquiredOn:            row.AcquiredOn.Time,
		CostBasisCents:        row.CostBasisCents,
		Method:                storage.DepreciationMethod(row.Method),
		Group:                 storage.AssetGroup(row.AssetGroup),
		UsefulLifeMonths:      int4ToPtr(row.UsefulLifeMonths),
		ExpenseID:             uuidToPtr(row.ExpenseID),
		OpeningBookValueCents: int8ToPtr(row.OpeningBookValueCents),
		OpeningBookValueYear:  int4ToPtr(row.OpeningBookValueYear),
		DisposedOn:            dateToPtr(row.DisposedOn),
		DisposalProceedsCents: int8ToPtr(row.DisposalProceedsCents),
		Imported:              row.Imported,
		CreatedAt:             timeFromTimestamptz(row.CreatedAt),
		UpdatedAt:             timeFromTimestamptz(row.UpdatedAt),
	}
}
