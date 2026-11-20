package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/account"
	"github.com/ostendorf-cloud/govue/session"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// loginFixture is one tenant with one active local account, and govue's
// account service over AccountStore, the path a password login takes.
type loginFixture struct {
	store    *fakeStore
	tenant   storage.Tenant
	account  storage.Account
	password string
	secret   string
	now      time.Time
	service  *account.Service
}

func newLoginFixture(t *testing.T, status storage.AccountStatus) loginFixture {
	t.Helper()

	const password = "correct horse battery staple"

	hasher := testHasher(t)
	hash, err := hasher.HashPassword(t.Context(), password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	secret, err := testTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}

	store := newFakeStore()
	tenant := store.addTenant(storage.Tenant{Slug: "acme", Name: "Acme"})
	acc := store.addAccount(storage.Account{
		TenantID:     tenant.ID,
		Email:        "owner@acme.example",
		PasswordHash: &hash,
		TOTPSecret:   &secret,
		Role:         storage.AccountRoleOwner,
		Status:       status,
	})

	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

	service, err := account.NewService(account.ServiceConfig{
		Store:       NewAccountStore(store),
		Hasher:      hasher,
		Issuer:      TOTPIssuer,
		EndSessions: func(context.Context, string) error { return nil },
		Logger:      slog.New(slog.DiscardHandler),
		Now:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	return loginFixture{
		store: store, tenant: tenant, account: acc, password: password,
		secret: secret, now: now, service: service,
	}
}

func (f loginFixture) code(t *testing.T) string {
	t.Helper()

	code, err := testTOTPCode(f.secret, f.now)
	if err != nil {
		t.Fatalf("TOTPCode: %v", err)
	}

	return code
}

func (f loginFixture) ctx(t *testing.T) context.Context {
	return ContextWithTenant(t.Context(), f.tenant.ID)
}

func TestLoginWithCorrectCredentialsThroughGovue(t *testing.T) {
	t.Parallel()

	f := newLoginFixture(t, storage.AccountStatusActive)

	id, err := f.service.Login(f.ctx(t), "owner@acme.example", f.password, f.code(t))
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if id.ID != f.account.ID.String() {
		t.Errorf("Identity.ID = %q, want the accounts.id %q", id.ID, f.account.ID)
	}
	if id.Login != "owner@acme.example" {
		t.Errorf("Identity.Login = %q, want the account's email", id.Login)
	}

	// Every store call ran scoped to the fixture's tenant, never another
	// one and never unscoped.
	for _, scope := range f.store.scopes {
		if scope != f.tenant.ID {
			t.Errorf("a store call ran in tenant %s, want only %s", scope, f.tenant.ID)
		}
	}
	if len(f.store.scopes) == 0 {
		t.Error("no store call ran inside WithTenant")
	}
}

func TestLoginIsCaseInsensitiveOnTheEmail(t *testing.T) {
	t.Parallel()

	f := newLoginFixture(t, storage.AccountStatusActive)

	if _, err := f.service.Login(f.ctx(t), "Owner@ACME.example", f.password, f.code(t)); err != nil {
		t.Fatalf("Login with the email in another letter case: %v", err)
	}
}

// TestLoginFailsIndistinguishably carries over the old LocalAuthenticator
// test: every refusal is the one account.ErrInvalidCredentials, whichever
// part of the guess was wrong, an unknown tenant included.
func TestLoginFailsIndistinguishably(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		unknown  bool
		email    string
		password func(f loginFixture) string
		code     func(t *testing.T, f loginFixture) string
	}{
		{
			name:     "unknown tenant",
			unknown:  true,
			email:    "owner@acme.example",
			password: func(f loginFixture) string { return f.password },
			code:     func(t *testing.T, f loginFixture) string { return f.code(t) },
		},
		{
			name:     "unknown email",
			email:    "nobody@acme.example",
			password: func(f loginFixture) string { return f.password },
			code:     func(t *testing.T, f loginFixture) string { return f.code(t) },
		},
		{
			name:     "wrong password",
			email:    "owner@acme.example",
			password: func(loginFixture) string { return "not the right password" },
			code:     func(t *testing.T, f loginFixture) string { return f.code(t) },
		},
		{
			name:     "wrong totp code",
			email:    "owner@acme.example",
			password: func(f loginFixture) string { return f.password },
			code:     func(*testing.T, loginFixture) string { return "000000" },
		},
		{
			name:     "missing totp code",
			email:    "owner@acme.example",
			password: func(f loginFixture) string { return f.password },
			code:     func(*testing.T, loginFixture) string { return "" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newLoginFixture(t, storage.AccountStatusActive)
			ctx := f.ctx(t)
			if tt.unknown {
				ctx = ContextWithUnknownTenant(t.Context())
			}

			_, err := f.service.Login(ctx, tt.email, tt.password(f), tt.code(t, f))
			if !errors.Is(err, account.ErrInvalidCredentials) {
				t.Errorf("err = %v, want account.ErrInvalidCredentials", err)
			}
		})
	}
}

// TestLoginForAnUnknownTenantStillRunsTheLookup: the lookup of an
// unknown tenant runs in a scope no tenant has, the nil UUID, so it costs
// the same transaction as a known one, and its result is discarded.
func TestLoginForAnUnknownTenantStillRunsTheLookup(t *testing.T) {
	t.Parallel()

	f := newLoginFixture(t, storage.AccountStatusActive)

	_, err := f.service.Login(ContextWithUnknownTenant(t.Context()), f.account.Email, f.password, f.code(t))
	if !errors.Is(err, account.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want account.ErrInvalidCredentials, although the fake finds the account in any scope", err)
	}

	f.store.mu.Lock()
	defer f.store.mu.Unlock()

	if len(f.store.scopes) != 1 || f.store.scopes[0] != uuid.Nil {
		t.Errorf("scopes = %v, want exactly one lookup in the nil scope", f.store.scopes)
	}
}

func TestLoginRejectsAPendingOrDisabledAccount(t *testing.T) {
	t.Parallel()

	for _, status := range []storage.AccountStatus{storage.AccountStatusPending, storage.AccountStatusDisabled} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()

			f := newLoginFixture(t, status)

			_, err := f.service.Login(f.ctx(t), "owner@acme.example", f.password, f.code(t))
			if !errors.Is(err, account.ErrInvalidCredentials) {
				t.Errorf("err = %v, want account.ErrInvalidCredentials", err)
			}
		})
	}
}

