package auth

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/account"
	"github.com/ostendorf-cloud/govue/account/accounttest"
	"github.com/ostendorf-cloud/govue/passkey"
	"github.com/ostendorf-cloud/govue/passkey/passkeytest"

	"github.com/ostendorf-cloud/solobill/internal/auth/authtest"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// These run govue's conformance suites against the adapters over the in
// memory fakes, so the mapping in AccountStore and PasskeyStore (ids,
// errors, empty values, the NULL counter) is checked on every go test.
// The suites against PostgreSQL, which prove the SQL statements atomic and
// the tenant scoping real, are TestAccountStoreConformance and
// TestPasskeyStoreConformance in internal/storage/postgres, integration
// only.
//
// The fakes serialise every read, compare and write under one mutex, so
// fewer race rounds than govue's default lose nothing here.

func TestAccountStoreConformanceInMemory(t *testing.T) {
	store := newFakeStore()
	tenant := store.addTenant(storage.Tenant{Slug: "acme", Name: "Acme"})

	accounttest.Run(t, accounttest.Harness{
		NewStore: func(*testing.T) account.Store {
			return authtest.ScopedAccountStore{
				Store: NewAccountStore(store),
				Scope: func(ctx context.Context) context.Context { return ContextWithTenant(ctx, tenant.ID) },
			}
		},
		FoldsCase: true,
		Disable:   func(t *testing.T, _ account.Store, id string) { disableFakeAccount(t, store, id) },
		Rounds:    50,
	})
}

func TestPasskeyStoreConformanceInMemory(t *testing.T) {
	store := newFakeStore()
	tenant := store.addTenant(storage.Tenant{Slug: "acme", Name: "Acme"})

	passkeytest.Run(t, passkeytest.Harness{
		NewStore: func(*testing.T) passkey.Store {
			return authtest.ScopedPasskeyStore{
				Store: NewPasskeyStore(store),
				Scope: func(ctx context.Context) context.Context { return ContextWithTenant(ctx, tenant.ID) },
			}
		},
		NewAccount: func(*testing.T, passkey.Store) account.Identity {
			email := "passkey-" + uuid.NewString() + "@acme.example"
			a := store.addAccount(storage.Account{
				TenantID: tenant.ID,
				Email:    email,
				Role:     storage.AccountRoleMember,
				Status:   storage.AccountStatusActive,
			})

			return account.Identity{ID: a.ID.String(), Login: email}
		},
		Disable: func(t *testing.T, _ passkey.Store, id string) { disableFakeAccount(t, store, id) },
		Rounds:  50,
	})
}

func disableFakeAccount(t *testing.T, store *fakeStore, id string) {
	t.Helper()

	accountID, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("parse account id %q: %v", id, err)
	}

	a, err := store.Accounts().FindByID(t.Context(), accountID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}

	a.Status = storage.AccountStatusDisabled
	if _, err := store.Accounts().Update(t.Context(), a); err != nil {
		t.Fatalf("Update: %v", err)
	}
}
