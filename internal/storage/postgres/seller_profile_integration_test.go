//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres"
)

// profileTestEnv is a store connected as the restricted, NOBYPASSRLS role,
// two tenants and the restricted pool for raw statements.
type profileTestEnv struct {
	store *postgres.Store
	pool  *pgxpool.Pool
	a, b  storage.Tenant
}

func newProfileTestEnv(t *testing.T) profileTestEnv {
	t.Helper()

	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed seller profile test")
	}

	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	restrictedURL := setUpTestAppRole(ctx, t, adminPool, adminURL)

	pool, err := postgres.NewPool(ctx, restrictedURL)
	if err != nil {
		t.Fatalf("open restricted pool as %s: %v", testAppRole, err)
	}
	t.Cleanup(pool.Close)

	store := postgres.NewStore(pool)

	a, err := store.Tenants().Create(ctx, storage.Tenant{Slug: "profile-a-" + uuid.NewString(), Name: "A"})
	if err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	b, err := store.Tenants().Create(ctx, storage.Tenant{Slug: "profile-b-" + uuid.NewString(), Name: "B"})
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	return profileTestEnv{store: store, pool: pool, a: a, b: b}
}

func profileFor(tenant storage.Tenant, name string) storage.SellerProfile {
	tax := "12/345/67890"

	return storage.SellerProfile{
		TenantID:         tenant.ID,
		LegalName:        name,
		Street:           "Main Street 1",
		PostalCode:       "12345",
		City:             "Example City",
		CountryCode:      "DE",
		TaxNumber:        &tax,
		DefaultVATRegime: storage.VATRegimeKleinunternehmer,
		DefaultLanguage:  storage.DocumentLanguageGerman,
		NumberFormats: map[storage.DocumentType]string{
			storage.DocumentTypeInvoice: "RE-{year}{seq:4}",
			storage.DocumentTypeOffer:   "AN-{year}{seq:4}",
		},
	}
}

func TestSellerProfileIsIsolatedPerTenant(t *testing.T) {
	env := newProfileTestEnv(t)
	ctx := context.Background()

	if err := env.store.WithTenant(ctx, env.a.ID, func(ctx context.Context) error {
		_, err := env.store.SellerProfiles().Put(ctx, profileFor(env.a, "Seller A"))

		return err
	}); err != nil {
		t.Fatalf("put profile A: %v", err)
	}

	// Another tenant reads nothing.
	err := env.store.WithTenant(ctx, env.b.ID, func(ctx context.Context) error {
		_, err := env.store.SellerProfiles().Get(ctx)

		return err
	})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("tenant B Get() error = %v, want ErrNotFound", err)
	}

	// Another tenant cannot write a row for tenant A, whatever the row says.
	err = env.store.WithTenant(ctx, env.b.ID, func(ctx context.Context) error {
		_, err := env.store.SellerProfiles().Put(ctx, profileFor(env.a, "Overwritten by B"))

		return err
	})
	if err == nil {
		t.Error("tenant B wrote a profile row for tenant A")
	}

	// An upsert for the owner's own tenant replaces its row, and B's
	// attempt left A's row untouched.
	var got storage.SellerProfile
	if err := env.store.WithTenant(ctx, env.a.ID, func(ctx context.Context) error {
		first, err := env.store.SellerProfiles().Get(ctx)
		if err != nil {
			return err
		}
		if first.LegalName != "Seller A" {
			t.Errorf("tenant A LegalName = %q, want Seller A", first.LegalName)
		}

		updated := profileFor(env.a, "Seller A renamed")
		updated.NumberFormats[storage.DocumentTypeInvoice] = "RG-{year}{seq:4}"
		if _, err := env.store.SellerProfiles().Put(ctx, updated); err != nil {
			return err
		}
		got, err = env.store.SellerProfiles().Get(ctx)

		return err
	}); err != nil {
		t.Fatalf("tenant A read and replace: %v", err)
	}
	if got.LegalName != "Seller A renamed" || got.NumberFormats[storage.DocumentTypeInvoice] != "RG-{year}{seq:4}" {
		t.Errorf("replaced profile = %+v", got)
	}

	// Without a tenant scope the repository refuses, and a raw unscoped
	// SELECT through the restricted role sees no row at all.
	if _, err := env.store.SellerProfiles().Get(ctx); !errors.Is(err, storage.ErrNoTenantScope) {
		t.Errorf("unscoped Get() error = %v, want ErrNoTenantScope", err)
	}
	var count int
	if err := env.pool.QueryRow(ctx, `SELECT count(*) FROM seller_profiles`).Scan(&count); err != nil {
		t.Fatalf("raw count: %v", err)
	}
	if count != 0 {
		t.Errorf("unscoped SELECT saw %d rows, want 0", count)
	}
}

