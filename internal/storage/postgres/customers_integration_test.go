//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres"
)

// TestCustomerRepository covers the columns of issue #69's migration, the
// soft delete rules and the refusal of a cross tenant write, all through
// the restricted application role, so row level security applies.
func TestCustomerRepository(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed customer repository test")
	}

	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	restrictedPool, err := postgres.NewPool(ctx, setUpTestAppRole(ctx, t, adminPool, adminURL))
	if err != nil {
		t.Fatalf("open restricted pool: %v", err)
	}
	t.Cleanup(restrictedPool.Close)

	store := postgres.NewStore(restrictedPool)

	newTenant := func(name string) storage.Tenant {
		tenant, err := store.Tenants().Create(ctx, storage.Tenant{Slug: name + "-" + uuid.NewString(), Name: name})
		if err != nil {
			t.Fatalf("create tenant: %v", err)
		}

		return tenant
	}
	tenantA, tenantB := newTenant("customers-a"), newTenant("customers-b")

	in := func(tenant storage.Tenant, fn func(ctx context.Context) error) error {
		return store.WithTenant(ctx, tenant.ID, fn)
	}

	text := func(s string) *string { return &s }

	var created storage.Customer
	if err := in(tenantA, func(ctx context.Context) error {
		var err error
		created, err = store.Customers().Create(ctx, storage.Customer{
			TenantID:         tenantA.ID,
			Name:             "Customer Ltd",
			Number:           text("K-1"),
			AddressLine2:     text("Floor 3"),
			DocumentLanguage: storage.DocumentLanguageEnglish,
		})

		return err
	}); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	if created.Number == nil || *created.Number != "K-1" || created.AddressLine2 == nil || created.DocumentLanguage != storage.DocumentLanguageEnglish {
		t.Fatalf("created customer lost a column: %+v", created)
	}

	t.Run("an unset language is stored as German", func(t *testing.T) {
		var c storage.Customer
		if err := in(tenantA, func(ctx context.Context) error {
			var err error
			c, err = store.Customers().Create(ctx, storage.Customer{TenantID: tenantA.ID, Name: "No language"})

			return err
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
		if c.DocumentLanguage != storage.DocumentLanguageGerman {
			t.Fatalf("DocumentLanguage = %q, want de", c.DocumentLanguage)
		}
	})

	t.Run("a duplicate number conflicts, case insensitively, within a tenant only", func(t *testing.T) {
		err := in(tenantA, func(ctx context.Context) error {
			_, err := store.Customers().Create(ctx, storage.Customer{TenantID: tenantA.ID, Name: "Dup", Number: text("k-1")})

			return err
		})
		if !errors.Is(err, storage.ErrConflict) {
			t.Fatalf("duplicate number err = %v, want ErrConflict", err)
		}

		if err := in(tenantB, func(ctx context.Context) error {
			_, err := store.Customers().Create(ctx, storage.Customer{TenantID: tenantB.ID, Name: "Other tenant", Number: text("K-1")})

			return err
		}); err != nil {
			t.Fatalf("the same number in another tenant: %v", err)
		}
	})

	t.Run("another tenant can neither update nor delete the row", func(t *testing.T) {
		err := in(tenantB, func(ctx context.Context) error {
			_, err := store.Customers().Update(ctx, storage.Customer{ID: created.ID, Name: "Hijacked"})

			return err
		})
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("cross tenant update err = %v, want ErrNotFound", err)
		}
		err = in(tenantB, func(ctx context.Context) error { return store.Customers().Delete(ctx, created.ID) })
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("cross tenant delete err = %v, want ErrNotFound", err)
		}

		// A write that names the other tenant is refused by the policy.
		err = in(tenantB, func(ctx context.Context) error {
			_, err := store.Customers().Create(ctx, storage.Customer{TenantID: tenantA.ID, Name: "Forged"})

			return err
		})
		if err == nil {
			t.Fatal("creating a row for another tenant succeeded, want the policy to refuse it")
		}

		var reread storage.Customer
		if err := in(tenantA, func(ctx context.Context) error {
			var err error
			reread, err = store.Customers().FindByID(ctx, created.ID)

			return err
		}); err != nil {
			t.Fatalf("reread: %v", err)
		}
		if reread.Name != "Customer Ltd" || reread.DeletedAt != nil {
			t.Fatalf("tenant A's row changed: %+v", reread)
		}
	})

	t.Run("a soft deleted customer stays readable by id and frees its number", func(t *testing.T) {
		if err := in(tenantA, func(ctx context.Context) error { return store.Customers().Delete(ctx, created.ID) }); err != nil {
			t.Fatalf("delete: %v", err)
		}

		var found storage.Customer
		var list []storage.Customer
		if err := in(tenantA, func(ctx context.Context) error {
			var err error
			if found, err = store.Customers().FindByID(ctx, created.ID); err != nil {
				return err
			}
			list, err = store.Customers().List(ctx)

			return err
		}); err != nil {
			t.Fatalf("read after delete: %v", err)
		}
		if found.DeletedAt == nil {
			t.Fatal("FindByID of a deleted customer has no DeletedAt")
		}
		for _, c := range list {
			if c.ID == created.ID {
				t.Fatal("List returned a soft deleted customer")
			}
		}

		if err := in(tenantA, func(ctx context.Context) error {
			_, err := store.Customers().Create(ctx, storage.Customer{TenantID: tenantA.ID, Name: "Reuse", Number: text("K-1")})

			return err
		}); err != nil {
			t.Fatalf("reusing a deleted customer's number: %v", err)
		}
	})
}
