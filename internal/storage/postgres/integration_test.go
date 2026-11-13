//go:build integration

// This file needs a real, reachable Postgres and is excluded from a plain
// "go test ./..." by the build tag above; "task test:integration" is the
// only Taskfile target that passes "-tags=integration". DATABASE_URL still
// gets checked at runtime too, and t.Skip()s without it, so running this
// binary by hand with the tag but no database gives a clear skip instead
// of a confusing dial failure.
package postgres_test

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres"
)

// testAppRole is a role this test creates on the target Postgres if it is
// not already there, and then runs every assertion below through. It is
// deliberately NOSUPERUSER and NOBYPASSRLS: a Postgres superuser bypasses
// row level security regardless of FORCE ROW LEVEL SECURITY, so running
// these assertions as the admin connection from DATABASE_URL (ordinarily
// the postgres image's default superuser, which CI also uses to run the
// migrations beforehand) would prove nothing about row level security at
// all. testAppPassword is a fixed, obviously-fake value that only ever
// reaches a disposable test Postgres (a CI service container, or a
// developer's own local one); it is not a credential for anything real.
const (
	testAppRole     = "solobill_test_app"
	testAppPassword = "solobill-test-only-not-a-real-secret"
)

// accountRawRow mirrors the accounts table's own column order and names,
// used only to decode the literal, deliberately unscoped "SELECT * FROM
// accounts" below: that query has to stay exactly that, with no column
// list and no WHERE clause, to prove what it is meant to prove, so the
// test needs a destination shaped like the whole table rather than like
// one of its own queries.
type accountRawRow struct {
	ID           uuid.UUID `db:"id"`
	TenantID     uuid.UUID `db:"tenant_id"`
	Email        string    `db:"email"`
	PasswordHash *string   `db:"password_hash"`
	TOTPSecret   *string   `db:"totp_secret"`
	Role         string    `db:"role"`
	Status       string    `db:"status"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
}

// TestRowLevelSecurity is the single most important test in this
// milestone: it proves that Postgres row level security, not application
// code, is what actually keeps one tenant's rows out of another tenant's
// reach, including when a query does not even try to scope itself.
func TestRowLevelSecurity(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed row level security test")
	}

	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	restrictedURL := setUpTestAppRole(ctx, t, adminPool, adminURL)

	restrictedPool, err := postgres.NewPool(ctx, restrictedURL)
	if err != nil {
		t.Fatalf("open restricted pool as %s: %v", testAppRole, err)
	}
	t.Cleanup(restrictedPool.Close)

	store := postgres.NewStore(restrictedPool)

	tenantA, err := store.Tenants().Create(ctx, storage.Tenant{Slug: "tenant-a-" + uuid.NewString(), Name: "Tenant A"})
	if err != nil {
		t.Fatalf("create tenant A: %v", err)
	}

	tenantB, err := store.Tenants().Create(ctx, storage.Tenant{Slug: "tenant-b-" + uuid.NewString(), Name: "Tenant B"})
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	var accountA, accountB storage.Account

	if err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
		var err error
		accountA, err = store.Accounts().Create(ctx, storage.Account{
			TenantID: tenantA.ID,
			Email:    "owner@tenant-a.example",
			Role:     storage.AccountRoleOwner,
			Status:   storage.AccountStatusActive,
		})

		return err
	}); err != nil {
		t.Fatalf("create account under tenant A: %v", err)
	}

	if err := store.WithTenant(ctx, tenantB.ID, func(ctx context.Context) error {
		var err error
		accountB, err = store.Accounts().Create(ctx, storage.Account{
			TenantID: tenantB.ID,
			Email:    "owner@tenant-b.example",
			Role:     storage.AccountRoleOwner,
			Status:   storage.AccountStatusActive,
		})

		return err
	}); err != nil {
		t.Fatalf("create account under tenant B: %v", err)
	}

	t.Run("tenant A sees only its own account through the repository", func(t *testing.T) {
		var accounts []storage.Account

		if err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
			var err error
			accounts, err = store.Accounts().List(ctx)

			return err
		}); err != nil {
			t.Fatalf("list accounts as tenant A: %v", err)
		}

		assertOnlyAccount(t, accounts, accountA.ID)
	})

	t.Run("tenant B sees only its own account through the repository", func(t *testing.T) {
		var accounts []storage.Account

		if err := store.WithTenant(ctx, tenantB.ID, func(ctx context.Context) error {
			var err error
			accounts, err = store.Accounts().List(ctx)

			return err
		}); err != nil {
			t.Fatalf("list accounts as tenant B: %v", err)
		}

		assertOnlyAccount(t, accounts, accountB.ID)
	})

	t.Run("an unscoped SELECT star on a transaction scoped to tenant B still sees only tenant B", func(t *testing.T) {
		tx, err := restrictedPool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantB.ID.String()); err != nil {
			t.Fatalf("set_config: %v", err)
		}

		rows, err := tx.Query(ctx, `SELECT * FROM accounts`)
		if err != nil {
			t.Fatalf("SELECT * FROM accounts: %v", err)
		}

		ids := accountIDs(t, rows)

		if len(ids) != 1 || ids[0] != accountB.ID {
			t.Fatalf("SELECT * FROM accounts while scoped to tenant B = %v, want exactly [%v]", ids, accountB.ID)
		}
	})

	t.Run("no tenant set at all returns zero rows, not every row and not an error", func(t *testing.T) {
		tx, err := restrictedPool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		// Deliberately no set_config call on this transaction at all: this
		// is the "a connection never scoped by WithTenant" case.

		rows, err := tx.Query(ctx, `SELECT * FROM accounts`)
		if err != nil {
			t.Fatalf("SELECT * FROM accounts with no tenant set: %v", err)
		}

		ids := accountIDs(t, rows)

		if len(ids) != 0 {
			t.Fatalf("SELECT * FROM accounts with no tenant set = %v, want none", ids)
		}
	})

	t.Run("a tenant-scoped transaction that commits, then an unscoped transaction on the same connection, still sees zero rows", func(t *testing.T) {
		// This is the case that actually failed against a real Postgres: a
		// physical connection, not a fresh one, held onto across two
		// transactions via a single pool.Acquire, exactly what a connection
		// pool hands an unrelated later request once it gives this
		// connection back. set_config(..., true) only reverts its *value*
		// at COMMIT; the custom parameter it created keeps existing for
		// the rest of the session, as an empty string, not as "unset". So
		// the second, deliberately unscoped transaction below must still
		// see zero rows and no error, not every row and not a cast
		// failure on "".
		conn, err := restrictedPool.Acquire(ctx)
		if err != nil {
			t.Fatalf("acquire connection: %v", err)
		}
		defer conn.Release()

		scoped, err := conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin scoped transaction: %v", err)
		}

		if _, err := scoped.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantB.ID.String()); err != nil {
			t.Fatalf("set_config: %v", err)
		}

		if _, err := scoped.Exec(ctx, `SELECT 1`); err != nil {
			t.Fatalf("exercise the scoped transaction: %v", err)
		}

		if err := scoped.Commit(ctx); err != nil {
			t.Fatalf("commit scoped transaction: %v", err)
		}

		unscoped, err := conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin unscoped transaction on the same connection: %v", err)
		}
		defer func() { _ = unscoped.Rollback(ctx) }()

		// Deliberately no set_config call on this second transaction: the
		// connection already carries app.tenant_id from the one above.

		rows, err := unscoped.Query(ctx, `SELECT * FROM accounts`)
		if err != nil {
			t.Fatalf("SELECT * FROM accounts on a reused connection with no tenant set: %v", err)
		}

		ids := accountIDs(t, rows)

		if len(ids) != 0 {
			t.Fatalf("SELECT * FROM accounts on a reused connection with no tenant set = %v, want none", ids)
		}
	})

	t.Run("the admin connection, a superuser that bypasses row level security, still sees both accounts", func(t *testing.T) {
		rows, err := adminPool.Query(ctx, `SELECT * FROM accounts WHERE id = ANY($1)`, []uuid.UUID{accountA.ID, accountB.ID})
		if err != nil {
			t.Fatalf("admin SELECT: %v", err)
		}

		ids := accountIDs(t, rows)

		if len(ids) != 2 {
			t.Fatalf("admin SELECT found %d of the 2 accounts, want 2: "+
				"either the inserts above did not actually happen, or row level security "+
				"is unexpectedly also filtering a superuser connection", len(ids))
		}
	})
}

// setUpTestAppRole creates testAppRole on the database adminURL points at,
// if it does not exist yet, grants it exactly the privileges the
// application needs on the tenant-scoped tables, and returns a connection
// string for it. Role creation has no "IF NOT EXISTS" in Postgres, so this
// checks pg_roles itself to stay safe to run repeatedly against the same,
// persistent database.
func setUpTestAppRole(ctx context.Context, t *testing.T, adminPool *pgxpool.Pool, adminURL string) string {
	t.Helper()

	createRole := `
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '` + testAppRole + `') THEN
        CREATE ROLE ` + testAppRole + ` LOGIN PASSWORD '` + testAppPassword + `' NOSUPERUSER NOBYPASSRLS;
    END IF;
