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
	"errors"
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
	ID              uuid.UUID `db:"id"`
	TenantID        uuid.UUID `db:"tenant_id"`
	Email           string    `db:"email"`
	PasswordHash    *string   `db:"password_hash"`
	TOTPSecret      *string   `db:"totp_secret"`
	TOTPLastCounter *int64    `db:"totp_last_counter"`
	OIDCSubject     *string   `db:"oidc_subject"`
	Role            string    `db:"role"`
	Status          string    `db:"status"`
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
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

	t.Run("an INSERT scoped to tenant B with tenant_id set to tenant A is rejected", func(t *testing.T) {
		// The WITH CHECK half of the policy, exercised on INSERT: a new
		// row whose tenant_id does not match the scope the transaction
		// was set_config'd to must be refused, not silently written
		// under the wrong tenant.
		tx, err := restrictedPool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantB.ID.String()); err != nil {
			t.Fatalf("set_config: %v", err)
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO accounts (tenant_id, email, role) VALUES ($1, $2, 'owner')`,
			tenantA.ID, "cross-tenant-insert@tenant-a.example")
		if err == nil {
			t.Fatal("INSERT with tenant_id = tenant A while scoped to tenant B succeeded, want it rejected by WITH CHECK")
		}
	})

	t.Run("an UPDATE scoped to tenant A moving a row's tenant_id to tenant B either keeps the original tenant or is rejected", func(t *testing.T) {
		// The WITH CHECK half of the policy, exercised on UPDATE this
		// time: the new row version (tenant_id = tenant B) is checked
		// against the scope the transaction is set_config'd to (tenant
		// A), so the row can never actually move to another tenant.
		tx, err := restrictedPool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantA.ID.String()); err != nil {
			t.Fatalf("set_config: %v", err)
		}

		var gotTenantID uuid.UUID

		err = tx.QueryRow(ctx,
			`UPDATE accounts SET tenant_id = $1 WHERE id = $2 RETURNING tenant_id`,
			tenantB.ID, accountA.ID,
		).Scan(&gotTenantID)
		if err != nil {
			// Rejected outright: WITH CHECK doing its job, the
			// preferred and expected outcome.
			return
		}

		if gotTenantID != tenantA.ID {
			t.Fatalf("UPDATE moved account A's tenant_id to %v, want it to stay %v or be rejected entirely", gotTenantID, tenantA.ID)
		}
	})

	t.Run("FindByID for another tenant's account id returns not found, not the real row", func(t *testing.T) {
		// Only List was ever exercised through the repository before
		// this: a point lookup by an id leaked from another tenant (for
		// example a cross-tenant guess, or an id copied from a stale
		// link) must answer not found too, never the real row.
		err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
			_, err := store.Accounts().FindByID(ctx, accountB.ID)

			return err
		})
		if err == nil {
			t.Fatal("FindByID(accountB.ID) while scoped to tenant A succeeded, want storage.ErrNotFound")
		}
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("FindByID(accountB.ID) while scoped to tenant A: err = %v, want errors.Is(err, storage.ErrNotFound)", err)
		}
	})

	t.Run("an invite created under tenant A is invisible while scoped to tenant B", func(t *testing.T) {
		// Every subtest above only ever touches accounts; invites
		// carries the same policy and must be proved too.
		var invite storage.Invite

		if err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
			created, err := store.Invites().Create(ctx, storage.Invite{
				TenantID:  tenantA.ID,
				Email:     "invitee@tenant-a.example",
				Role:      storage.AccountRoleMember,
				TokenHash: "test-token-hash-" + uuid.NewString(),
				ExpiresAt: time.Now().Add(time.Hour),
			})
			if err != nil {
				return err
			}

			invite = created

			return nil
		}); err != nil {
			t.Fatalf("create invite under tenant A: %v", err)
		}

		err := store.WithTenant(ctx, tenantB.ID, func(ctx context.Context) error {
			_, err := store.Invites().FindByID(ctx, invite.ID)

			return err
		})
		if err == nil {
			t.Fatal("FindByID found tenant A's invite while scoped to tenant B, want storage.ErrNotFound")
		}
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("err = %v, want errors.Is(err, storage.ErrNotFound)", err)
		}
	})

	t.Run("the table owner is still scoped by row level security because of FORCE", func(t *testing.T) {
		// Every role exercised above is either DATABASE_URL's own role,
		// a superuser that bypasses row level security unconditionally
		// (FORCE included; see the subtest above), or testAppRole,
		// NOSUPERUSER NOBYPASSRLS but not the accounts table's owner, so
		// ENABLE alone would already keep it scoped regardless of
		// FORCE. Neither actually exercises what FORCE itself adds:
		// binding the table's own OWNER to the policy too (ENABLE alone
		// exempts the owner). This subtest makes testOwnerRole, which
		// is NOSUPERUSER NOBYPASSRLS, own accounts for its duration
		// (restored via t.Cleanup) and proves the policy still applies
		// to it.
		ownerURL := setUpTestOwnerRole(ctx, t, adminPool, adminURL)
		makeTemporaryOwner(ctx, t, adminPool, "accounts", testOwnerRole)

		ownerPool, err := postgres.NewPool(ctx, ownerURL)
		if err != nil {
			t.Fatalf("open pool as %s: %v", testOwnerRole, err)
		}
		defer ownerPool.Close()

		tx, err := ownerPool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantB.ID.String()); err != nil {
			t.Fatalf("set_config: %v", err)
		}

		rows, err := tx.Query(ctx, `SELECT * FROM accounts`)
		if err != nil {
			t.Fatalf("SELECT * FROM accounts as the table owner, scoped to tenant B: %v", err)
		}

		ids := accountIDs(t, rows)

		if len(ids) != 1 || ids[0] != accountB.ID {
			t.Fatalf("SELECT * FROM accounts as the table owner, scoped to tenant B = %v, want exactly [%v]; "+
				"if every row comes back instead, FORCE ROW LEVEL SECURITY is not actually binding the table owner",
				ids, accountB.ID)
		}
	})
}

// testOwnerRole and testOwnerPassword name a second disposable test role,
// alongside testAppRole: NOSUPERUSER NOBYPASSRLS like testAppRole, but
// this one is temporarily made the owner of a table (see
// makeTemporaryOwner), which testAppRole never is. testOwnerPassword is a
// fixed, obviously-fake value, the same way testAppPassword is.
const (
	testOwnerRole     = "solobill_test_owner"
	testOwnerPassword = "solobill-test-only-not-a-real-secret-owner"
)

// setUpTestOwnerRole creates testOwnerRole if it does not exist yet, the
// same idempotent pattern setUpTestAppRole uses, and returns a connection
// string for it. It grants no table privileges: ownership, which
// makeTemporaryOwner confers separately, already carries every privilege
// on the table it owns.
func setUpTestOwnerRole(ctx context.Context, t *testing.T, adminPool *pgxpool.Pool, adminURL string) string {
	t.Helper()

	createRole := `
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '` + testOwnerRole + `') THEN
        CREATE ROLE ` + testOwnerRole + ` LOGIN PASSWORD '` + testOwnerPassword + `' NOSUPERUSER NOBYPASSRLS;
    END IF;
END
$$;`

	if _, err := adminPool.Exec(ctx, createRole); err != nil {
		t.Fatalf("create %s role: %v", testOwnerRole, err)
	}

	ownerURL, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}

	ownerURL.User = url.UserPassword(testOwnerRole, testOwnerPassword)

	return ownerURL.String()
}

// makeTemporaryOwner makes role the owner of tableName for the rest of
// the calling test, restoring whichever role owned it before via
// t.Cleanup. This is the only way to actually exercise FORCE ROW LEVEL
// SECURITY: it is specifically the clause that binds a table's OWNER to
// its own policy, and nothing else this suite connects as is both
// non-superuser and the table's owner at the same time.
func makeTemporaryOwner(ctx context.Context, t *testing.T, adminPool *pgxpool.Pool, tableName, role string) {
	t.Helper()

	var originalOwner string

	if err := adminPool.QueryRow(ctx,
		`SELECT pg_get_userbyid(relowner) FROM pg_class WHERE relname = $1`, tableName,
	).Scan(&originalOwner); err != nil {
		t.Fatalf("read current owner of %s: %v", tableName, err)
	}

	if _, err := adminPool.Exec(ctx, `ALTER TABLE `+tableName+` OWNER TO `+role); err != nil {
		t.Fatalf("make %s own %s: %v", role, tableName, err)
	}

	t.Cleanup(func() {
		if _, err := adminPool.Exec(context.Background(), `ALTER TABLE `+tableName+` OWNER TO `+originalOwner); err != nil {
			t.Errorf("restore %s as owner of %s: %v", originalOwner, tableName, err)
		}
	})
}

// TestEveryTenantScopedTableEnforcesRowLevelSecurity introspects the
// schema itself, rather than naming accounts, invites and tenants by
// hand, so it catches a future table that adds a tenant_id column but
// forgets ENABLE ROW LEVEL SECURITY, FORCE ROW LEVEL SECURITY or
// CREATE POLICY: exactly the three properties the existing, per-table
// tests above assume rather than verify generically. It runs as
// DATABASE_URL's own admin/superuser role: querying pg_class, pg_policies
// and information_schema.columns needs no special privilege, and this
// test is about the schema's shape, not about who can read which row, so
// the restricted role TestRowLevelSecurity's own tests exercise is beside
// the point here.
func TestEveryTenantScopedTableEnforcesRowLevelSecurity(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed row level security introspection test")
	}

	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	rows, err := adminPool.Query(ctx, `
SELECT
    c.relname,
    c.relrowsecurity,
    c.relforcerowsecurity,
    EXISTS (
        SELECT 1 FROM pg_policies p
        WHERE p.schemaname = 'public' AND p.tablename = c.relname
    ) AS has_policy
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public'
  AND c.relkind = 'r'
  AND EXISTS (
      SELECT 1 FROM information_schema.columns col
      WHERE col.table_schema = 'public'
        AND col.table_name = c.relname
        AND col.column_name = 'tenant_id'
  )
ORDER BY c.relname`)
	if err != nil {
		t.Fatalf("introspect tenant-scoped tables: %v", err)
	}

	type tenantScopedTable struct {
		Name        string `db:"relname"`
		RowSecurity bool   `db:"relrowsecurity"`
		ForceRLS    bool   `db:"relforcerowsecurity"`
		HasPolicy   bool   `db:"has_policy"`
	}

	tables, err := pgx.CollectRows(rows, pgx.RowToStructByName[tenantScopedTable])
	if err != nil {
		t.Fatalf("scan tenant-scoped tables: %v", err)
	}

	if len(tables) == 0 {
		t.Fatal("introspection found no table with a tenant_id column at all; " +
			"either the query above is broken, or db/migrations has not actually been applied")
	}

	for _, table := range tables {
		t.Run(table.Name, func(t *testing.T) {
			if !table.RowSecurity {
				t.Errorf("table %q has a tenant_id column but ROW LEVEL SECURITY is not enabled", table.Name)
			}
			if !table.ForceRLS {
				t.Errorf("table %q has a tenant_id column but FORCE ROW LEVEL SECURITY is not set", table.Name)
			}
			if !table.HasPolicy {
				t.Errorf("table %q has a tenant_id column but carries no row level security policy at all", table.Name)
			}
		})
	}
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

	// Granted at the minimum each table actually needs, mirroring
	// docker/postgres/init-app-role.sh's own grant to the compose role:
	// nothing in this codebase deletes a row, so no table gets DELETE,
	// and tenants is never updated (no UpdateTenant method exists), so
	// it gets only SELECT and INSERT.
	grant := `
GRANT USAGE ON SCHEMA public TO ` + testAppRole + `;
GRANT SELECT, INSERT ON tenants TO ` + testAppRole + `;
GRANT SELECT, INSERT, UPDATE ON accounts, invites TO ` + testAppRole + `;`

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
