package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/session"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// sessionCookieFor starts a session for acc and returns its cookie.
func sessionCookieFor(t *testing.T, sessions *session.Manager, acc storage.Account) *http.Cookie {
	t.Helper()

	rec := httptest.NewRecorder()
	if _, err := sessions.Start(t.Context(), rec, IdentityFromAccount(acc, "", nil).Session()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	for _, c := range rec.Result().Cookies() {
		if c.Name == sessions.CookieName() && c.Value != "" {
			return c
		}
	}

	t.Fatal("no session cookie")

	return nil
}

// otherTenantRequest is a request that names another tenant every way a
// client could try after login: a header, a query parameter and a path
// value. None of them may reach the scope.
func otherTenantRequest(t *testing.T, target string, body string, other storage.Tenant, cookie *http.Cookie) *http.Request {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, target+"?tenant="+other.Slug, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant", other.Slug)
	req.Header.Set("X-Tenant-ID", other.ID.String())
	req.SetPathValue("tenant", other.Slug)
	if cookie != nil {
		req.AddCookie(cookie)
	}

	return req
}

func TestTenantFromSessionTakesTheTenantOnlyFromTheSession(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	tenant := store.addTenant(storage.Tenant{Slug: "acme", Name: "Acme"})
	other := store.addTenant(storage.Tenant{Slug: "other", Name: "Other"})
	acc := store.addAccount(storage.Account{TenantID: tenant.ID, Email: "a@acme.example", Role: storage.AccountRoleOwner, Status: storage.AccountStatusActive})
	sessions := newTestSessions(t)

	var (
		got   uuid.UUID
		known bool
	)
	h := TenantFromSession(sessions, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, known = TenantFromContext(r.Context())
	}))

	h.ServeHTTP(httptest.NewRecorder(), otherTenantRequest(t, "/", "{}", other, sessionCookieFor(t, sessions, acc)))
	if !known || got != tenant.ID {
		t.Errorf("scope = %s (known %v), want the session's tenant %s, never the one the request names", got, known, tenant.ID)
	}

	got, known = uuid.Nil, false
	h.ServeHTTP(httptest.NewRecorder(), otherTenantRequest(t, "/", "{}", other, nil))
	if known {
		t.Errorf("a request without a session got the scope %s, want none", got)
	}
}

func TestTenantFromSessionLeavesAStoreFailureUnscoped(t *testing.T) {
	t.Parallel()

	sessions, err := session.New(session.Config{Store: failingSessionStore{}, AllowPlainHTTP: true})
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}

	var known bool
	h := TenantFromSession(sessions, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, known = TenantFromContext(r.Context())
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessions.CookieName(), Value: "anything"})
	h.ServeHTTP(httptest.NewRecorder(), req)

	if known {
		t.Error("a session store failure produced a tenant scope, want none")
	}
}

type failingSessionStore struct{}

func (failingSessionStore) Get(context.Context, string) (session.Session, error) {
	return session.Session{}, errors.New("store down")
}
func (failingSessionStore) Save(context.Context, session.Session) error { return nil }
func (failingSessionStore) Delete(context.Context, string) error        { return nil }
func (failingSessionStore) DeleteSubject(context.Context, string) error { return nil }

// TestSessionRoutesRunOnlyInTheSessionsTenant drives govue's password
// change and passkey list behind TenantFromSession, the way
// internal/httpapi mounts them, with a request that names another tenant
// in its header, query and path, and checks every store call ran in the
// session's tenant.
func TestSessionRoutesRunOnlyInTheSessionsTenant(t *testing.T) {
	t.Parallel()

	f := newLoginFixture(t, storage.AccountStatusActive)
	other := f.store.addTenant(storage.Tenant{Slug: "other", Name: "Other"})
	sessions := newTestSessions(t)

	login, err := NewLogin(LoginConfig{
		Store:    f.store,
		Sessions: sessions,
		Hasher:   testHasher(t),
		Logger:   slog.New(slog.DiscardHandler),
		Passkey:  &PasskeyConfig{RPID: "localhost", RPDisplayName: "test", Origins: []string{"http://localhost"}, AllowPlainHTTP: true},
	})
	if err != nil {
		t.Fatalf("NewLogin: %v", err)
	}

	cookie := sessionCookieFor(t, sessions, f.account)

	// The passkey list first: the password change ends every session.
	for _, route := range []struct {
		name string
		h    http.Handler
	}{
		{"passkey list", TenantFromSession(sessions, login.Passkeys.List())},
		{"password change", TenantFromSession(sessions, login.Accounts.ChangePassword())},
	} {
		name, h := route.name, route.h

		f.store.mu.Lock()
		f.store.scopes = nil
		f.store.mu.Unlock()

		body := `{"current_password":"` + f.password + `","new_password":"another long passphrase"}`
		req := otherTenantRequest(t, "/", body, other, cookie)
		if name == "passkey list" {
			req.Method = http.MethodGet
		}

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d, want 200, body %s", name, rec.Code, rec.Body.String())
		}

		f.store.mu.Lock()
		scopes := append([]uuid.UUID(nil), f.store.scopes...)
		f.store.mu.Unlock()

		if len(scopes) == 0 {
			t.Errorf("%s: no store call ran inside WithTenant", name)
		}
		for _, s := range scopes {
			if s != f.tenant.ID {
				t.Errorf("%s: a store call ran in tenant %s, want only the session's %s", name, s, f.tenant.ID)
			}
		}
	}

	// The password change went through in the session's tenant.
	if _, err := f.service.Login(f.ctx(t), f.account.Email, "another long passphrase", f.code(t)); err != nil {
		t.Errorf("Login with the changed password: %v", err)
	}
}
