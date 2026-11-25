//go:build integration

// Issue #80: euer_assignments against a real PostgreSQL, through the
// restricted, NOBYPASSRLS application role, and through the admin role
// where only the triggers stand. Same reasons as integration_test.go's
// own build tag comment.
//
// Every statement that is expected to be refused runs in a scope or a
// transaction of its own, so an aborted transaction is never used again,
// and every refusal is asserted by its SQLSTATE or its storage sentinel.
package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// sqlStateAssignmentBlocked is the class 23000 refusal of the mutation
// triggers.
const sqlStateAssignmentBlocked = "23000"

type assignmentEnv struct {
	expenseTestEnv
	ctx       context.Context
	adminPool *pgxpool.Pool
	// Booked expenses of tenant A: one inside the window, one outside,
	// one with is_asset; a draft inside the window; and a booked one of
	// tenant B inside the window.
	inWindow, outside, asset, draft, otherInWindow, otherTenant uuid.UUID
}

func newAssignmentEnv(t *testing.T) assignmentEnv {
	t.Helper()

	ctx := context.Background()
	env := assignmentEnv{expenseTestEnv: newExpenseTestEnv(ctx, t), ctx: ctx}

	adminPool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)
	env.adminPool = adminPool

	add := func(tenantID uuid.UUID, paidOn string, isAsset, book bool) uuid.UUID {
		var id uuid.UUID
		env.inTenant(ctx, t, tenantID, func(ctx context.Context) error {
			e := testExpense(tenantID, paidOn)
			e.IsAsset = isAsset
			created, err := env.store.Expenses().Create(ctx, e)
			if err != nil {
				return err
			}
			id = created.ID
			if book {
				_, err = env.store.Expenses().Book(ctx, id, time.Date(2027, 1, 12, 9, 0, 0, 0, time.UTC))
			}

			return err
		})

		return id
	}
	env.inWindow = add(env.tenantA.ID, "2026-12-28", false, true)
	env.otherInWindow = add(env.tenantA.ID, "2027-01-03", false, true)
	env.outside = add(env.tenantA.ID, "2026-12-21", false, true)
	env.asset = add(env.tenantA.ID, "2026-12-29", true, true)
	env.draft = add(env.tenantA.ID, "2026-12-30", false, false)
	env.otherTenant = add(env.tenantB.ID, "2026-12-28", false, true)

	return env
}

func (env assignmentEnv) create(tenantID uuid.UUID, a storage.EuerAssignment) (storage.EuerAssignment, error) {
	var created storage.EuerAssignment
	err := env.store.WithTenant(env.ctx, tenantID, func(ctx context.Context) error {
		var err error
		created, err = env.store.EuerAssignments().Create(ctx, a)

		return err
	})

	return created, err
}

func (env assignmentEnv) effective(t *testing.T, tenantID uuid.UUID) []storage.EuerAssignment {
	t.Helper()

	var list []storage.EuerAssignment
	env.inTenant(env.ctx, t, tenantID, func(ctx context.Context) error {
		var err error
		list, err = env.store.EuerAssignments().ListEffective(ctx)

		return err
	})

	return list
}

func expenseAssignment(tenantID, entryID uuid.UUID, year int32, supersedes *uuid.UUID) storage.EuerAssignment {
	return storage.EuerAssignment{
		TenantID: tenantID, EntryKind: storage.EuerEntryExpense, EntryID: entryID, TaxYear: year, SupersedesID: supersedes,
	}
}

// rawErr runs one statement through the restricted role in a transaction
// of its own, scoped to tenantID, and rolls it back.
func (env assignmentEnv) rawErr(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID, sql string, args ...any) error {
	t.Helper()

	tx, err := pool.Begin(env.ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(env.ctx) }()

	if _, err := tx.Exec(env.ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID.String()); err != nil {
		t.Fatalf("scope: %v", err)
	}
	_, err = tx.Exec(env.ctx, sql, args...)

	return err
}

