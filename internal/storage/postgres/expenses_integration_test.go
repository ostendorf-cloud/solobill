//go:build integration

// Same reasons as integration_test.go's own build tag comment: needs a
// real, reachable Postgres, excluded from a plain "go test ./..." by the
// tag above, and DATABASE_URL still checked at runtime, t.Skip()ing
// without it.
package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostendorf-cloud/solobill/internal/category"
	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres"
)

// expenseTestEnv is one restricted store with two tenants and a receipt
// file in each.
type expenseTestEnv struct {
	store          *postgres.Store
	restrictedPool *pgxpool.Pool
	tenantA        storage.Tenant
	tenantB        storage.Tenant
	fileA, fileB   uuid.UUID
}

func newExpenseTestEnv(ctx context.Context, t *testing.T) expenseTestEnv {
	t.Helper()

	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed expenses test")
	}

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	restrictedPool, err := postgres.NewPool(ctx, setUpTestAppRole(ctx, t, adminPool, adminURL))
	if err != nil {
		t.Fatalf("open restricted pool as %s: %v", testAppRole, err)
	}
	t.Cleanup(restrictedPool.Close)

	store := postgres.NewStore(restrictedPool)
	env := expenseTestEnv{store: store, restrictedPool: restrictedPool}

	if env.tenantA, err = store.Tenants().Create(ctx, storage.Tenant{Slug: "exp-a-" + uuid.NewString(), Name: "Expenses A"}); err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	if env.tenantB, err = store.Tenants().Create(ctx, storage.Tenant{Slug: "exp-b-" + uuid.NewString(), Name: "Expenses B"}); err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	addFile := func(tenantID uuid.UUID) uuid.UUID {
		id := uuid.New()
		if err := store.WithTenant(ctx, tenantID, func(ctx context.Context) error {
			_, err := store.Files().Create(ctx, storage.File{
				ID:          id,
				TenantID:    tenantID,
				Purpose:     storage.FilePurposeReceipt,
				ObjectKey:   tenantID.String() + "/" + id.String(),
				SHA256:      testFileSHA256,
				SizeBytes:   4,
				ContentType: "application/pdf",
				UploadedAt:  time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC),
			})

			return err
		}); err != nil {
			t.Fatalf("create file: %v", err)
		}

		return id
	}
	env.fileA = addFile(env.tenantA.ID)
	env.fileB = addFile(env.tenantB.ID)

	return env
}

func expenseDate(s string) *time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}

	return &d
}

func testExpense(tenantID uuid.UUID, paidOn string) storage.Expense {
	return storage.Expense{
		TenantID:    tenantID,
		PaidOn:      expenseDate(paidOn),
		Supplier:    "Office Supplies Inc",
		Description: "Paper",
		GrossCents:  11900,
		NetCents:    10000,
		VATCents:    1900,
		// The table checks shape and sums, not the regime: that is the
		// application's check, against vat_regime_periods.
		VATRatePermyriad: 1900,
		Category:         category.WorkEquipment,
	}
}

// inTenant runs fn in a WithTenant scope and fails the test on an error.
func (env expenseTestEnv) inTenant(ctx context.Context, t *testing.T, tenantID uuid.UUID, fn func(ctx context.Context) error) {
	t.Helper()

	if err := env.store.WithTenant(ctx, tenantID, fn); err != nil {
		t.Fatal(err)
	}
}

