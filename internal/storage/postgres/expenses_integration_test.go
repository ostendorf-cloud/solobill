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

// SQLSTATEs the expense tests expect from Postgres. A row level security
// WITH CHECK refusal reports insufficient_privilege.
const (
	sqlStateRLSRefused          = sqlStateInsufficientPrivilege
	sqlStateCheckViolation      = "23514"
	sqlStateForeignKeyViolation = "23503"
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

// scopeErr runs fn in a WithTenant scope of its own and returns its
// error. A test that expects a refusal uses it, so the transaction the
// refusal aborts is never used again.
func (env expenseTestEnv) scopeErr(ctx context.Context, tenantID uuid.UUID, fn func(ctx context.Context) error) error {
	return env.store.WithTenant(ctx, tenantID, fn)
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
			list, err := repo.List(ctx, storage.ExpenseFilter{})
			if err != nil {
				return err
			}
			if len(list) != 0 {
				t.Errorf("List() under tenant B = %v, want none", list)
			}

			return nil
		})

		// Each refused call in its own scope: none of them should raise a
		// database error, but a scope is never reused after a refusal.
		changed := draft
		changed.Description = "tampered"
		for name, call := range map[string]func(ctx context.Context) error{
			"FindByID": func(ctx context.Context) error {
				_, err := repo.FindByID(ctx, draft.ID)

				return err
			},
			"UpdateDraft": func(ctx context.Context) error {
				_, err := repo.UpdateDraft(ctx, changed)

				return err
			},
			"DeleteDraft": func(ctx context.Context) error {
				return repo.DeleteDraft(ctx, draft.ID)
			},
			"Book": func(ctx context.Context) error {
				_, err := repo.Book(ctx, draft.ID, time.Now())

				return err
			},
			"Storno": func(ctx context.Context) error {
				_, err := repo.Storno(ctx, booked.ID, *expenseDate("2026-11-25"), time.Now())

				return err
			},
		} {
			if err := env.scopeErr(ctx, env.tenantB.ID, call); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("%s() under tenant B = %v, want ErrNotFound", name, err)
			}
		}

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
		err := env.scopeErr(ctx, env.tenantA.ID, func(ctx context.Context) error {
			_, err := repo.Create(ctx, testExpense(env.tenantB.ID, "2026-11-18"))

			return err
		})
		requireSQLState(t, err, sqlStateRLSRefused, "Create() of a tenant B row under tenant A")
	})

	t.Run("a receipt of another tenant is refused", func(t *testing.T) {
		err := env.scopeErr(ctx, env.tenantA.ID, func(ctx context.Context) error {
			foreign := testExpense(env.tenantA.ID, "2026-11-18")
			foreign.FileID = &env.fileB
			_, err := repo.Create(ctx, foreign)

			return err
		})
		requireSQLState(t, err, sqlStateForeignKeyViolation, "Create() with tenant B's file")
	})

	t.Run("the table checks the amounts and the category shape", func(t *testing.T) {
		for name, mutate := range map[string]func(e *storage.Expense){
			"parts do not add up": func(e *storage.Expense) { e.VATCents = 1800 },
			"zero amount":         func(e *storage.Expense) { e.GrossCents, e.NetCents, e.VATCents = 0, 0, 0 },
			"negative amount":     func(e *storage.Expense) { e.GrossCents, e.NetCents, e.VATCents = -11900, -10000, -1900 },
			"form number":         func(e *storage.Expense) { e.Category = "27" },
			"rate above 100":      func(e *storage.Expense) { e.VATRatePermyriad = 10001 },
		} {
			err := env.scopeErr(ctx, env.tenantA.ID, func(ctx context.Context) error {
				e := testExpense(env.tenantA.ID, "2026-11-18")
				mutate(&e)
				_, err := repo.Create(ctx, e)

				return err
			})
			requireSQLState(t, err, sqlStateCheckViolation, name)
		}
	})

	t.Run("a booked row is immutable", func(t *testing.T) {
		// Each refusal runs in its own scope that returns the error: a
		// statement Postgres refuses aborts the whole transaction, so a
		// later statement in the same scope would fail with 25P02.
		changed := booked
		changed.Description = "tampered"
		err := env.scopeErr(ctx, env.tenantA.ID, func(ctx context.Context) error {
			_, err := repo.UpdateDraft(ctx, changed)

			return err
		})
		if !errors.Is(err, storage.ErrConflict) {
			t.Errorf("UpdateDraft() of a booked row = %v, want ErrConflict", err)
		}
		err = env.scopeErr(ctx, env.tenantA.ID, func(ctx context.Context) error {
			return repo.DeleteDraft(ctx, booked.ID)
		})
		if !errors.Is(err, storage.ErrConflict) {
			t.Errorf("DeleteDraft() of a booked row = %v, want ErrConflict", err)
		}
		err = env.scopeErr(ctx, env.tenantA.ID, func(ctx context.Context) error {
			_, err := repo.Book(ctx, booked.ID, time.Now())

			return err
		})
		if !errors.Is(err, storage.ErrConflict) {
			t.Errorf("Book() of a booked row = %v, want ErrConflict", err)
		}

		for _, stmt := range []string{
			`UPDATE expenses SET description = 'tampered' WHERE id = $1`,
			`UPDATE expenses SET state = 'draft', booked_at = NULL WHERE id = $1`,
			`DELETE FROM expenses WHERE id = $1`,
		} {
			tx := beginScoped(ctx, t, env.restrictedPool, env.tenantA.ID)
			_, err := tx.Exec(ctx, stmt, booked.ID)
			_ = tx.Rollback(ctx)
			requireSQLState(t, err, pgIntegrityConstraintViolationClass, stmt)
		}

		// The row is unchanged, read in a fresh scope.
		env.inTenant(ctx, t, env.tenantA.ID, func(ctx context.Context) error {
			got, err := repo.FindByID(ctx, booked.ID)
			if err != nil {
				return err
			}
			if got.State != storage.BookingStateBooked || got.Description != booked.Description {
				t.Errorf("booked row after the refusals = %+v", got)
			}

			return nil
		})
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
		// Every refusal runs in its own scope; see "a booked row is
		// immutable". The second storno of the same original fails on
		// expenses_reversed_once, a unique violation that aborts its
		// transaction.
		err := env.scopeErr(ctx, env.tenantA.ID, func(ctx context.Context) error {
			_, err := repo.Storno(ctx, draft.ID, *expenseDate("2026-11-25"), time.Now())

			return err
		})
		if !errors.Is(err, storage.ErrConflict) {
			t.Errorf("Storno() of a draft = %v, want ErrConflict", err)
		}

		var storno storage.Expense
		env.inTenant(ctx, t, env.tenantA.ID, func(ctx context.Context) error {
			var err error
			storno, err = repo.Storno(ctx, booked.ID, *expenseDate("2027-01-02"), time.Now())

			return err
		})
		if storno.State != storage.BookingStateBooked || storno.StornoOfID == nil || *storno.StornoOfID != booked.ID ||
			storno.GrossCents != -11900 || storno.NetCents != -10000 || storno.VATCents != -1900 ||
			storno.PaidOn.Format(time.DateOnly) != "2027-01-02" {
			t.Errorf("Storno() = %+v", storno)
		}

		err = env.scopeErr(ctx, env.tenantA.ID, func(ctx context.Context) error {
			_, err := repo.Storno(ctx, booked.ID, *expenseDate("2027-01-03"), time.Now())

			return err
		})
		if !errors.Is(err, storage.ErrConflict) {
			t.Errorf("second Storno() = %v, want ErrConflict", err)
		}
		err = env.scopeErr(ctx, env.tenantA.ID, func(ctx context.Context) error {
			_, err := repo.Storno(ctx, storno.ID, *expenseDate("2027-01-03"), time.Now())

			return err
		})
		if !errors.Is(err, storage.ErrConflict) {
			t.Errorf("Storno() of a storno = %v, want ErrConflict", err)
		}

		// The refused attempts left exactly one storno of the original.
		env.inTenant(ctx, t, env.tenantA.ID, func(ctx context.Context) error {
			all, err := repo.List(ctx, storage.ExpenseFilter{})
			if err != nil {
				return err
			}
			reversals := 0
			for _, e := range all {
				if e.StornoOfID != nil && *e.StornoOfID == booked.ID {
					reversals++
				}
			}
			if reversals != 1 {
				t.Errorf("%d storno rows of the original, want 1", reversals)
			}

			return nil
		})

		// A raw storno row that does not mirror its original is refused by
		// expenses_check_storno, in a transaction of its own.
		var other storage.Expense
		env.inTenant(ctx, t, env.tenantA.ID, func(ctx context.Context) error {
			var err error
			if other, err = repo.Create(ctx, testExpense(env.tenantA.ID, "2026-11-18")); err != nil {
				return err
			}
			other, err = repo.Book(ctx, other.ID, time.Now())

			return err
		})
		tx := beginScoped(ctx, t, env.restrictedPool, env.tenantA.ID)
		_, err = tx.Exec(ctx, `INSERT INTO expenses (tenant_id, state, paid_on, gross_cents, net_cents, vat_rate_permyriad,
			vat_cents, category, storno_of_id, booked_at)
			VALUES ($1, 'booked', '2026-11-30', -100, -100, 0, 0, 'euer.z51', $2, now())`, env.tenantA.ID, other.ID)
		_ = tx.Rollback(ctx)
		requireSQLState(t, err, pgIntegrityConstraintViolationClass, "raw storno with other amounts")
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

// TestExpenseTablesRefuseTruncate: TRUNCATE skips row triggers, so the
// BEFORE TRUNCATE triggers of expenses and other_income must refuse it,
// even for the admin role. Each statement runs in its own transaction
// that is rolled back, so a missing refusal empties nothing and an aborted
// transaction is never reused.
//
// A plain TRUNCATE may also be refused by Postgres itself (0A000) once a
// later table references one of them by foreign key (assets, issue #78).
// The self reference of storno_of_id does not count: it stays inside the
// truncated table. TRUNCATE ... CASCADE always reaches the named table's
// trigger first, which refuses with integrity_constraint_violation.
func TestExpenseTablesRefuseTruncate(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed truncate test")
	}

	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	truncateErr := func(stmt string) error {
		t.Helper()

		tx, err := adminPool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, stmt)

		return err
	}

	for _, table := range []string{"expenses", "other_income"} {
		err := truncateErr(`TRUNCATE ` + table)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || (pgErr.Code != "0A000" && pgErr.Code != pgIntegrityConstraintViolationClass) {
			t.Errorf("TRUNCATE %s as the admin role error = %v, want a refusal (0A000 or 23000)", table, err)
		}

		err = truncateErr(`TRUNCATE ` + table + ` CASCADE`)
		requireSQLState(t, err, pgIntegrityConstraintViolationClass, "TRUNCATE "+table+" CASCADE as the admin role")
	}
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

	// Every refusal runs in its own scope that returns the error; see
	// TestExpenseRepository.
	err := env.scopeErr(ctx, env.tenantB.ID, func(ctx context.Context) error {
		_, err := repo.FindByID(ctx, booked.ID)

		return err
	})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("FindByID() under tenant B = %v, want ErrNotFound", err)
	}
	err = env.scopeErr(ctx, env.tenantB.ID, func(ctx context.Context) error {
		_, err := repo.Storno(ctx, booked.ID, *expenseDate("2027-01-02"), time.Now())

		return err
	})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Storno() under tenant B = %v, want ErrNotFound", err)
	}
	err = env.scopeErr(ctx, env.tenantB.ID, func(ctx context.Context) error {
		foreign := refund(env.tenantB.ID, "2026-12-31")
		foreign.FileID = &env.fileA
		_, err := repo.Create(ctx, foreign)

		return err
	})
	requireSQLState(t, err, sqlStateForeignKeyViolation, "Create() with tenant A's file under tenant B")

	err = env.scopeErr(ctx, env.tenantA.ID, func(ctx context.Context) error {
		return repo.DeleteDraft(ctx, booked.ID)
	})
	if !errors.Is(err, storage.ErrConflict) {
		t.Errorf("DeleteDraft() of a booked row = %v, want ErrConflict", err)
	}

	var storno storage.OtherIncome
	env.inTenant(ctx, t, env.tenantA.ID, func(ctx context.Context) error {
		var err error
		storno, err = repo.Storno(ctx, booked.ID, *expenseDate("2027-01-02"), time.Now())

		return err
	})
	if storno.GrossCents != -12345 || storno.StornoOfID == nil || *storno.StornoOfID != booked.ID {
		t.Errorf("Storno() = %+v", storno)
	}

	err = env.scopeErr(ctx, env.tenantA.ID, func(ctx context.Context) error {
		_, err := repo.Storno(ctx, booked.ID, *expenseDate("2027-01-03"), time.Now())

		return err
	})
	if !errors.Is(err, storage.ErrConflict) {
		t.Errorf("second Storno() = %v, want ErrConflict", err)
	}

	env.inTenant(ctx, t, env.tenantA.ID, func(ctx context.Context) error {
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
	_, err = tx.Exec(ctx, `UPDATE other_income SET gross_cents = 1, net_cents = 1 WHERE id = $1`, booked.ID)
	_ = tx.Rollback(ctx)
	requireSQLState(t, err, pgIntegrityConstraintViolationClass, "raw UPDATE of booked other income")
}