func TestEuerAssignmentRepository(t *testing.T) {
	env := newAssignmentEnv(t)
	a := env.tenantA.ID

	first, err := env.create(a, expenseAssignment(a, env.inWindow, 2027, nil))
	if err != nil {
		t.Fatalf("first assignment: %v", err)
	}
	if first.ID == uuid.Nil || first.TaxYear != 2027 || first.CreatedAt.IsZero() || first.TenantID != a {
		t.Fatalf("first = %+v", first)
	}

	// A second first row of the same entry.
	if _, err := env.create(a, expenseAssignment(a, env.inWindow, 2026, nil)); err != storage.ErrConflict { //nolint:errorlint // the contract is the bare sentinel
		t.Errorf("second first row: err = %v, want storage.ErrConflict", err)
	}

	note := "Miete Dezember"
	changed := expenseAssignment(a, env.inWindow, 2026, &first.ID)
	changed.Note = &note
	second, err := env.create(a, changed)
	if err != nil {
		t.Fatalf("changed assignment: %v", err)
	}

	// The superseded row is superseded once.
	if _, err := env.create(a, expenseAssignment(a, env.inWindow, 2027, &first.ID)); err != storage.ErrConflict { //nolint:errorlint // the contract is the bare sentinel
		t.Errorf("superseding a superseded row: err = %v, want storage.ErrConflict", err)
	}
	// A row of another entry is never the effective row of this one.
	if _, err := env.create(a, expenseAssignment(a, env.otherInWindow, 2026, &second.ID)); err != storage.ErrConflict { //nolint:errorlint // the contract is the bare sentinel
		t.Errorf("superseding another entry's row: err = %v, want storage.ErrConflict", err)
	}

	list := env.effective(t, a)
	if len(list) != 1 || list[0].ID != second.ID || list[0].SupersedesID == nil || *list[0].SupersedesID != first.ID ||
		list[0].Note == nil || *list[0].Note != note || list[0].TaxYear != 2026 {
		t.Fatalf("effective = %+v, want only the changed row", list)
	}

	// Tenant B sees none of it, and cannot name A's entry.
	if list := env.effective(t, env.tenantB.ID); len(list) != 0 {
		t.Errorf("tenant B sees %+v", list)
	}
	_, err = env.create(env.tenantB.ID, expenseAssignment(env.tenantB.ID, env.inWindow, 2026, nil))
	requireSQLState(t, err, sqlStateForeignKeyViolation, "tenant B naming tenant A's expense")
}