// TestExpenseRepository proves the expenses table end to end through the
// restricted role: tenant isolation for every method, the receipt of
// another tenant refused by the composite foreign key, immutability of a
// booked row against the repository and raw SQL, the storno rules, and
// the year filter at the year boundary.
func TestExpenseRepository(t *testing.T) {
	ctx := context.Background()
	env := newExpenseTestEnv(ctx, t)
	repo := env.store.Expenses()

	var draft, booked storage.Expense
	env.inTenant(ctx, t, env.tenantA.ID, func(ctx context.Context) error {
		var err error
		withFile := testExpense(env.tenantA.ID, "2026-11-18")
		withFile.FileID = &env.fileA
		if draft, err = repo.Create(ctx, withFile); err != nil {
			return err
		}
		if draft.State != storage.BookingStateDraft || draft.FileID == nil || *draft.FileID != env.fileA {
			t.Errorf("Create() = %+v", draft)
		}
		if booked, err = repo.Create(ctx, testExpense(env.tenantA.ID, "2026-11-18")); err != nil {
			return err
		}
		booked, err = repo.Book(ctx, booked.ID, time.Now())

		return err
	})

	t.Run("another tenant can neither read nor write", func(t *testing.T) {
		env.inTenant(ctx, t, env.tenantB.ID, func(ctx context.Context) error {
			if _, err := repo.FindByID(ctx, draft.ID); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("FindByID() = %v, want ErrNotFound", err)
			}
			list, err := repo.List(ctx, storage.ExpenseFilter{})
			if err != nil {
				return err
			}
			if len(list) != 0 {
				t.Errorf("List() under tenant B = %v, want none", list)
			}
			changed := draft
			changed.Description = "tampered"
			if _, err := repo.UpdateDraft(ctx, changed); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("UpdateDraft() = %v, want ErrNotFound", err)
			}
			if err := repo.DeleteDraft(ctx, draft.ID); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("DeleteDraft() = %v, want ErrNotFound", err)
			}
			if _, err := repo.Book(ctx, draft.ID, time.Now()); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("Book() = %v, want ErrNotFound", err)
			}
			if _, err := repo.Storno(ctx, booked.ID, *expenseDate("2026-11-25"), time.Now()); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("Storno() = %v, want ErrNotFound", err)
			}

			return nil
		})

		// Raw SQL under tenant B's scope, without a tenant filter: row
		// level security leaves nothing to touch.
		tx := beginScoped(ctx, t, env.restrictedPool, env.tenantB.ID)
		defer func() { _ = tx.Rollback(ctx) }()
		var visible int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM expenses`).Scan(&visible); err != nil {
			t.Fatal(err)
		}
		if visible != 0 {
			t.Errorf("tenant B sees %d expenses, want 0", visible)
		}
		tag, err := tx.Exec(ctx, `UPDATE expenses SET description = 'tampered' WHERE id = $1`, draft.ID)
		if err != nil {
			t.Fatal(err)
		}
		if tag.RowsAffected() != 0 {
			t.Errorf("tenant B updated %d rows of tenant A", tag.RowsAffected())
		}
	})

	t.Run("a row of another tenant cannot be written from this scope", func(t *testing.T) {
		err := env.store.WithTenant(ctx, env.tenantA.ID, func(ctx context.Context) error {
			_, err := repo.Create(ctx, testExpense(env.tenantB.ID, "2026-11-18"))

			return err
		})
		if err == nil {
			t.Error("Create() of a tenant B row under tenant A succeeded")
		}
	})

	t.Run("a receipt of another tenant is refused", func(t *testing.T) {
		err := env.store.WithTenant(ctx, env.tenantA.ID, func(ctx context.Context) error {
			foreign := testExpense(env.tenantA.ID, "2026-11-18")
			foreign.FileID = &env.fileB
			_, err := repo.Create(ctx, foreign)

			return err
		})
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
			t.Errorf("Create() with tenant B's file = %v, want a foreign key violation", err)
		}
	})

	t.Run("the table checks the amounts and the category shape", func(t *testing.T) {
		for name, mutate := range map[string]func(e *storage.Expense){
			"parts do not add up": func(e *storage.Expense) { e.VATCents = 1800 },
			"zero amount":         func(e *storage.Expense) { e.GrossCents, e.NetCents, e.VATCents = 0, 0, 0 },
			"negative amount":     func(e *storage.Expense) { e.GrossCents, e.NetCents, e.VATCents = -11900, -10000, -1900 },
			"form number":         func(e *storage.Expense) { e.Category = "27" },
			"rate above 100":      func(e *storage.Expense) { e.VATRatePermyriad = 10001 },
		} {
			err := env.store.WithTenant(ctx, env.tenantA.ID, func(ctx context.Context) error {
				e := testExpense(env.tenantA.ID, "2026-11-18")
				mutate(&e)
				_, err := repo.Create(ctx, e)

				return err
			})
			if err == nil {
				t.Errorf("%s: Create() succeeded", name)
			}
		}
	})

	t.Run("a booked row is immutable", func(t *testing.T) {
		env.inTenant(ctx, t, env.tenantA.ID, func(ctx context.Context) error {
			changed := booked
			changed.Description = "tampered"
			if _, err := repo.UpdateDraft(ctx, changed); !errors.Is(err, storage.ErrConflict) {
				t.Errorf("UpdateDraft() of a booked row = %v, want ErrConflict", err)
			}
			if err := repo.DeleteDraft(ctx, booked.ID); !errors.Is(err, storage.ErrConflict) {
				t.Errorf("DeleteDraft() of a booked row = %v, want ErrConflict", err)
			}
			if _, err := repo.Book(ctx, booked.ID, time.Now()); !errors.Is(err, storage.ErrConflict) {
				t.Errorf("Book() of a booked row = %v, want ErrConflict", err)
			}

			return nil
		})

		for _, stmt := range []string{
			`UPDATE expenses SET description = 'tampered' WHERE id = $1`,
			`UPDATE expenses SET state = 'draft', booked_at = NULL WHERE id = $1`,
			`DELETE FROM expenses WHERE id = $1`,
		} {
			tx := beginScoped(ctx, t, env.restrictedPool, env.tenantA.ID)
			_, err := tx.Exec(ctx, stmt, booked.ID)
			_ = tx.Rollback(ctx)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != pgIntegrityConstraintViolationClass {
				t.Errorf("%s = %v, want the trigger's integrity_constraint_violation", stmt, err)
			}
		}
	})

	t.Run("a draft can change and be deleted", func(t *testing.T) {
		env.inTenant(ctx, t, env.tenantA.ID, func(ctx context.Context) error {
			changed := draft
			changed.Description = "Toner"
			got, err := repo.UpdateDraft(ctx, changed)
			if err != nil {
				return err
			}
			if got.Description != "Toner" {
				t.Errorf("UpdateDraft() = %+v", got)
			}
			extra, err := repo.Create(ctx, testExpense(env.tenantA.ID, "2026-11-18"))
			if err != nil {
				return err
			}
			if err := repo.DeleteDraft(ctx, extra.ID); err != nil {
				return err
			}
			if _, err := repo.FindByID(ctx, extra.ID); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("FindByID() after DeleteDraft() = %v, want ErrNotFound", err)
			}

			return nil
		})
	})

	t.Run("storno", func(t *testing.T) {
		env.inTenant(ctx, t, env.tenantA.ID, func(ctx context.Context) error {
			if _, err := repo.Storno(ctx, draft.ID, *expenseDate("2026-11-25"), time.Now()); !errors.Is(err, storage.ErrConflict) {
				t.Errorf("Storno() of a draft = %v, want ErrConflict", err)
			}
			storno, err := repo.Storno(ctx, booked.ID, *expenseDate("2027-01-02"), time.Now())
			if err != nil {
				return err
			}
			if storno.State != storage.BookingStateBooked || storno.StornoOfID == nil || *storno.StornoOfID != booked.ID ||
				storno.GrossCents != -11900 || storno.NetCents != -10000 || storno.VATCents != -1900 ||
				storno.PaidOn.Format(time.DateOnly) != "2027-01-02" {
				t.Errorf("Storno() = %+v", storno)
			}
			if _, err := repo.Storno(ctx, booked.ID, *expenseDate("2027-01-03"), time.Now()); !errors.Is(err, storage.ErrConflict) {
				t.Errorf("second Storno() = %v, want ErrConflict", err)
			}
			if _, err := repo.Storno(ctx, storno.ID, *expenseDate("2027-01-03"), time.Now()); !errors.Is(err, storage.ErrConflict) {
				t.Errorf("Storno() of a storno = %v, want ErrConflict", err)
			}

			return nil
		})

		// A raw storno row that does not mirror its original is refused by
		// expenses_check_storno.
		tx := beginScoped(ctx, t, env.restrictedPool, env.tenantA.ID)
		defer func() { _ = tx.Rollback(ctx) }()
		var other storage.Expense
		if err := env.store.WithTenant(ctx, env.tenantA.ID, func(ctx context.Context) error {
			var err error
			if other, err = repo.Create(ctx, testExpense(env.tenantA.ID, "2026-11-18")); err != nil {
				return err
			}
			other, err = repo.Book(ctx, other.ID, time.Now())

			return err
		}); err != nil {
			t.Fatal(err)
		}
		_, err := tx.Exec(ctx, `INSERT INTO expenses (tenant_id, state, paid_on, gross_cents, net_cents, vat_rate_permyriad,
			vat_cents, category, storno_of_id, booked_at)
			VALUES ($1, 'booked', '2026-11-30', -100, -100, 0, 0, 'euer.z51', $2, now())`, env.tenantA.ID, other.ID)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != pgIntegrityConstraintViolationClass {
			t.Errorf("raw storno with other amounts = %v, want the trigger's integrity_constraint_violation", err)
		}
	})

	t.Run("the year comes from paid_on", func(t *testing.T) {
		tenant, err := env.store.Tenants().Create(ctx, storage.Tenant{Slug: "exp-y-" + uuid.NewString(), Name: "Years"})
		if err != nil {
			t.Fatal(err)
		}
		env.inTenant(ctx, t, tenant.ID, func(ctx context.Context) error {
			dec31, err := repo.Create(ctx, testExpense(tenant.ID, "2026-12-31"))
			if err != nil {
				return err
			}
			jan1, err := repo.Create(ctx, testExpense(tenant.ID, "2027-01-01"))
			if err != nil {
				return err
			}
			rent := testExpense(tenant.ID, "2026-01-01")
			rent.Category = category.Rent
			jan1of2026, err := repo.Create(ctx, rent)
			if err != nil {
				return err
			}
			undated := testExpense(tenant.ID, "2026-01-01")
			undated.PaidOn = nil
			if _, err := repo.Create(ctx, undated); err != nil {
				return err
			}

			year := 2026
			got, err := repo.List(ctx, storage.ExpenseFilter{Year: &year})
			if err != nil {
				return err
			}
			if len(got) != 2 || got[0].ID != jan1of2026.ID || got[1].ID != dec31.ID {
				t.Errorf("List(2026) = %v, want 1 January 2026 and 31 December 2026 in order", got)
			}
			year = 2027
			got, err = repo.List(ctx, storage.ExpenseFilter{Year: &year})
			if err != nil {
				return err
			}
			if len(got) != 1 || got[0].ID != jan1.ID {
				t.Errorf("List(2027) = %v, want only 1 January 2027", got)
			}
			key := category.Rent
			got, err = repo.List(ctx, storage.ExpenseFilter{Category: &key})
			if err != nil {
				return err
			}
			if len(got) != 1 || got[0].ID != jan1of2026.ID {
				t.Errorf("List(rent) = %v", got)
			}
			all, err := repo.List(ctx, storage.ExpenseFilter{})
			if err != nil {
				return err
			}
			if len(all) != 4 || all[3].PaidOn != nil {
				t.Errorf("List() = %v, want four rows, the undated draft last", all)
			}

			return nil
		})
	})
}

// TestOtherIncomeRepository covers other_income the same way, shorter:
// isolation, immutability and the storno.
func TestOtherIncomeRepository(t *testing.T) {
	ctx := context.Background()
	env := newExpenseTestEnv(ctx, t)
	repo := env.store.OtherIncome()

	refund := func(tenantID uuid.UUID, receivedOn string) storage.OtherIncome {
		return storage.OtherIncome{
			TenantID:   tenantID,
			ReceivedOn: expenseDate(receivedOn),
			GrossCents: 12345,
			NetCents:   12345,
			Category:   category.VATRefunded,
		}
	}

	var booked storage.OtherIncome
	env.inTenant(ctx, t, env.tenantA.ID, func(ctx context.Context) error {
		created, err := repo.Create(ctx, refund(env.tenantA.ID, "2026-12-31"))
		if err != nil {
			return err
		}
		booked, err = repo.Book(ctx, created.ID, time.Now())

		return err
	})

	env.inTenant(ctx, t, env.tenantB.ID, func(ctx context.Context) error {
		if _, err := repo.FindByID(ctx, booked.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("FindByID() under tenant B = %v, want ErrNotFound", err)
		}
		if _, err := repo.Storno(ctx, booked.ID, *expenseDate("2027-01-02"), time.Now()); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("Storno() under tenant B = %v, want ErrNotFound", err)
		}
		foreign := refund(env.tenantB.ID, "2026-12-31")
		foreign.FileID = &env.fileA
		if _, err := repo.Create(ctx, foreign); err == nil {
			t.Error("Create() with tenant A's file under tenant B succeeded")
		}

		return nil
	})

	env.inTenant(ctx, t, env.tenantA.ID, func(ctx context.Context) error {
		if err := repo.DeleteDraft(ctx, booked.ID); !errors.Is(err, storage.ErrConflict) {
			t.Errorf("DeleteDraft() of a booked row = %v, want ErrConflict", err)
		}
		storno, err := repo.Storno(ctx, booked.ID, *expenseDate("2027-01-02"), time.Now())
		if err != nil {
			return err
		}
		if storno.GrossCents != -12345 || storno.StornoOfID == nil || *storno.StornoOfID != booked.ID {
			t.Errorf("Storno() = %+v", storno)
		}
		year := 2027
		list, err := repo.List(ctx, storage.OtherIncomeFilter{Year: &year})
		if err != nil {
			return err
		}
		if len(list) != 1 || list[0].ID != storno.ID {
			t.Errorf("List(2027) = %v, want only the storno", list)
		}

		return nil
	})

	tx := beginScoped(ctx, t, env.restrictedPool, env.tenantA.ID)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err := tx.Exec(ctx, `UPDATE other_income SET gross_cents = 1, net_cents = 1 WHERE id = $1`, booked.ID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgIntegrityConstraintViolationClass {
		t.Errorf("raw UPDATE of booked other income = %v, want the trigger's integrity_constraint_violation", err)
	}
}