END
$$;`

	if _, err := adminPool.Exec(ctx, createRole); err != nil {
		t.Fatalf("create %s role: %v", testAppRole, err)
	}

	grant := `
GRANT USAGE ON SCHEMA public TO ` + testAppRole + `;
GRANT SELECT, INSERT, UPDATE, DELETE ON tenants, accounts, invites TO ` + testAppRole + `;`

	if _, err := adminPool.Exec(ctx, grant); err != nil {
		t.Fatalf("grant %s privileges: %v", testAppRole, err)
	}

	restricted, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}

	restricted.User = url.UserPassword(testAppRole, testAppPassword)

	return restricted.String()
}

// accountIDs decodes a literal "SELECT * FROM accounts" result into the
// ids it carries. accountRawRow's "db" tags, not column position, do the
// matching, so the test stays correct even if a later migration adds a
// column to accounts.
func accountIDs(t *testing.T, rows pgx.Rows) []uuid.UUID {
	t.Helper()

	records, err := pgx.CollectRows(rows, pgx.RowToStructByName[accountRawRow])
	if err != nil {
		t.Fatalf("scan accounts: %v", err)
	}

	ids := make([]uuid.UUID, len(records))
	for i, record := range records {
		ids[i] = record.ID
	}

	return ids
}

func assertOnlyAccount(t *testing.T, accounts []storage.Account, wantID uuid.UUID) {
	t.Helper()

	if len(accounts) != 1 {
		t.Fatalf("got %d accounts, want exactly 1", len(accounts))
	}

	if accounts[0].ID != wantID {
		t.Fatalf("got account %v, want %v", accounts[0].ID, wantID)
	}
}