// TestEuerAssignmentInsertTrigger proves the table refuses by itself what
// the handler checks first. Each refusal runs in a transaction of its own.
func TestEuerAssignmentInsertTrigger(t *testing.T) {
	env := newAssignmentEnv(t)
	a := env.tenantA.ID
	const insert = `INSERT INTO euer_assignments (tenant_id, entry_kind, entry_id, tax_year) VALUES ($1, $2, $3, $4)`

	tests := []struct {
		name  string
		kind  string
		entry uuid.UUID
		year  int
		want  string
	}{
		{"outside the window", "expense", env.outside, 2026, sqlStateCheckViolation},
		{"a year that is no neighbour", "expense", env.inWindow, 2025, sqlStateCheckViolation},
		{"an asset expense", "expense", env.asset, 2026, sqlStateForeignKeyViolation},
		{"a draft", "expense", env.draft, 2026, sqlStateForeignKeyViolation},
		{"another tenant's expense", "expense", env.otherTenant, 2026, sqlStateForeignKeyViolation},
		{"a missing entry", "expense", uuid.New(), 2026, sqlStateForeignKeyViolation},
		{"the wrong kind", "payment", env.inWindow, 2026, sqlStateForeignKeyViolation},
		{"an unknown kind", "invoice", env.inWindow, 2026, sqlStateForeignKeyViolation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireSQLState(t, env.rawErr(t, env.restrictedPool, a, insert, a, tt.kind, tt.entry, tt.year), tt.want, tt.name)
		})
	}

	t.Run("the year of the date and the next one pass", func(t *testing.T) {
		for _, year := range []int{2026, 2027} {
			if err := env.rawErr(t, env.restrictedPool, a, insert, a, "expense", env.inWindow, year); err != nil {
				t.Errorf("%d: %v", year, err)
			}
		}
	})

	t.Run("a January date and the year before pass", func(t *testing.T) {
		if err := env.rawErr(t, env.restrictedPool, a, insert, a, "expense", env.otherInWindow, 2026); err != nil {
			t.Error(err)
		}
	})

	t.Run("a second first row of one entry", func(t *testing.T) {
		tx, err := env.restrictedPool.Begin(env.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(env.ctx) }()
		if _, err := tx.Exec(env.ctx, `SELECT set_config('app.tenant_id', $1, true)`, a.String()); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(env.ctx, insert, a, "expense", env.inWindow, 2026); err != nil {
			t.Fatalf("first row: %v", err)
		}
		_, err = tx.Exec(env.ctx, insert, a, "expense", env.inWindow, 2027)
		requireSQLState(t, err, sqlStateUniqueViolation, "second first row")
	})

	t.Run("superseding a row of another entry", func(t *testing.T) {
		tx, err := env.restrictedPool.Begin(env.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(env.ctx) }()
		if _, err := tx.Exec(env.ctx, `SELECT set_config('app.tenant_id', $1, true)`, a.String()); err != nil {
			t.Fatal(err)
		}
		row := uuid.New()
		if _, err := tx.Exec(env.ctx, `INSERT INTO euer_assignments (id, tenant_id, entry_kind, entry_id, tax_year) VALUES ($1, $2, 'expense', $3, 2026)`,
			row, a, env.inWindow); err != nil {
			t.Fatalf("first row: %v", err)
		}
		_, err = tx.Exec(env.ctx, `INSERT INTO euer_assignments (tenant_id, entry_kind, entry_id, tax_year, supersedes_id) VALUES ($1, 'expense', $2, 2026, $3)`,
			a, env.otherInWindow, row)
		requireSQLState(t, err, sqlStateForeignKeyViolation, "a row superseding another entry's row")
	})
}

// TestEuerAssignmentsAreAppendOnly: the grant refuses UPDATE, DELETE and
// TRUNCATE to the application role, and the triggers refuse them to the
// admin role.
func TestEuerAssignmentsAreAppendOnly(t *testing.T) {
	env := newAssignmentEnv(t)
	a := env.tenantA.ID

	row, err := env.create(a, expenseAssignment(a, env.inWindow, 2026, nil))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	statements := map[string]string{
		"update":   `UPDATE euer_assignments SET tax_year = 2027 WHERE id = $1`,
		"delete":   `DELETE FROM euer_assignments WHERE id = $1`,
		"truncate": `TRUNCATE euer_assignments`,
	}
	for name, sql := range statements {
		args := []any{row.ID}
		if name == "truncate" {
			args = nil
		}
		t.Run("app role "+name, func(t *testing.T) {
			requireSQLState(t, env.rawErr(t, env.restrictedPool, a, sql, args...), sqlStateInsufficientPrivilege, name)
		})
		t.Run("admin role "+name, func(t *testing.T) {
			requireSQLState(t, env.rawErr(t, env.adminPool, a, sql, args...), sqlStateAssignmentBlocked, name)
		})
	}

	if list := env.effective(t, a); len(list) != 1 || list[0].ID != row.ID || list[0].TaxYear != 2026 {
		t.Errorf("after the refusals = %+v, want the row unchanged", list)
	}
}

// TestEuerAssignmentRowLevelSecurity: a row written under tenant A is
// invisible to tenant B's scope, also for a raw SELECT.
func TestEuerAssignmentRowLevelSecurity(t *testing.T) {
	env := newAssignmentEnv(t)
	a := env.tenantA.ID

	row, err := env.create(a, expenseAssignment(a, env.inWindow, 2026, nil))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	tx, err := env.restrictedPool.Begin(env.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(env.ctx) }()
	if _, err := tx.Exec(env.ctx, `SELECT set_config('app.tenant_id', $1, true)`, env.tenantB.ID.String()); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := tx.QueryRow(env.ctx, `SELECT count(*) FROM euer_assignments WHERE id = $1`, row.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("tenant B sees %d rows of tenant A", n)
	}
}
