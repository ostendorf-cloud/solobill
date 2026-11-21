//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// sqlStateCheckViolation is a CHECK constraint refusing a row.
const sqlStateCheckViolation = "23514"

func assetDate(t *testing.T, s string) time.Time {
	t.Helper()

	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}

	return d
}

func laptopAsset(t *testing.T, tenant storage.Tenant, acquired string) storage.Asset {
	t.Helper()

	life := int32(36)

	return storage.Asset{
		TenantID: tenant.ID, Name: "Laptop", AcquiredOn: assetDate(t, acquired), CostBasisCents: 149900,
		Method: storage.DepreciationLinear, Group: storage.AssetGroupOfficeEquipment, UsefulLifeMonths: &life,
	}
}

// TestAssetRepository covers the repository rules. Every refusal runs in
// a WithTenant scope of its own, so a refused statement never aborts the
// transaction a later assertion needs.
func TestAssetRepository(t *testing.T) {
	env := newProfileTestEnv(t)
	ctx := context.Background()
	repo := env.store.Assets()

	inA := func(fn func(ctx context.Context) error) error {
		return env.store.WithTenant(ctx, env.a.ID, fn)
	}

	var created storage.Asset
	expenseID := uuid.New()
	if err := inA(func(ctx context.Context) error {
		a := laptopAsset(t, env.a, "2026-03-10")
		a.ExpenseID = &expenseID
		var err error
		created, err = repo.Create(ctx, a)

		return err
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == uuid.Nil || created.Imported || created.ExpenseID == nil || *created.ExpenseID != expenseID ||
		created.UsefulLifeMonths == nil || *created.UsefulLifeMonths != 36 || !created.AcquiredOn.Equal(assetDate(t, "2026-03-10")) {
		t.Errorf("created = %+v", created)
	}

	t.Run("read back", func(t *testing.T) {
		if err := inA(func(ctx context.Context) error {
			got, err := repo.FindByID(ctx, created.ID)
			if err != nil || got.Name != "Laptop" || got.CostBasisCents != 149900 {
				t.Errorf("FindByID() = %+v, %v", got, err)
			}
			list, err := repo.List(ctx)
			if err != nil || len(list) != 1 {
				t.Errorf("List() = %d rows, %v, want 1", len(list), err)
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("another tenant sees nothing", func(t *testing.T) {
		err := env.store.WithTenant(ctx, env.b.ID, func(ctx context.Context) error {
			list, err := repo.List(ctx)
			if err != nil || len(list) != 0 {
				t.Errorf("tenant B List() = %d rows, %v, want none", len(list), err)
			}

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		err = env.store.WithTenant(ctx, env.b.ID, func(ctx context.Context) error {
			_, err := repo.FindByID(ctx, created.ID)

			return err
		})
		if !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("tenant B FindByID() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("one asset per expense", func(t *testing.T) {
		err := inA(func(ctx context.Context) error {
			a := laptopAsset(t, env.a, "2026-04-01")
			a.ExpenseID = &expenseID
			_, err := repo.Create(ctx, a)

			return err
		})
		if !errors.Is(err, storage.ErrConflict) {
			t.Errorf("second asset for one expense error = %v, want ErrConflict", err)
		}
	})

	t.Run("update while the first year is open", func(t *testing.T) {
		err := inA(func(ctx context.Context) error {
			a := created
			a.Name = "Laptop 14"
			got, err := repo.Update(ctx, a, assetDate(t, "2026-12-31"))
			if err == nil && got.Name != "Laptop 14" {
				t.Errorf("Update() = %+v", got)
			}

			return err
		})
		if err != nil {
			t.Errorf("Update() in 2026 error = %v", err)
		}
	})

	t.Run("update after the first year is refused", func(t *testing.T) {
		err := inA(func(ctx context.Context) error {
			_, err := repo.Update(ctx, created, assetDate(t, "2027-01-01"))

			return err
		})
		if !errors.Is(err, storage.ErrConflict) {
			t.Errorf("Update() in 2027 error = %v, want ErrConflict", err)
		}
	})

	t.Run("moving into a closed year is refused", func(t *testing.T) {
		err := inA(func(ctx context.Context) error {
			a := created
			a.AcquiredOn = assetDate(t, "2025-12-01")
			_, err := repo.Update(ctx, a, assetDate(t, "2026-06-01"))

			return err
		})
		if !errors.Is(err, storage.ErrConflict) {
			t.Errorf("Update() into 2025 error = %v, want ErrConflict", err)
		}
	})

	t.Run("update of a missing asset", func(t *testing.T) {
		err := inA(func(ctx context.Context) error {
			a := created
			a.ID = uuid.New()
			_, err := repo.Update(ctx, a, assetDate(t, "2026-06-01"))

			return err
		})
		if !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("Update() of a missing asset error = %v, want ErrNotFound", err)
		}
	})

	t.Run("disposal once", func(t *testing.T) {
		proceeds := int64(20000)
		err := inA(func(ctx context.Context) error {
			got, err := repo.RecordDisposal(ctx, created.ID, assetDate(t, "2026-11-30"), &proceeds)
			if err == nil && (got.DisposedOn == nil || got.DisposalProceedsCents == nil || *got.DisposalProceedsCents != 20000) {
				t.Errorf("RecordDisposal() = %+v", got)
			}

			return err
		})
		if err != nil {
			t.Fatalf("RecordDisposal() error = %v", err)
		}

		err = inA(func(ctx context.Context) error {
			_, err := repo.RecordDisposal(ctx, created.ID, assetDate(t, "2026-12-01"), nil)

			return err
		})
		if !errors.Is(err, storage.ErrConflict) {
			t.Errorf("second RecordDisposal() error = %v, want ErrConflict", err)
		}

		err = inA(func(ctx context.Context) error {
			_, err := repo.Update(ctx, created, assetDate(t, "2026-12-01"))

			return err
		})
		if !errors.Is(err, storage.ErrConflict) {
			t.Errorf("Update() after disposal error = %v, want ErrConflict", err)
		}

		err = inA(func(ctx context.Context) error {
			_, err := repo.RecordDisposal(ctx, uuid.New(), assetDate(t, "2026-12-01"), nil)

			return err
		})
		if !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("RecordDisposal() of a missing asset error = %v, want ErrNotFound", err)
		}
	})
}

// TestAssetsDatabaseRefusals goes around the repository with raw SQL as
// the restricted role, to prove the table itself refuses what the Go
// checks refuse. Each statement runs in a tenant scoped transaction of
// its own that is rolled back.
func TestAssetsDatabaseRefusals(t *testing.T) {
	env := newProfileTestEnv(t)
	ctx := context.Background()

	scoped := func(t *testing.T, tenant uuid.UUID, sql string, args ...any) error {
		t.Helper()

		tx, err := env.pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
			t.Fatalf("scope: %v", err)
		}
		_, err = tx.Exec(ctx, sql, args...)

		return err
	}

	const insert = `INSERT INTO assets (tenant_id, name, acquired_on, cost_basis_cents, method, asset_group, useful_life_months,
		opening_book_value_cents, opening_book_value_year, disposed_on)
		VALUES ($1, 'Item', $2, $3, $4, 'office_equipment', $5, $6, $7, $8)`

	// The valid row first: every refusal below differs from it in one value.
	if err := scoped(t, env.a.ID, insert, env.a.ID, "2026-03-10", 149900, "linear", 36, nil, nil, nil); err != nil {
		t.Fatalf("valid insert: %v", err)
	}

	tests := []struct {
		name string
		args []any
	}{
		{"degressive method", []any{env.a.ID, "2026-03-10", 149900, "degressive", 36, nil, nil, nil}},
		{"gwg at 800.01", []any{env.a.ID, "2026-03-10", 80001, "gwg", nil, nil, nil, nil}},
		{"immediate at 250.01", []any{env.a.ID, "2026-03-10", 25001, "immediate", nil, nil, nil, nil}},
		{"sammelposten at 250.00", []any{env.a.ID, "2026-03-10", 25000, "sammelposten", nil, nil, nil, nil}},
		{"sammelposten at 1000.01", []any{env.a.ID, "2026-03-10", 100001, "sammelposten", nil, nil, nil, nil}},
		{"linear without useful life", []any{env.a.ID, "2026-03-10", 149900, "linear", nil, nil, nil, nil}},
		{"useful life on a gwg", []any{env.a.ID, "2026-03-10", 50000, "gwg", 12, nil, nil, nil}},
		{"zero cost", []any{env.a.ID, "2026-03-10", 0, "linear", 36, nil, nil, nil}},
		{"opening value without year", []any{env.a.ID, "2024-03-10", 149900, "linear", 36, 1000, nil, nil}},
		{"opening year not after acquisition", []any{env.a.ID, "2026-03-10", 149900, "linear", 36, 1000, 2026, nil}},
		{"opening value above cost", []any{env.a.ID, "2024-03-10", 149900, "linear", 36, 149901, 2026, nil}},
		{"disposal before acquisition", []any{env.a.ID, "2026-03-10", 149900, "linear", 36, nil, nil, "2026-03-09"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireSQLState(t, scoped(t, env.a.ID, insert, tt.args...), sqlStateCheckViolation, tt.name)
		})
	}

	t.Run("insert for another tenant", func(t *testing.T) {
		err := scoped(t, env.a.ID, insert, env.b.ID, "2026-03-10", 149900, "linear", 36, nil, nil, nil)
		requireSQLState(t, err, sqlStateInsufficientPrivilege, "insert with tenant B's id in tenant A's scope")
	})

	t.Run("no DELETE grant", func(t *testing.T) {
		requireSQLState(t, scoped(t, env.a.ID, `DELETE FROM assets`), sqlStateInsufficientPrivilege, "DELETE FROM assets")
	})

	t.Run("no TRUNCATE grant", func(t *testing.T) {
		requireSQLState(t, scoped(t, env.a.ID, `TRUNCATE assets`), sqlStateInsufficientPrivilege, "TRUNCATE assets")
	})
}