func TestVATRegimePeriodsAreIsolatedAndAppendOnly(t *testing.T) {
	env := newProfileTestEnv(t)
	ctx := context.Background()
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}

		return d
	}

	var first, second storage.VATRegimePeriod
	if err := env.store.WithTenant(ctx, env.a.ID, func(ctx context.Context) error {
		repo := env.store.VATRegimePeriods()
		var err error
		first, err = repo.Create(ctx, storage.VATRegimePeriod{TenantID: env.a.ID, ValidFrom: day("2026-01-01"), Regime: storage.VATRegimeKleinunternehmer})
		if err != nil {
			return err
		}
		second, err = repo.Create(ctx, storage.VATRegimePeriod{TenantID: env.a.ID, ValidFrom: day("2026-10-14"), Regime: storage.VATRegimeStandard})

		return err
	}); err != nil {
		t.Fatalf("create periods: %v", err)
	}

	t.Run("rules of one tenant", func(t *testing.T) {
		err := env.store.WithTenant(ctx, env.a.ID, func(ctx context.Context) error {
			repo := env.store.VATRegimePeriods()

			// Same valid_from as an effective row: conflict.
			if _, err := repo.Create(ctx, storage.VATRegimePeriod{TenantID: env.a.ID, ValidFrom: day("2026-01-01"), Regime: storage.VATRegimeStandard}); !errors.Is(err, storage.ErrConflict) {
				t.Errorf("duplicate valid_from error = %v, want ErrConflict", err)
			}

			// A correction may reuse the wrong row's valid_from.
			fix, err := repo.Create(ctx, storage.VATRegimePeriod{TenantID: env.a.ID, ValidFrom: day("2026-01-01"), Regime: storage.VATRegimeStandard, SupersedesID: &first.ID})
			if err != nil {
				t.Errorf("correction error = %v", err)

				return nil
			}

			// A row is superseded at most once.
			if _, err := repo.Create(ctx, storage.VATRegimePeriod{TenantID: env.a.ID, ValidFrom: day("2026-02-01"), Regime: storage.VATRegimeStandard, SupersedesID: &first.ID}); !errors.Is(err, storage.ErrConflict) {
				t.Errorf("second supersede error = %v, want ErrConflict", err)
			}

			// The effective regime on a date skips the superseded row.
			got, err := repo.RegimeOn(ctx, day("2026-03-01"))
			if err != nil || got.ID != fix.ID || got.Regime != storage.VATRegimeStandard {
				t.Errorf("RegimeOn(2026-03-01) = %+v, %v, want the correction", got, err)
			}
			got, err = repo.RegimeOn(ctx, day("2026-10-14"))
			if err != nil || got.ID != second.ID {
				t.Errorf("RegimeOn(2026-10-14) = %+v, %v, want the second period", got, err)
			}
			if _, err := repo.RegimeOn(ctx, day("2025-12-31")); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("RegimeOn before the first period error = %v, want ErrNotFound", err)
			}

			list, err := repo.List(ctx)
			if err != nil || len(list) != 3 {
				t.Errorf("List() = %d rows, %v, want 3 with the superseded one", len(list), err)
			}

			return nil
		})
		if err != nil {
			t.Fatalf("tenant A scope: %v", err)
		}
	})

	t.Run("another tenant", func(t *testing.T) {
		err := env.store.WithTenant(ctx, env.b.ID, func(ctx context.Context) error {
			repo := env.store.VATRegimePeriods()

			list, err := repo.List(ctx)
			if err != nil || len(list) != 0 {
				t.Errorf("tenant B List() = %d rows, %v, want none", len(list), err)
			}
			if _, err := repo.RegimeOn(ctx, day("2026-12-01")); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("tenant B RegimeOn error = %v, want ErrNotFound", err)
			}

			// Superseding another tenant's row is a not found, never a hit.
			if _, err := repo.Create(ctx, storage.VATRegimePeriod{TenantID: env.b.ID, ValidFrom: day("2026-05-01"), Regime: storage.VATRegimeStandard, SupersedesID: &second.ID}); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("tenant B supersedes A's row error = %v, want ErrNotFound", err)
			}

			return nil
		})
		if err != nil {
			t.Fatalf("tenant B scope: %v", err)
		}

		// A row written for tenant A from B's scope is refused by the
		// policy's WITH CHECK. Postgres aborts the transaction on that
		// error, so it gets a scope of its own that returns the refusal
		// and rolls back, instead of committing an aborted transaction.
		err = env.store.WithTenant(ctx, env.b.ID, func(ctx context.Context) error {
			_, err := env.store.VATRegimePeriods().Create(ctx, storage.VATRegimePeriod{
				TenantID: env.a.ID, ValidFrom: day("2026-06-01"), Regime: storage.VATRegimeStandard,
			})

			return err
		})
		if err == nil {
			t.Error("tenant B wrote a period row for tenant A")
		}

		// The refused row is not visible to tenant A either.
		if err := env.store.WithTenant(ctx, env.a.ID, func(ctx context.Context) error {
			list, err := env.store.VATRegimePeriods().List(ctx)
			if err != nil {
				return err
			}
			for _, p := range list {
				if p.ValidFrom.Equal(day("2026-06-01")) {
					t.Error("the refused row of tenant B exists")
				}
			}

			return nil
		}); err != nil {
			t.Fatalf("tenant A scope: %v", err)
		}
	})

	t.Run("append only", func(t *testing.T) {
		// The restricted role has no UPDATE and no DELETE privilege.
		if _, err := env.pool.Exec(ctx, `UPDATE vat_regime_periods SET regime = 'standard'`); err == nil {
			t.Error("the application role updated vat_regime_periods")
		}
		if _, err := env.pool.Exec(ctx, `DELETE FROM vat_regime_periods`); err == nil {
			t.Error("the application role deleted from vat_regime_periods")
		}
	})
}
