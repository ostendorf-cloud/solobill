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
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// customerRawRow mirrors the customers table's own column order and
// names, used only to decode the literal, deliberately unscoped "SELECT
// * FROM customers" below, the same reason accountRawRow exists for
// accounts: that query has to stay exactly that, with no column list
// and no WHERE clause, to prove what it is meant to prove.
type customerRawRow struct {
	ID          uuid.UUID  `db:"id"`
	TenantID    uuid.UUID  `db:"tenant_id"`
	Name        string     `db:"name"`
	Email       *string    `db:"email"`
	VATID       *string    `db:"vat_id"`
	Street      *string    `db:"street"`
	PostalCode  *string    `db:"postal_code"`
	City        *string    `db:"city"`
	CountryCode *string    `db:"country_code"`
	DeletedAt   *time.Time `db:"deleted_at"`
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"`

	// Added by issue #69's migration.
	DocumentLanguage string  `db:"document_language"`
	Number           *string `db:"number"`
	AddressLine2     *string `db:"address_line2"`
}

// catalogueItemRawRow mirrors the catalogue_items table's own column
// order and names, used only to decode the literal, deliberately
// unscoped "SELECT * FROM catalogue_items" below, the same reason
// accountRawRow and customerRawRow exist for their own tables: that
// query has to stay exactly that, with no column list and no WHERE
// clause, to prove what it is meant to prove.
type catalogueItemRawRow struct {
	ID             uuid.UUID  `db:"id"`
	TenantID       uuid.UUID  `db:"tenant_id"`
	Name           string     `db:"name"`
	Description    *string    `db:"description"`
	SKU            *string    `db:"sku"`
	UnitPriceCents int64      `db:"unit_price_cents"`
	Unit           string     `db:"unit"`
	DeletedAt      *time.Time `db:"deleted_at"`
	CreatedAt      time.Time  `db:"created_at"`
	UpdatedAt      time.Time  `db:"updated_at"`
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

	var customerA, customerB storage.Customer

	if err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
		var err error
		customerA, err = store.Customers().Create(ctx, storage.Customer{
			TenantID: tenantA.ID,
			Name:     "Customer A",
		})

		return err
	}); err != nil {
		t.Fatalf("create customer under tenant A: %v", err)
	}

	if err := store.WithTenant(ctx, tenantB.ID, func(ctx context.Context) error {
		var err error
		customerB, err = store.Customers().Create(ctx, storage.Customer{
			TenantID: tenantB.ID,
			Name:     "Customer B",
		})

		return err
	}); err != nil {
		t.Fatalf("create customer under tenant B: %v", err)
	}

	var catalogueItemA, catalogueItemB storage.CatalogueItem

	if err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
		var err error
		catalogueItemA, err = store.CatalogueItems().Create(ctx, storage.CatalogueItem{
			TenantID:       tenantA.ID,
			Name:           "Catalogue Item A",
			UnitPriceCents: 1000,
		})

		return err
	}); err != nil {
		t.Fatalf("create catalogue item under tenant A: %v", err)
	}

	if err := store.WithTenant(ctx, tenantB.ID, func(ctx context.Context) error {
		var err error
		catalogueItemB, err = store.CatalogueItems().Create(ctx, storage.CatalogueItem{
			TenantID:       tenantB.ID,
			Name:           "Catalogue Item B",
			UnitPriceCents: 2000,
		})

		return err
	}); err != nil {
		t.Fatalf("create catalogue item under tenant B: %v", err)
	}

	var documentA, documentB storage.Document

	if err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
		var err error
		documentA, err = store.Documents().Create(ctx, storage.Document{
			TenantID:   tenantA.ID,
			Type:       storage.DocumentTypeInvoice,
			CustomerID: customerA.ID,
			VATRegime:  storage.VATRegimeStandard,
		}, []storage.DocumentLine{
			{Description: "Line A", QuantityThousandths: 1000, UnitPriceCents: 1000, Unit: "piece", VATRatePermyriad: 1900},
		})

		return err
	}); err != nil {
		t.Fatalf("create document under tenant A: %v", err)
	}

	if err := store.WithTenant(ctx, tenantB.ID, func(ctx context.Context) error {
		var err error
		documentB, err = store.Documents().Create(ctx, storage.Document{
			TenantID:   tenantB.ID,
			Type:       storage.DocumentTypeInvoice,
			CustomerID: customerB.ID,
			VATRegime:  storage.VATRegimeStandard,
		}, []storage.DocumentLine{
			{Description: "Line B", QuantityThousandths: 1000, UnitPriceCents: 2000, Unit: "piece", VATRatePermyriad: 1900},
		})

		return err
	}); err != nil {
		t.Fatalf("create document under tenant B: %v", err)
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

	t.Run("tenant A sees only its own customer through the repository", func(t *testing.T) {
		var customers []storage.Customer

		if err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
			var err error
			customers, err = store.Customers().List(ctx)

			return err
		}); err != nil {
			t.Fatalf("list customers as tenant A: %v", err)
		}

		assertOnlyCustomer(t, customers, customerA.ID)
	})

	t.Run("tenant B sees only its own customer through the repository", func(t *testing.T) {
		var customers []storage.Customer

		if err := store.WithTenant(ctx, tenantB.ID, func(ctx context.Context) error {
			var err error
			customers, err = store.Customers().List(ctx)

			return err
		}); err != nil {
			t.Fatalf("list customers as tenant B: %v", err)
		}

		assertOnlyCustomer(t, customers, customerB.ID)
	})

	t.Run("FindByID for another tenant's customer id returns not found, not the real row", func(t *testing.T) {
		err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
			_, err := store.Customers().FindByID(ctx, customerB.ID)

			return err
		})
		if err == nil {
			t.Fatal("FindByID(customerB.ID) while scoped to tenant A succeeded, want storage.ErrNotFound")
		}
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("FindByID(customerB.ID) while scoped to tenant A: err = %v, want errors.Is(err, storage.ErrNotFound)", err)
		}
	})

	t.Run("Update on another tenant's customer id while scoped to the wrong tenant returns not found", func(t *testing.T) {
		err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
			_, err := store.Customers().Update(ctx, storage.Customer{
				ID:       customerB.ID,
				TenantID: tenantB.ID,
				Name:     "Renamed by tenant A",
			})

			return err
		})
		if err == nil {
			t.Fatal("Update(customerB.ID) while scoped to tenant A succeeded, want storage.ErrNotFound")
		}
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("Update(customerB.ID) while scoped to tenant A: err = %v, want errors.Is(err, storage.ErrNotFound)", err)
		}
	})

	t.Run("Delete on another tenant's customer id while scoped to the wrong tenant returns not found and leaves it active", func(t *testing.T) {
		err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
			return store.Customers().Delete(ctx, customerB.ID)
		})
		if err == nil {
			t.Fatal("Delete(customerB.ID) while scoped to tenant A succeeded, want storage.ErrNotFound")
		}
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("Delete(customerB.ID) while scoped to tenant A: err = %v, want errors.Is(err, storage.ErrNotFound)", err)
		}

		// The error alone only proves the call was rejected; this proves
		// the delete did not take effect either, by confirming tenant B
		// still sees its own customer as active (deleted_at still unset)
		// through its own, correctly scoped List.
		var customers []storage.Customer

		if err := store.WithTenant(ctx, tenantB.ID, func(ctx context.Context) error {
			var err error
			customers, err = store.Customers().List(ctx)

			return err
		}); err != nil {
			t.Fatalf("list customers as tenant B after blocked cross-tenant delete: %v", err)
		}

		assertOnlyCustomer(t, customers, customerB.ID)
	})

	t.Run("an unscoped SELECT star on a transaction scoped to tenant B still sees only tenant B's customer", func(t *testing.T) {
		tx, err := restrictedPool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantB.ID.String()); err != nil {
			t.Fatalf("set_config: %v", err)
		}

		rows, err := tx.Query(ctx, `SELECT * FROM customers`)
		if err != nil {
			t.Fatalf("SELECT * FROM customers: %v", err)
		}

		ids := customerIDs(t, rows)

		if len(ids) != 1 || ids[0] != customerB.ID {
			t.Fatalf("SELECT * FROM customers while scoped to tenant B = %v, want exactly [%v]", ids, customerB.ID)
		}
	})

	t.Run("an INSERT of a customer scoped to tenant B with tenant_id set to tenant A is rejected", func(t *testing.T) {
		// The WITH CHECK half of the policy, exercised on INSERT,
		// bypassing CustomerRepository entirely: a new row whose
		// tenant_id does not match the scope the transaction was
		// set_config'd to must be refused by the policy itself, not
		// merely by the repository's own query filter.
		tx, err := restrictedPool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantB.ID.String()); err != nil {
			t.Fatalf("set_config: %v", err)
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO customers (tenant_id, name) VALUES ($1, $2)`,
			tenantA.ID, "Cross-Tenant Customer")
		if err == nil {
			t.Fatal("INSERT with tenant_id = tenant A while scoped to tenant B succeeded, want it rejected by WITH CHECK")
		}
	})

	t.Run("an UPDATE of a customer scoped to tenant A moving its tenant_id to tenant B either keeps the original tenant or is rejected", func(t *testing.T) {
		// The WITH CHECK half of the policy, exercised on UPDATE this
		// time, bypassing CustomerRepository entirely: the new row
		// version (tenant_id = tenant B) is checked against the scope
		// the transaction is set_config'd to (tenant A), so the row can
		// never actually move to another tenant.
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
			`UPDATE customers SET tenant_id = $1 WHERE id = $2 RETURNING tenant_id`,
			tenantB.ID, customerA.ID,
		).Scan(&gotTenantID)
		if err != nil {
			// Rejected outright: WITH CHECK doing its job, the
			// preferred and expected outcome.
			return
		}

		if gotTenantID != tenantA.ID {
			t.Fatalf("UPDATE moved customer A's tenant_id to %v, want it to stay %v or be rejected entirely", gotTenantID, tenantA.ID)
		}
	})

	t.Run("tenant A sees only its own catalogue item through the repository", func(t *testing.T) {
		var items []storage.CatalogueItem

		if err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
			var err error
			items, err = store.CatalogueItems().List(ctx)

			return err
		}); err != nil {
			t.Fatalf("list catalogue items as tenant A: %v", err)
		}

		assertOnlyCatalogueItem(t, items, catalogueItemA.ID)
	})

	t.Run("tenant B sees only its own catalogue item through the repository", func(t *testing.T) {
		var items []storage.CatalogueItem

		if err := store.WithTenant(ctx, tenantB.ID, func(ctx context.Context) error {
			var err error
			items, err = store.CatalogueItems().List(ctx)

			return err
		}); err != nil {
			t.Fatalf("list catalogue items as tenant B: %v", err)
		}

		assertOnlyCatalogueItem(t, items, catalogueItemB.ID)
	})

	t.Run("FindByID for another tenant's catalogue item id returns not found, not the real row", func(t *testing.T) {
		err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
			_, err := store.CatalogueItems().FindByID(ctx, catalogueItemB.ID)

			return err
		})
		if err == nil {
			t.Fatal("FindByID(catalogueItemB.ID) while scoped to tenant A succeeded, want storage.ErrNotFound")
		}
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("FindByID(catalogueItemB.ID) while scoped to tenant A: err = %v, want errors.Is(err, storage.ErrNotFound)", err)
		}
	})

	t.Run("Update on another tenant's catalogue item id while scoped to the wrong tenant returns not found", func(t *testing.T) {
		err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
			_, err := store.CatalogueItems().Update(ctx, storage.CatalogueItem{
				ID:             catalogueItemB.ID,
				TenantID:       tenantB.ID,
				Name:           "Renamed by tenant A",
				UnitPriceCents: 9999,
			})

			return err
		})
		if err == nil {
			t.Fatal("Update(catalogueItemB.ID) while scoped to tenant A succeeded, want storage.ErrNotFound")
		}
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("Update(catalogueItemB.ID) while scoped to tenant A: err = %v, want errors.Is(err, storage.ErrNotFound)", err)
		}
	})

	t.Run("Delete on another tenant's catalogue item id while scoped to the wrong tenant returns not found and leaves it active", func(t *testing.T) {
		err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
			return store.CatalogueItems().Delete(ctx, catalogueItemB.ID)
		})
		if err == nil {
			t.Fatal("Delete(catalogueItemB.ID) while scoped to tenant A succeeded, want storage.ErrNotFound")
		}
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("Delete(catalogueItemB.ID) while scoped to tenant A: err = %v, want errors.Is(err, storage.ErrNotFound)", err)
		}

		// Same proof as the customer delete subtest above: confirm
		// tenant B still sees its own catalogue item as active through
		// its own, correctly scoped List, not merely that the blocked
		// call returned an error.
		var items []storage.CatalogueItem

		if err := store.WithTenant(ctx, tenantB.ID, func(ctx context.Context) error {
			var err error
			items, err = store.CatalogueItems().List(ctx)

			return err
		}); err != nil {
			t.Fatalf("list catalogue items as tenant B after blocked cross-tenant delete: %v", err)
		}

		assertOnlyCatalogueItem(t, items, catalogueItemB.ID)
	})

	t.Run("an unscoped SELECT star on a transaction scoped to tenant B still sees only tenant B's catalogue item", func(t *testing.T) {
		tx, err := restrictedPool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantB.ID.String()); err != nil {
			t.Fatalf("set_config: %v", err)
		}

		rows, err := tx.Query(ctx, `SELECT * FROM catalogue_items`)
		if err != nil {
			t.Fatalf("SELECT * FROM catalogue_items: %v", err)
		}

		ids := catalogueItemIDs(t, rows)

		if len(ids) != 1 || ids[0] != catalogueItemB.ID {
			t.Fatalf("SELECT * FROM catalogue_items while scoped to tenant B = %v, want exactly [%v]", ids, catalogueItemB.ID)
		}
	})

	t.Run("an INSERT of a catalogue item scoped to tenant B with tenant_id set to tenant A is rejected", func(t *testing.T) {
		// The WITH CHECK half of the policy, exercised on INSERT,
		// bypassing CatalogueItemRepository entirely: a new row whose
		// tenant_id does not match the scope the transaction was
		// set_config'd to must be refused by the policy itself, not
		// merely by the repository's own query filter. unit is
		// deliberately omitted from the column list: it defaults to
		// 'piece' at the database level (see db/migrations), so this
		// INSERT isolates exactly one failure cause, the tenant
		// mismatch, not an unrelated NOT NULL or CHECK violation on a
		// column this subtest does not care about.
		tx, err := restrictedPool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantB.ID.String()); err != nil {
			t.Fatalf("set_config: %v", err)
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO catalogue_items (tenant_id, name, unit_price_cents) VALUES ($1, $2, $3)`,
			tenantA.ID, "Cross-Tenant Catalogue Item", int64(500))
		if err == nil {
			t.Fatal("INSERT with tenant_id = tenant A while scoped to tenant B succeeded, want it rejected by WITH CHECK")
		}
	})

	t.Run("an UPDATE of a catalogue item scoped to tenant A moving its tenant_id to tenant B either keeps the original tenant or is rejected", func(t *testing.T) {
		// The WITH CHECK half of the policy, exercised on UPDATE this
		// time, bypassing CatalogueItemRepository entirely: the new row
		// version (tenant_id = tenant B) is checked against the scope
		// the transaction is set_config'd to (tenant A), so the row can
		// never actually move to another tenant.
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
			`UPDATE catalogue_items SET tenant_id = $1 WHERE id = $2 RETURNING tenant_id`,
			tenantB.ID, catalogueItemA.ID,
		).Scan(&gotTenantID)
		if err != nil {
			// Rejected outright: WITH CHECK doing its job, the
			// preferred and expected outcome.
			return
		}

		if gotTenantID != tenantA.ID {
			t.Fatalf("UPDATE moved catalogue item A's tenant_id to %v, want it to stay %v or be rejected entirely", gotTenantID, tenantA.ID)
		}
	})

	t.Run("tenant A sees only its own document through the repository", func(t *testing.T) {
		var documents []storage.Document

		if err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
			var err error
			documents, err = store.Documents().List(ctx)

			return err
		}); err != nil {
			t.Fatalf("list documents as tenant A: %v", err)
		}

		if len(documents) != 1 || documents[0].ID != documentA.ID {
			t.Fatalf("list documents as tenant A = %v, want exactly [%v]", documents, documentA.ID)
		}
	})

	t.Run("tenant B sees only its own document through the repository", func(t *testing.T) {
		var documents []storage.Document

		if err := store.WithTenant(ctx, tenantB.ID, func(ctx context.Context) error {
			var err error
			documents, err = store.Documents().List(ctx)

			return err
		}); err != nil {
			t.Fatalf("list documents as tenant B: %v", err)
		}

		if len(documents) != 1 || documents[0].ID != documentB.ID {
			t.Fatalf("list documents as tenant B = %v, want exactly [%v]", documents, documentB.ID)
		}
	})

	t.Run("FindByID for another tenant's document id returns not found, not the real row", func(t *testing.T) {
		err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
			_, err := store.Documents().FindByID(ctx, documentB.ID)

			return err
		})
		if err == nil {
			t.Fatal("FindByID(documentB.ID) while scoped to tenant A succeeded, want storage.ErrNotFound")
		}
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("FindByID(documentB.ID) while scoped to tenant A: err = %v, want errors.Is(err, storage.ErrNotFound)", err)
		}
	})

	t.Run("a document's lines are invisible while scoped to the wrong tenant, bypassing DocumentRepository entirely", func(t *testing.T) {
		// documentB's line was created under tenant B; a raw, unscoped
		// "SELECT * FROM document_lines" while scoped to tenant A must
		// still see none of it, the same proof
		// TestEveryTenantScopedTableEnforcesRowLevelSecurity's existence
		// check does generically for every tenant_id-bearing table, made
		// concrete here for this one.
		tx, err := restrictedPool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantA.ID.String()); err != nil {
			t.Fatalf("set_config: %v", err)
		}

		var count int

		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_lines WHERE document_id = $1`, documentB.ID).Scan(&count); err != nil {
			t.Fatalf("count document_lines for tenant B's document while scoped to tenant A: %v", err)
		}

		if count != 0 {
			t.Fatalf("count = %d, want 0: tenant B's document line is visible while scoped to tenant A", count)
		}
	})

	t.Run("an INSERT of a document scoped to tenant B with tenant_id set to tenant A is rejected", func(t *testing.T) {
		// The WITH CHECK half of the policy, exercised on INSERT,
		// bypassing DocumentRepository entirely. Asserting the SQLSTATE
		// and message, not merely err != nil: a foreign key violation, a
		// CHECK violation or a type error would all satisfy "an error
		// occurred" without the policy itself ever having refused
		// anything, the same reason
		// document_immutability_integration_test.go pins its own
		// rejections to a specific class and message rather than just
		// checking for an error.
		tx, err := restrictedPool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantB.ID.String()); err != nil {
			t.Fatalf("set_config: %v", err)
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO documents (tenant_id, type, customer_id, vat_regime) VALUES ($1, 'invoice', $2, 'standard')`,
			tenantA.ID, customerA.ID)
		if err == nil {
			t.Fatal("INSERT with tenant_id = tenant A while scoped to tenant B succeeded, want it rejected by WITH CHECK")
		}

		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) {
			t.Fatalf("err = %v, want a *pgconn.PgError", err)
		}

		if pgErr.Code != pgInsufficientPrivilegeClass {
			t.Fatalf("pgErr.Code = %q, want %q (row level security's own WITH CHECK rejection class)", pgErr.Code, pgInsufficientPrivilegeClass)
		}

		if !strings.Contains(pgErr.Message, "row-level security") {
			t.Fatalf("pgErr.Message = %q, want it to name row-level security: this must be the policy's own rejection, not some other error", pgErr.Message)
		}
	})

	t.Run("tenant A's document_numbers counter is invisible and unaffected while scoped to tenant B", func(t *testing.T) {
		// Allocating under tenant A, then reading the same (type, year)
		// key while scoped to tenant B through DocumentNumbers().Current,
		// proves at most that Current's own SQL predicate works: that
		// query (queries/document_numbers.sql's GetDocumentNumber)
		// already names "AND tenant_id = NULLIF(current_setting(...),
		// '')::uuid" in its own WHERE clause, so it would answer this
		// same 0 even against document_numbers' row level security
		// policy replaced outright by USING (true) WITH CHECK (true):
		// the repository's own filter, not the policy, would be doing
		// all the work. It is still worth keeping as a contract test for
		// Current itself.
		const year = 2031 // far from any other test's own year, avoiding collision

		if err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
			_, err := store.DocumentNumbers().AllocateNext(ctx, storage.DocumentTypeInvoice, year)

			return err
		}); err != nil {
			t.Fatalf("allocate under tenant A: %v", err)
		}

		var currentForB int64

		if err := store.WithTenant(ctx, tenantB.ID, func(ctx context.Context) error {
			var err error
			currentForB, err = store.DocumentNumbers().Current(ctx, storage.DocumentTypeInvoice, year)

			return err
		}); err != nil {
			t.Fatalf("read current counter under tenant B: %v", err)
		}

		if currentForB != 0 {
			t.Fatalf("tenant B's view of the invoice/%d counter = %d, want 0 (tenant A's own allocation must be invisible)", year, currentForB)
		}

		// The actual proof of document_numbers' own policy: a raw,
		// unscoped read under the restricted role, scoped to tenant B,
		// naming tenant A's row explicitly in its own WHERE clause (the
		// same pattern the document_lines subtest above uses). If the
		// policy were the widened USING (true) WITH CHECK (true) that
		// would defeat the read above, this one would still find the
		// row and fail.
		tx, err := restrictedPool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantB.ID.String()); err != nil {
			t.Fatalf("set_config: %v", err)
		}

		var count int

		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM document_numbers WHERE tenant_id = $1 AND type = 'invoice' AND number_year = $2`,
			tenantA.ID, year).Scan(&count); err != nil {
			t.Fatalf("count document_numbers for tenant A's own key while scoped to tenant B: %v", err)
		}

		if count != 0 {
			t.Fatalf("count = %d, want 0: tenant A's document_numbers row is visible while scoped to tenant B", count)
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

// TestCatalogueItemRepository_Update exercises Update's own contract on
// the unit column, as opposed to tenant isolation (TestRowLevelSecurity
// above already covers that for every column, unit included). Update is
// a full-column replace: a caller that leaves Unit as the Go zero value
// is supplying an incomplete item, which must fail loudly against the
// column's NOT NULL and CHECK constraints rather than being silently
// coerced to "piece", the mistake this test reproduces and then pins
// against a regression.
func TestCatalogueItemRepository_Update(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed catalogue item update test")
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

	tenant, err := store.Tenants().Create(ctx, storage.Tenant{Slug: "tenant-update-unit-" + uuid.NewString(), Name: "Tenant Update Unit"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	var dayRateItem storage.CatalogueItem

	if err := store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
		var err error
		dayRateItem, err = store.CatalogueItems().Create(ctx, storage.CatalogueItem{
			TenantID:       tenant.ID,
			Name:           "Day-Rate Hire",
			UnitPriceCents: 50000,
			Unit:           "day",
		})

		return err
	}); err != nil {
		t.Fatalf("create day-rate catalogue item: %v", err)
	}

	if dayRateItem.Unit != "day" {
		t.Fatalf("created item Unit = %q, want %q", dayRateItem.Unit, "day")
	}

	t.Run("Update without a Unit is rejected, not silently coerced to piece", func(t *testing.T) {
		err := store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
			_, err := store.CatalogueItems().Update(ctx, storage.CatalogueItem{
				ID:             dayRateItem.ID,
				TenantID:       tenant.ID,
				Name:           "Day-Rate Hire, renamed",
				UnitPriceCents: 60000,
				// Unit deliberately left as the Go zero value: the
				// caller forgot to carry it forward, the exact mistake
				// this test exists to catch.
			})

			return err
		})
		if err == nil {
			t.Fatal("Update with an empty Unit succeeded, want it rejected by the unit CHECK constraint")
		}
		if errors.Is(err, storage.ErrNotFound) || errors.Is(err, storage.ErrConflict) {
			t.Fatalf("Update with an empty Unit failed with %v, want a plain CHECK-violation error, not a storage sentinel that implies the row itself was the problem", err)
		}

		// The failed Update must not have taken effect at all: refetch
		// the row under its own, successful transaction and confirm
		// Unit is still "day", not silently rewritten to "piece".
		var reread storage.CatalogueItem

		if err := store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
			var err error
			reread, err = store.CatalogueItems().FindByID(ctx, dayRateItem.ID)

			return err
		}); err != nil {
			t.Fatalf("FindByID after the rejected update: %v", err)
		}

		if reread.Unit != "day" {
			t.Fatalf("Unit after the rejected update = %q, want %q (unchanged)", reread.Unit, "day")
		}
		if reread.Name != "Day-Rate Hire" {
			t.Fatalf("Name after the rejected update = %q, want %q (unchanged, same full-row rejection)", reread.Name, "Day-Rate Hire")
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

// tablesWithoutTenantID is every table in the public schema allowed to
// have no tenant_id column, with the reason. A new table without
// tenant_id that is missing here fails
// TestEveryTableIsTenantScopedOrAllowlisted: a table that forgot its
// tenant_id would otherwise escape the row level security test above
// entirely, since that test only looks at tables that have the column.
var tablesWithoutTenantID = map[string]string{
	"tenants":           "a row here is a tenant, not something a tenant owns",
	"schema_migrations": "dbmate's own bookkeeping, never read by the application",
}

// TestEveryTableIsTenantScopedOrAllowlisted closes the gap the test above
// leaves: every table in the public schema, ordinary or partitioned (the
// parent, not its partitions), either carries tenant_id (and is then
// checked for row level security above) or is listed in
// tablesWithoutTenantID with a reason.
func TestEveryTableIsTenantScopedOrAllowlisted(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed table introspection test")
	}

	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	rows, err := adminPool.Query(ctx, `
