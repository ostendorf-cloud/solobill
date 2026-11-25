//go:build e2e

// Package e2e proves the login session and the CSRF contract through the
// routing of the two images: a browser talks to one origin, the frontend
// answers the app, and /api reaches the API. It runs against a running
// stack, never in "go test ./...": the e2e build tag keeps it out, and
// "task test:e2e" runs it.
//
// E2E_BASE_URL is the origin the browser would open, the frontend of
// "docker compose up --build" by default (http://localhost:8080), or an
// Ingress. E2E_DATABASE_URL is a superuser connection to the same database,
// used once to create a local account with a password and a TOTP secret
// in the tenant E2E_TENANT_SLUG (default demo, the compose seed): nothing
// in the API creates a first local account on its own. The account is
// created fresh on every run, with a random e-mail address.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ostendorf-cloud/govue/account"
	"github.com/ostendorf-cloud/govue/csrf"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres"
)

// password is the fake password of the account this test creates.
const password = "e2e-only-not-a-real-password-4711"

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}

// seedAccount creates an active owner account with a password and a TOTP
// secret and returns its e-mail address and secret.
func seedAccount(t *testing.T, ctx context.Context, databaseURL, tenantSlug string) (string, string) {
	t.Helper()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect E2E_DATABASE_URL: %v", err)
	}
	t.Cleanup(pool.Close)
	store := postgres.NewStore(pool)

	tenant, err := store.Tenants().FindBySlug(ctx, tenantSlug)
	if err != nil {
		t.Fatalf("find tenant %q: %v", tenantSlug, err)
	}

	hasher, err := account.NewHasher(account.HasherConfig{})
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	hash, err := hasher.HashPassword(ctx, password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	secret, err := account.GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}

	email := "e2e-" + uuid.NewString() + "@example.invalid"
	err = store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
		_, err := store.Accounts().Create(ctx, storage.Account{
			TenantID:     tenant.ID,
			Email:        email,
			PasswordHash: &hash,
			TOTPSecret:   &secret,
			Role:         storage.AccountRoleOwner,
			Status:       storage.AccountStatusActive,
		})

		return err
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	return email, secret
}

// browser sends requests the way a browser on base does: one cookie jar,
// and Origin and Sec-Fetch-Site on every request it makes from the page.
type browser struct {
	t      *testing.T
	base   *url.URL
	client *http.Client
}

func (b browser) do(method, path string, body any, header http.Header) (*http.Response, []byte) {
	b.t.Helper()

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			b.t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(b.t.Context(), method, b.base.JoinPath(path).String(), reader)
	if err != nil {
		b.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", b.base.Scheme+"://"+b.base.Host)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for k, v := range header {
		req.Header[k] = v
	}

	res, err := b.client.Do(req)
	if err != nil {
		b.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		b.t.Fatal(err)
	}

	return res, data
}

// TestLoginAndCSRFThroughTheRouting logs in, reads the session with
// GET /api/v1/me and sends a CSRF protected POST, all on the one origin the
// frontend serves. A POST without the token is refused, and the app's
// paths answer the app, not the API.
func TestLoginAndCSRFThroughTheRouting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	databaseURL := os.Getenv("E2E_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("E2E_DATABASE_URL is required: a superuser connection to the stack's database")
	}
	base, err := url.Parse(envOr("E2E_BASE_URL", "http://localhost:8080"))
	if err != nil {
		t.Fatalf("E2E_BASE_URL: %v", err)
	}
	tenantSlug := envOr("E2E_TENANT_SLUG", "demo")
	email, secret := seedAccount(t, ctx, databaseURL, tenantSlug)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	b := browser{t: t, base: base, client: &http.Client{Jar: jar, Timeout: 30 * time.Second}}

	// The frontend answers the app on every route of its router.
	for _, path := range []string{"/", "/documents/42", "/fixed-assets"} {
		res, body := b.do(http.MethodGet, path, nil, nil)
		if res.StatusCode != http.StatusOK || !strings.Contains(res.Header.Get("Content-Type"), "text/html") ||
			!strings.Contains(string(body), `id="app"`) {
			t.Fatalf("GET %s = %d %s, want the app's index.html", path, res.StatusCode, res.Header.Get("Content-Type"))
		}
		if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
			t.Errorf("GET %s CSP = %q, want script-src 'self'", path, csp)
		}
	}

	// Not logged in yet: the API answers, with JSON.
	if res, _ := b.do(http.MethodGet, "/api/v1/me", nil, nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/me before login = %d, want %d", res.StatusCode, http.StatusUnauthorized)
	}

	code, err := account.TOTPCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	res, body := b.do(http.MethodPost, "/api/v1/tenants/"+tenantSlug+"/auth/login",
		map[string]string{"login": email, "password": password, "code": code}, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login = %d %s, want %d", res.StatusCode, body, http.StatusOK)
	}
	if len(jar.Cookies(base)) == 0 {
		t.Fatal("login set no cookie the browser keeps for the frontend's origin")
	}

	res, body = b.do(http.MethodGet, "/api/v1/me", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/me after login = %d %s, want %d", res.StatusCode, body, http.StatusOK)
	}
	var me struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(body, &me); err != nil || me.CSRFToken == "" {
		t.Fatalf("GET /api/v1/me body has no csrf_token: %v", err)
	}

	// A CSRF protected POST without the token is refused, the session
	// survives it.
	if res, _ := b.do(http.MethodPost, "/api/v1/auth/logout", nil, nil); res.StatusCode != http.StatusForbidden {
		t.Errorf("POST /api/v1/auth/logout without the token = %d, want %d", res.StatusCode, http.StatusForbidden)
	}
	if res, _ := b.do(http.MethodGet, "/api/v1/me", nil, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/me after the refused POST = %d, want %d", res.StatusCode, http.StatusOK)
	}

	// With the token the POST goes through and ends the session.
	res, body = b.do(http.MethodPost, "/api/v1/auth/logout", nil, http.Header{csrf.HeaderName: {me.CSRFToken}})
	if res.StatusCode >= http.StatusBadRequest {
		t.Fatalf("POST /api/v1/auth/logout with the token = %d %s, want success", res.StatusCode, body)
	}
	if res, _ := b.do(http.MethodGet, "/api/v1/me", nil, nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /api/v1/me after logout = %d, want %d", res.StatusCode, http.StatusUnauthorized)
	}
}
