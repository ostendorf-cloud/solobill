//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// TestVATRegimePeriodsConcurrentDoubleSupersede runs several corrections of
// the same row at once. The per tenant lock and the unique index let
// exactly one through, and the rest are ErrConflict, never a second row.
func TestVATRegimePeriodsConcurrentDoubleSupersede(t *testing.T) {
	env := newProfileTestEnv(t)
	ctx := context.Background()
	repo := env.store.VATRegimePeriods()

	var wrong storage.VATRegimePeriod
	if err := env.store.WithTenant(ctx, env.a.ID, func(ctx context.Context) error {
		var err error
		wrong, err = repo.Create(ctx, storage.VATRegimePeriod{
			TenantID: env.a.ID, ValidFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Regime: storage.VATRegimeKleinunternehmer,
		})

		return err
	}); err != nil {
		t.Fatalf("create the wrong period: %v", err)
	}

	const writers = 6

	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			errs[i] = env.store.WithTenant(ctx, env.a.ID, func(ctx context.Context) error {
				// A different valid_from each, so only the supersede rule
				// can refuse all but one.
				_, err := repo.Create(ctx, storage.VATRegimePeriod{
					TenantID:     env.a.ID,
					ValidFrom:    time.Date(2026, 2, 1+i, 0, 0, 0, 0, time.UTC),
					Regime:       storage.VATRegimeStandard,
					SupersedesID: &wrong.ID,
				})

				return err
			})
		}()
	}
	wg.Wait()

	succeeded, conflicted := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, storage.ErrConflict):
			conflicted++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != writers-1 {
		t.Errorf("succeeded = %d, conflicted = %d, want 1 and %d", succeeded, conflicted, writers-1)
	}
}

// TestVATRegimePeriodsConcurrentSameDate runs several inserts for one
// valid_from at once: exactly one effective period may result.
func TestVATRegimePeriodsConcurrentSameDate(t *testing.T) {
	env := newProfileTestEnv(t)
	ctx := context.Background()
	repo := env.store.VATRegimePeriods()

	const writers = 6

	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			errs[i] = env.store.WithTenant(ctx, env.a.ID, func(ctx context.Context) error {
				_, err := repo.Create(ctx, storage.VATRegimePeriod{
					TenantID: env.a.ID, ValidFrom: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), Regime: storage.VATRegimeStandard,
				})

				return err
			})
		}()
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, storage.ErrConflict):
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if succeeded != 1 {
		t.Errorf("succeeded = %d, want exactly 1", succeeded)
	}
}

// TestVATRegimePeriodsDatabaseConstraints inserts with raw SQL, past the
// Go checks, to prove the database itself refuses a second effective
// period for a date and a second correction of one row.
func TestVATRegimePeriodsDatabaseConstraints(t *testing.T) {
	env := newProfileTestEnv(t)
	ctx := context.Background()

	scoped := func(t *testing.T, fn func(exec func(sql string, args ...any) error) error) error {
		t.Helper()

		tx, err := env.pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, env.a.ID.String()); err != nil {
			t.Fatalf("scope: %v", err)
		}

		return fn(func(sql string, args ...any) error {
			_, err := tx.Exec(ctx, sql, args...)

			return err
		})
	}

	first, second := uuid.New(), uuid.New()
	const insert = `INSERT INTO vat_regime_periods (id, tenant_id, valid_from, regime, supersedes_id) VALUES ($1, $2, $3, 'standard', $4)`

	t.Run("two effective periods for one date", func(t *testing.T) {
		err := scoped(t, func(exec func(string, ...any) error) error {
			if err := exec(insert, first, env.a.ID, "2026-04-01", nil); err != nil {
				t.Fatalf("first insert: %v", err)
			}

			return exec(insert, second, env.a.ID, "2026-04-01", nil)
		})
		requireSQLState(t, err, sqlStateUniqueViolation, "second effective period for 2026-04-01")
	})

	t.Run("a correction may reuse the date of the row it supersedes", func(t *testing.T) {
		err := scoped(t, func(exec func(string, ...any) error) error {
			if err := exec(insert, first, env.a.ID, "2026-04-01", nil); err != nil {
				t.Fatalf("first insert: %v", err)
			}

			return exec(insert, second, env.a.ID, "2026-04-01", first)
		})
		if err != nil {
			t.Errorf("correction on the same date: %v", err)
		}
	})

	t.Run("a superseded row is superseded at most once", func(t *testing.T) {
		err := scoped(t, func(exec func(string, ...any) error) error {
			if err := exec(insert, first, env.a.ID, "2026-04-01", nil); err != nil {
				t.Fatalf("first insert: %v", err)
			}
			if err := exec(insert, second, env.a.ID, "2026-05-01", first); err != nil {
				t.Fatalf("first correction: %v", err)
			}

			return exec(insert, uuid.New(), env.a.ID, "2026-06-01", first)
		})
		requireSQLState(t, err, sqlStateUniqueViolation, "second correction of one row")
	})
}