// TestLoginRejectsAReplayedTOTPCode: RFC 6238 Section 5.2, a code is good
// once. govue consumes it through AccountStore.AdvanceTOTPCounter.
func TestLoginRejectsAReplayedTOTPCode(t *testing.T) {
	t.Parallel()

	f := newLoginFixture(t, storage.AccountStatusActive)
	code := f.code(t)

	if _, err := f.service.Login(f.ctx(t), "owner@acme.example", f.password, code); err != nil {
		t.Fatalf("first Login: %v", err)
	}

	_, err := f.service.Login(f.ctx(t), "owner@acme.example", f.password, code)
	if !errors.Is(err, account.ErrInvalidCredentials) {
		t.Errorf("second Login with the same totp code: err = %v, want account.ErrInvalidCredentials", err)
	}
}

// TestLoginWithoutATenantScopeFailsClosed: a wiring mistake that forgets
// the tenant is a failure, never an unscoped lookup and never a refusal
// that looks like a wrong password.
func TestLoginWithoutATenantScopeFailsClosed(t *testing.T) {
	t.Parallel()

	f := newLoginFixture(t, storage.AccountStatusActive)

	_, err := f.service.Login(t.Context(), "owner@acme.example", f.password, f.code(t))
	if !errors.Is(err, ErrNoTenant) {
		t.Errorf("err = %v, want ErrNoTenant", err)
	}
	if len(f.store.scopes) != 0 {
		t.Errorf("WithTenant ran %d times without a tenant on the context, want 0", len(f.store.scopes))
	}
}

func newTestSessions(t *testing.T) *session.Manager {
	t.Helper()

	sessions, err := session.New(session.Config{
		Store:          session.NewMemoryStore(nil, 100),
		AllowPlainHTTP: true,
	})
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}

	return sessions
}

