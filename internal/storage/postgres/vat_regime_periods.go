package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres/sqlcgen"
)

// vatRegimePeriodRepository satisfies storage.VATRegimePeriodRepository.
// Every method requires a context storage.TenantScope.WithTenant
// produced; see txFromContext.
type vatRegimePeriodRepository struct{}

// List implements storage.VATRegimePeriodRepository.
func (r *vatRegimePeriodRepository) List(ctx context.Context) ([]storage.VATRegimePeriod, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := sqlcgen.New(tx).ListVATRegimePeriods(ctx)
	if err != nil {
		return nil, mapReadError(err)
	}

	periods := make([]storage.VATRegimePeriod, len(rows))
	for i, row := range rows {
		periods[i] = vatRegimePeriodFromRow(row)
	}

	return periods, nil
}

// Create implements storage.VATRegimePeriodRepository. It takes a
// per tenant advisory lock first, so the duplicate valid_from check
// below cannot race a concurrent insert. A superseded row that does not
// exist in this tenant (another tenant's included, row level security
// hides it) is storage.ErrNotFound.
func (r *vatRegimePeriodRepository) Create(ctx context.Context, period storage.VATRegimePeriod) (storage.VATRegimePeriod, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.VATRegimePeriod{}, err
	}

	q := sqlcgen.New(tx)

	if err := q.LockVATRegimePeriods(ctx); err != nil {
		return storage.VATRegimePeriod{}, mapWriteError(err)
	}

	if period.SupersedesID != nil {
		if _, err := q.GetVATRegimePeriod(ctx, *period.SupersedesID); err != nil {
			return storage.VATRegimePeriod{}, mapReadError(err)
		}

		superseded, err := q.IsVATRegimePeriodSuperseded(ctx, ptrToUUID(period.SupersedesID))
		if err != nil {
			return storage.VATRegimePeriod{}, mapReadError(err)
		}
		if superseded {
			return storage.VATRegimePeriod{}, storage.ErrConflict
		}
	}

	same, err := q.CountEffectiveVATRegimePeriodsFrom(ctx, sqlcgen.CountEffectiveVATRegimePeriodsFromParams{
		ValidFrom:    pgtype.Date{Time: period.ValidFrom, Valid: true},
		SupersededID: ptrToUUID(period.SupersedesID),
	})
	if err != nil {
		return storage.VATRegimePeriod{}, mapReadError(err)
	}
	if same > 0 {
		return storage.VATRegimePeriod{}, storage.ErrConflict
	}

	row, err := q.CreateVATRegimePeriod(ctx, sqlcgen.CreateVATRegimePeriodParams{
		TenantID:     period.TenantID,
		ValidFrom:    pgtype.Date{Time: period.ValidFrom, Valid: true},
		Regime:       string(period.Regime),
		Note:         ptrToText(period.Note),
		SupersedesID: ptrToUUID(period.SupersedesID),
	})
	if err != nil {
		return storage.VATRegimePeriod{}, mapWriteError(err)
	}

	return vatRegimePeriodFromRow(row), nil
}

// RegimeOn implements storage.VATRegimePeriodRepository.
func (r *vatRegimePeriodRepository) RegimeOn(ctx context.Context, date time.Time) (storage.VATRegimePeriod, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.VATRegimePeriod{}, err
	}

	row, err := sqlcgen.New(tx).RegimeOnVATDate(ctx, pgtype.Date{Time: date, Valid: true})
	if err != nil {
		return storage.VATRegimePeriod{}, mapReadError(err)
	}

	return vatRegimePeriodFromRow(row), nil
}

func vatRegimePeriodFromRow(row sqlcgen.VatRegimePeriod) storage.VATRegimePeriod {
	return storage.VATRegimePeriod{
		ID:           row.ID,
		TenantID:     row.TenantID,
		ValidFrom:    row.ValidFrom.Time,
		Regime:       storage.VATRegime(row.Regime),
		SupersedesID: uuidToPtr(row.SupersedesID),
		Note:         textToPtr(row.Note),
		CreatedAt:    timeFromTimestamptz(row.CreatedAt),
	}
}
