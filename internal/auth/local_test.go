package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// localTestFixture is one tenant and one fully active account, shared by
// every case in TestLocalAuthenticatorLogin: each case tries a single
// wrong ingredient against it.
type localTestFixture struct {
	store    *fakeStore
	tenant   storage.Tenant
	account  storage.Account
	password string
	secret   string
	now      time.Time
}

func newLocalTestFixture(t *testing.T) localTestFixture {
	t.Helper()

	store := newFakeStore()
	tenant := store.addTenant(storage.Tenant{Slug: "acme", Name: "Acme"})

	password := "a reasonably long passphrase"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}

	account := store.addAccount(storage.Account{
		TenantID:     tenant.ID,
		Email:        "owner@acme.example",
		PasswordHash: &hash,
		TOTPSecret:   &secret,
		Role:         storage.AccountRoleOwner,
		Status:       storage.AccountStatusActive,
	})

	return localTestFixture{
		store:    store,
		tenant:   tenant,
		account:  account,
		password: password,
		secret:   secret,
		now:      time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC),
	}
}

func (f localTestFixture) code(t *testing.T) string {
	t.Helper()

	code, err := GenerateTOTPCode(f.secret, f.now)
	if err != nil {
		t.Fatalf("GenerateTOTPCode: %v", err)
	}

	return code
}

func TestLocalAuthenticatorLoginWithCorrectCredentials(t *testing.T) {
	t.Parallel()

	f := newLocalTestFixture(t)
	auth := NewLocalAuthenticator(f.store, func() time.Time { return f.now })

	id, err := auth.Login(t.Context(), f.tenant.Slug, f.account.Email, f.password, f.code(t))
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if id.AccountID != f.account.ID {
		t.Errorf("AccountID = %v, want %v", id.AccountID, f.account.ID)
	}
	if id.TenantID != f.tenant.ID {
		t.Errorf("TenantID = %v, want %v", id.TenantID, f.tenant.ID)
	}
	if id.Role != storage.AccountRoleOwner {
		t.Errorf("Role = %v, want %v", id.Role, storage.AccountRoleOwner)
	}
	if id.Email != f.account.Email {
		t.Errorf("Email = %v, want %v", id.Email, f.account.Email)
	}
}

func TestLocalAuthenticatorLoginFailsIndistinguishably(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(f *localTestFixture, slug, email, password, code *string)
	}{
		{
			name:   "unknown tenant slug",
			mutate: func(_ *localTestFixture, slug, _, _, _ *string) { *slug = "no-such-tenant" },
		},
		{
			name:   "unknown email",
			mutate: func(_ *localTestFixture, _, email, _, _ *string) { *email = "nobody@acme.example" },
		},
		{
			name:   "wrong password",
			mutate: func(_ *localTestFixture, _, _, password, _ *string) { *password = "the wrong passphrase" },
		},
		{
			name:   "wrong totp code",
			mutate: func(_ *localTestFixture, _, _, _, code *string) { *code = "000000" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newLocalTestFixture(t)
			slug, email, password, code := f.tenant.Slug, f.account.Email, f.password, f.code(t)
			tt.mutate(&f, &slug, &email, &password, &code)

			authenticator := NewLocalAuthenticator(f.store, func() time.Time { return f.now })

			_, err := authenticator.Login(t.Context(), slug, email, password, code)
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Errorf("err = %v, want errors.Is(err, ErrInvalidCredentials)", err)
			}
		})
	}
}

func TestLocalAuthenticatorLoginRejectsAPendingAccount(t *testing.T) {
	t.Parallel()

	f := newLocalTestFixture(t)
	f.account.Status = storage.AccountStatusPending
	f.store.accounts[f.account.ID] = f.account

	authenticator := NewLocalAuthenticator(f.store, func() time.Time { return f.now })

	_, err := authenticator.Login(t.Context(), f.tenant.Slug, f.account.Email, f.password, f.code(t))
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("err = %v, want errors.Is(err, ErrInvalidCredentials)", err)
	}
}

func TestLocalAuthenticatorLoginRejectsADisabledAccount(t *testing.T) {
	t.Parallel()

	f := newLocalTestFixture(t)
	f.account.Status = storage.AccountStatusDisabled
	f.store.accounts[f.account.ID] = f.account

	authenticator := NewLocalAuthenticator(f.store, func() time.Time { return f.now })

	_, err := authenticator.Login(t.Context(), f.tenant.Slug, f.account.Email, f.password, f.code(t))
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("err = %v, want errors.Is(err, ErrInvalidCredentials)", err)
	}
}