func TestStartSessionCarriesTenantAndRole(t *testing.T) {
	t.Parallel()

	f := newLoginFixture(t, storage.AccountStatusActive)
	sessions := newTestSessions(t)

	login, err := NewLogin(LoginConfig{
		Store:    f.store,
		Sessions: sessions,
		Hasher:   testHasher(t),
		Logger:   slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("NewLogin: %v", err)
	}

	req := httptest.NewRequestWithContext(f.ctx(t), http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()

	if err := login.StartSession(req.Context(), rec, req, account.Identity{ID: f.account.ID.String(), Login: f.account.Email}); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	// StartSession first ends the session the request carried, which
	// writes a clearing cookie of the same name. The browser keeps the
	// last one.
	var sessionCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessions.CookieName() {
			sessionCookie = c
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("StartSession set no session cookie")
	}

	check := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	check.AddCookie(sessionCookie)

	started, ok, err := sessions.Read(t.Context(), check)
	if err != nil || !ok {
		t.Fatalf("Read: ok %v, err %v", ok, err)
	}
	if started.Subject != f.account.ID.String() {
		t.Errorf("Subject = %q, want the accounts.id", started.Subject)
	}
	if started.Data[tenantIDDataKey] != f.tenant.ID.String() {
		t.Errorf("Data[tenant_id] = %q, want %q", started.Data[tenantIDDataKey], f.tenant.ID)
	}
	if started.Data[roleDataKey] != string(storage.AccountRoleOwner) {
		t.Errorf("Data[role] = %q, want owner", started.Data[roleDataKey])
	}
}

func TestStartSessionRefusesAnAccountThatIsNoLongerActive(t *testing.T) {
	t.Parallel()

	f := newLoginFixture(t, storage.AccountStatusDisabled)

	login, err := NewLogin(LoginConfig{
		Store:    f.store,
		Sessions: newTestSessions(t),
		Hasher:   testHasher(t),
		Logger:   slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("NewLogin: %v", err)
	}

	req := httptest.NewRequestWithContext(f.ctx(t), http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()

	err = login.StartSession(req.Context(), rec, req, account.Identity{ID: f.account.ID.String(), Login: f.account.Email})
	if !errors.Is(err, ErrAccountNotActive) {
		t.Errorf("err = %v, want ErrAccountNotActive", err)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Value != "" {
			t.Errorf("cookie %q was set for an account that is not active", c.Name)
		}
	}
}

func TestOtherFactors(t *testing.T) {
	t.Parallel()

	hash, secret, subject := "$argon2id$hash", "SECRET", "oidc-subject"
	zero, one := int64(0), int64(1)

	tests := []struct {
		name    string
		account storage.Account
		want    int
	}{
		{"nothing", storage.Account{}, 0},
		{"password and confirmed totp", storage.Account{PasswordHash: &hash, TOTPSecret: &secret, TOTPLastCounter: &one}, 1},
		{"password and a null counter, an adopted account", storage.Account{PasswordHash: &hash, TOTPSecret: &secret}, 1},
		{"password and unconfirmed totp", storage.Account{PasswordHash: &hash, TOTPSecret: &secret, TOTPLastCounter: &zero}, 0},
		{"password without totp", storage.Account{PasswordHash: &hash}, 0},
		{"oidc only", storage.Account{OIDCSubject: &subject}, 1},
		{"both", storage.Account{PasswordHash: &hash, TOTPSecret: &secret, OIDCSubject: &subject}, 2},
	}

	for _, tt := range tests {
		if got := OtherFactors(tt.account); got != tt.want {
			t.Errorf("%s: OtherFactors = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestTenantFromPath(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	tenant := store.addTenant(storage.Tenant{Slug: "acme", Name: "Acme"})

	tests := []struct {
		slug      string
		wantKnown bool
	}{
		{"acme", true},
		{"no-such-tenant", false},
	}

	for _, tt := range tests {
		var (
			got   uuid.UUID
			known bool
		)

		mux := http.NewServeMux()
		mux.Handle("POST /t/{tenant}/login", TenantFromPath(store, "tenant", http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got, known = TenantFromContext(r.Context())
		})))

		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/t/"+tt.slug+"/login", nil))

		if known != tt.wantKnown {
			t.Errorf("slug %q: known = %v, want %v", tt.slug, known, tt.wantKnown)
		}
		if tt.wantKnown && got != tenant.ID {
			t.Errorf("slug %q: tenant = %s, want %s", tt.slug, got, tenant.ID)
		}
	}
}