SELECT c.relname
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public'
  AND c.relkind IN ('r', 'p')
  AND NOT c.relispartition
  AND NOT EXISTS (
      SELECT 1 FROM information_schema.columns col
      WHERE col.table_schema = 'public'
        AND col.table_name = c.relname
        AND col.column_name = 'tenant_id'
  )
ORDER BY c.relname`)
	if err != nil {
		t.Fatalf("introspect tables without tenant_id: %v", err)
	}

	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("scan tables without tenant_id: %v", err)
	}

	for _, name := range names {
		if _, ok := tablesWithoutTenantID[name]; !ok {
			t.Errorf("table %q has no tenant_id column and is not in tablesWithoutTenantID: "+
				"add tenant_id with row level security, or allowlist it with a reason", name)
		}
	}
}

// TestAppRoleCannotChangeFiles asserts the primary control of the
// receipt storage design (docs/concepts/solobill.md, "Receipt storage and
// GoBD"): the application role can read and insert files rows, never
// update or delete them. The files table lands with issue #75; until
// then this test skips.
func TestAppRoleCannotChangeFiles(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed privilege test")
	}

	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	var exists bool
	if err := adminPool.QueryRow(ctx, `SELECT to_regclass('public.files') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatalf("look up files: %v", err)
	}
	if !exists {
		t.Skip("table files does not exist yet; it lands with issue #75")
	}

	setUpTestAppRole(ctx, t, adminPool, adminURL)

	for privilege, want := range map[string]bool{"SELECT": true, "INSERT": true, "UPDATE": false, "DELETE": false} {
		var got bool
		if err := adminPool.QueryRow(ctx, `SELECT has_table_privilege($1, 'public.files', $2)`, testAppRole, privilege).Scan(&got); err != nil {
			t.Fatalf("has_table_privilege(%s): %v", privilege, err)
		}
		if got != want {
			t.Errorf("%s on files = %v, want %v", privilege, got, want)
		}
	}

	// has_table_privilege answers for the whole table only. A column
	// level grant ("GRANT UPDATE (sha256) ON files") would still let the
	// role change that column, so every column is checked too.
	rows, err := adminPool.Query(ctx, `
SELECT a.attname
FROM pg_attribute a
WHERE a.attrelid = 'public.files'::regclass
  AND a.attnum > 0
  AND NOT a.attisdropped
  AND has_column_privilege($1, 'public.files', a.attname, 'UPDATE')`, testAppRole)
	if err != nil {
		t.Fatalf("check column privileges on files: %v", err)
	}
	columns, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("scan column privileges on files: %v", err)
	}
	if len(columns) != 0 {
		t.Errorf("the app role can UPDATE files columns %v, want none", columns)
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

	// Granted from the same files docker-compose.yaml's "grant-app-role"
	// service applies (docker/postgres/grants, one file per issue, in file
	// name order), with the role name replaced, so the test role carries
	// exactly the application role's privileges and the two lists cannot
	// drift. See the header of the base file for the per-table reasoning,
	// and document_immutability_integration_test.go for the subtest that
	// exercises document_lines' missing UPDATE.
	if _, err := adminPool.Exec(ctx, `GRANT USAGE ON SCHEMA public TO `+testAppRole); err != nil {
		t.Fatalf("grant %s schema usage: %v", testAppRole, err)
	}

	grantFiles, err := filepath.Glob(filepath.Join("..", "..", "..", "docker", "postgres", "grants", "*.sql"))
	if err != nil || len(grantFiles) == 0 {
		t.Fatalf("find grant files: %v (found %d)", err, len(grantFiles))
	}
	sort.Strings(grantFiles)

	for _, file := range grantFiles {
		sql, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if _, err := adminPool.Exec(ctx, strings.ReplaceAll(string(sql), "solobill_app", testAppRole)); err != nil {
			t.Fatalf("grant %s privileges from %s: %v", testAppRole, file, err)
		}
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

// customerIDs decodes a literal "SELECT * FROM customers" result into
// the ids it carries, the same pattern accountIDs uses for accounts:
// customerRawRow's "db" tags, not column position, do the matching, so
// the test stays correct even if a later migration adds a column to
// customers.
func customerIDs(t *testing.T, rows pgx.Rows) []uuid.UUID {
	t.Helper()

	records, err := pgx.CollectRows(rows, pgx.RowToStructByName[customerRawRow])
	if err != nil {
		t.Fatalf("scan customers: %v", err)
	}

	ids := make([]uuid.UUID, len(records))
	for i, record := range records {
		ids[i] = record.ID
	}

	return ids
}

func assertOnlyCustomer(t *testing.T, customers []storage.Customer, wantID uuid.UUID) {
	t.Helper()

	if len(customers) != 1 {
		t.Fatalf("got %d customers, want exactly 1", len(customers))
	}

	if customers[0].ID != wantID {
		t.Fatalf("got customer %v, want %v", customers[0].ID, wantID)
	}
}

// catalogueItemIDs decodes a literal "SELECT * FROM catalogue_items"
// result into the ids it carries, the same pattern accountIDs and
// customerIDs use for their own tables: catalogueItemRawRow's "db"
// tags, not column position, do the matching, so the test stays
// correct even if a later migration adds a column to catalogue_items.
func catalogueItemIDs(t *testing.T, rows pgx.Rows) []uuid.UUID {
	t.Helper()

	records, err := pgx.CollectRows(rows, pgx.RowToStructByName[catalogueItemRawRow])
	if err != nil {
		t.Fatalf("scan catalogue items: %v", err)
	}

	ids := make([]uuid.UUID, len(records))
	for i, record := range records {
		ids[i] = record.ID
	}

	return ids
}

func assertOnlyCatalogueItem(t *testing.T, items []storage.CatalogueItem, wantID uuid.UUID) {
	t.Helper()

	if len(items) != 1 {
		t.Fatalf("got %d catalogue items, want exactly 1", len(items))
	}

	if items[0].ID != wantID {
		t.Fatalf("got catalogue item %v, want %v", items[0].ID, wantID)
	}
}
