package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ostendorf-cloud/govue/csrf"
	"github.com/ostendorf-cloud/govue/health"
	"github.com/ostendorf-cloud/govue/logging"
	"github.com/ostendorf-cloud/govue/session"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// testApp is what buildTestApp hands every test: the public handler under
// test, its admin counterpart (see httpapi.Handlers), plus the pieces a
// test needs to build a fixture or check a session.
type testApp struct {
	handler      http.Handler
	adminHandler http.Handler
	store        *fakeStore
	csrfKey      []byte
	// repos is what the repository hooks set, so a feature test can type
	// assert its own fake back out of it.
	repos httpapi.Repositories
	// handlers is what NewHandler returned, for a test that inspects it
	// through export_test.go.
	handlers httpapi.Handlers
}

// buildTestApp builds a real internal/httpapi handler over a fakeStore, so
// a test exercises the actual LocalAuthenticator and InviteManager logic,
// the actual middleware chain and the actual routing, with no database.
// rateLimitBurst lets a test request a tiny bucket to exercise the rate
// limiter itself; every other test wants a bucket large enough to never
// trip.
func buildTestApp(t *testing.T, rateLimitBurst int) testApp {
	t.Helper()

	store := newFakeStore()
	csrfKey := fixedKey(t, 1)

	sessions, err := session.New(session.Config{
		Store:          session.NewMemoryStore(nil, 100),
		AllowPlainHTTP: true,
	})
	if err != nil {
		t.Fatalf("session.New() error = %v", err)
	}

	distFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><html></html>")},
	}

	healthRegistry := health.New()
	healthRegistry.Add("fixture", 0, func(context.Context) error { return nil })

	// Every feature's fakes, registered by the init of its own
	// fake_<resource>_test.go: see testhooks_test.go. Scope is the fake
	// store's own WithTenant, set before the hooks run, so a feature's
	// handlers open their tenant scope exactly as in production.
	repos := httpapi.Repositories{Scope: store}
	for _, hook := range repositoryHooks {
		hook(t, &repos)
	}

	handlers, err := httpapi.NewHandler(httpapi.Deps{
		Logger:         logging.New(io.Discard, slog.LevelError),
		Store:          store,
		Sessions:       sessions,
		CSRFKey:        csrfKey,
		Login:          newTestLogin(t, store, sessions),
		Invites:        newTestInvites(t, store),
		HealthRegistry: healthRegistry,
		RateLimitBurst: rateLimitBurst,
		RateLimitPer:   time.Minute,
		// The login limits follow the general one, so a test asking for a
		// tiny bucket gets tiny login limits as well, and every other test
		// gets limits that never trip.
		LoginLimits: httpapi.LoginLimits{
			ClientBurst:  rateLimitBurst,
			LoginBurst:   rateLimitBurst,
			CeilingBurst: rateLimitBurst,
		},
		DefaultInviteTTL: time.Hour,
		DistFS:           distFS,
		Repositories:     repos,
	})
	if err != nil {
		t.Fatalf("httpapi.NewHandler() error = %v", err)
	}

	return testApp{handler: handlers.Public, adminHandler: handlers.Admin, store: store, csrfKey: csrfKey, repos: repos, handlers: handlers}
}

// fixedKey returns a deterministic 32 byte key, seed changing only the
// first byte, so two calls in the same test never collide.
func fixedKey(t *testing.T, seed byte) []byte {
	t.Helper()

	key := make([]byte, 32)
	key[0] = seed

	return key
}

// doJSON performs one request against h and returns the recorded response.
func doJSON(t *testing.T, h http.Handler, method, path string, body any, cookies []*http.Cookie, csrfToken string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req := httptest.NewRequestWithContext(context.Background(), method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	if csrfToken != "" {
		req.Header.Set(csrf.HeaderName, csrfToken)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

// sessionCookie returns the one cookie rec's response set, for a test that
// just logged in or accepted an invite.
func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("response set %d cookies, want exactly 1", len(cookies))
	}

	return cookies[0]
}

// TestPublicHandlerServesNoAdminEndpoint is this issue's own acceptance
// criterion, proven by a test rather than by inspection: anything that can
// reach the application's own Ingress-routed listener must get a plain
// 404 for /metrics, /healthz and /readyz, never the real answer. The 404
// comes from httpapi.go's own explicit registrations
// (mux.Handle(path, route(api.NotFoundHandler()))), not from the SPA
// catch-all: those three paths are registered patterns, so http.ServeMux
// dispatches to the explicit handler ahead of "/", the same precedence
// TestSPAFallback's "unknown api route" case relies on for /api/. Without
// that registration the catch-all would answer instead, with a 200 and
// the SPA shell, the exact "looks like the endpoint still exists"
// outcome this split exists to prevent.
func TestPublicHandlerServesNoAdminEndpoint(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)

	for _, path := range []string{"/metrics", "/healthz", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			rec := doJSON(t, app.handler, http.MethodGet, path, nil, nil, "")
			if rec.Code != http.StatusNotFound {
				t.Errorf("GET %s on the public handler status = %d, want %d", path, rec.Code, http.StatusNotFound)
			}
		})
	}
}

// TestPublicHandlerServesNoAdminEndpointForEveryMethodAndSubpath proves the
// 404 registration is not GET-only and not exact-path-only: a non-GET
// method and a path nested under one of the three must both still get the
// explicit 404, never the 200 the SPA catch-all would otherwise answer
// with for an unregistered pattern.
func TestPublicHandlerServesNoAdminEndpointForEveryMethodAndSubpath(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "post metrics", method: http.MethodPost, path: "/metrics"},
		{name: "put readyz", method: http.MethodPut, path: "/readyz"},
		{name: "metrics trailing slash", method: http.MethodGet, path: "/metrics/"},
		{name: "path under healthz", method: http.MethodGet, path: "/healthz/anything"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := doJSON(t, app.handler, tt.method, tt.path, nil, nil, "")
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s %s on the public handler status = %d, want %d", tt.method, tt.path, rec.Code, http.StatusNotFound)
			}
		})
	}
}

// TestAdminHandlerServesHealthAndMetrics is the other half of this issue's
// acceptance criteria: the administrative handler, which cmd/solobill
// serves on its own listener through govue's httpserver.AdminConfig,
// answers all three.
func TestAdminHandlerServesHealthAndMetrics(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{name: "liveness", path: "/healthz", wantStatus: http.StatusOK},
		{name: "readiness", path: "/readyz", wantStatus: http.StatusOK},
		{name: "metrics", path: "/metrics", wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := doJSON(t, app.adminHandler, http.MethodGet, tt.path, nil, nil, "")
			if rec.Code != tt.wantStatus {
				t.Errorf("GET %s status = %d, want %d", tt.path, rec.Code, tt.wantStatus)
			}
		})
	}
}

// TestAdminHandlerReportsPublicTraffic proves the admin handler's /metrics
// and the public handler's own httpMetrics.Middleware share one registry:
// a request the public handler actually served shows up once /metrics is
// scraped on the admin handler, the same series a Prometheus scrape of a
// single-listener deployment would have seen before this split.
func TestAdminHandlerReportsPublicTraffic(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)

	doJSON(t, app.handler, http.MethodGet, "/", nil, nil, "")

	rec := doJSON(t, app.adminHandler, http.MethodGet, "/metrics", nil, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "http_requests_total") {
		t.Errorf("metrics body does not contain http_requests_total: %s", rec.Body.String())
	}
}

func TestReadinessFailsWhenACheckFails(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	healthRegistry := health.New()
	healthRegistry.Add("database", 0, func(context.Context) error { return errors.New("connection refused") })
	sessions := mustSessions(t)

	handlers, err := httpapi.NewHandler(httpapi.Deps{
		Logger:   logging.New(io.Discard, slog.LevelError),
		Store:    store,
		Sessions: sessions,
		CSRFKey:  fixedKey(t, 2),
		Login:    newTestLogin(t, store, sessions),
		Invites:  newTestInvites(t, store),

		HealthRegistry:   healthRegistry,
		RateLimitBurst:   100,
		RateLimitPer:     time.Minute,
		DefaultInviteTTL: time.Hour,
		DistFS:           fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	})
	if err != nil {
		t.Fatalf("httpapi.NewHandler() error = %v", err)
	}

	rec := doJSON(t, handlers.Admin, http.MethodGet, "/readyz", nil, nil, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /readyz status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

// TestAdminReadyzReflectsDrain proves the claim cmd/solobill's main.go
// makes in its BeforeShutdown comment: draining the health.Registry it
// hands to HealthRegistry makes the admin listener's own /readyz answer
// 503, even with every check still passing. This is the one part of that
// comment invisible from the chart, since the registry is shared with
// main rather than owned by this package.
func TestAdminReadyzReflectsDrain(t *testing.T) {
	t.Parallel()

	healthRegistry := health.New()
	healthRegistry.Add("fixture", 0, func(context.Context) error { return nil })

	store := newFakeStore()
	sessions := mustSessions(t)

	handlers, err := httpapi.NewHandler(httpapi.Deps{
		Logger:           logging.New(io.Discard, slog.LevelError),
		Store:            store,
		Sessions:         sessions,
		CSRFKey:          fixedKey(t, 4),
		Login:            newTestLogin(t, store, sessions),
		Invites:          newTestInvites(t, store),
		HealthRegistry:   healthRegistry,
		RateLimitBurst:   100,
		RateLimitPer:     time.Minute,
		DefaultInviteTTL: time.Hour,
		DistFS:           fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	})
	if err != nil {
		t.Fatalf("httpapi.NewHandler() error = %v", err)
	}

	before := doJSON(t, handlers.Admin, http.MethodGet, "/readyz", nil, nil, "")
	if before.Code != http.StatusOK {
		t.Fatalf("GET /readyz before Drain() status = %d, want %d", before.Code, http.StatusOK)
	}

	healthRegistry.Drain()

	after := doJSON(t, handlers.Admin, http.MethodGet, "/readyz", nil, nil, "")
	if after.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /readyz after Drain() status = %d, want %d, even though the fixture check still passes", after.Code, http.StatusServiceUnavailable)
	}
}

// brokenSessionStore is a session.Store whose Get always fails with
// something other than session.ErrNotFound, for TestSessionStoreFailure
// Unavailable: a store outage, not an ordinary "no such session".
type brokenSessionStore struct{}

func (brokenSessionStore) Get(context.Context, string) (session.Session, error) {
	return session.Session{}, errors.New("store: connection refused")
}
func (brokenSessionStore) Save(context.Context, session.Session) error { return nil }
func (brokenSessionStore) Delete(context.Context, string) error        { return nil }
func (brokenSessionStore) DeleteSubject(context.Context, string) error { return nil }

// TestSessionStoreFailureIsExposedAsAStaticMessage proves that handleMe and
// requireOwnerSession's api.Unavailable("session store is unavailable")
// actually reaches the client: both mark it Exposed, the only way a 503
// survives api.WriteError's usual collapse to the generic internal-error
// shape, since govue v0.3.0 added api.Error.Exposed.
func TestSessionStoreFailureIsExposedAsAStaticMessage(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	sessions, err := session.New(session.Config{Store: brokenSessionStore{}, AllowPlainHTTP: true})
	if err != nil {
		t.Fatalf("session.New() error = %v", err)
	}

	handlers, err := httpapi.NewHandler(httpapi.Deps{
		Logger:           logging.New(io.Discard, slog.LevelError),
		Store:            store,
		Sessions:         sessions,
		CSRFKey:          fixedKey(t, 3),
		Login:            newTestLogin(t, store, sessions),
		Invites:          newTestInvites(t, store),
		HealthRegistry:   health.New(),
		RateLimitBurst:   100,
		RateLimitPer:     time.Minute,
		DefaultInviteTTL: time.Hour,
		DistFS:           fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	})
	if err != nil {
		t.Fatalf("httpapi.NewHandler() error = %v", err)
	}

	cookie := &http.Cookie{Name: sessions.CookieName(), Value: "whatever-the-store-will-refuse"}
	rec := doJSON(t, handlers.Public, http.MethodGet, "/api/v1/me", nil, []*http.Cookie{cookie}, "")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}

	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.Error.Message != "session store is unavailable" {
		t.Errorf("message = %q, want the deliberately exposed static message", body.Error.Message)
	}
}

func mustSessions(t *testing.T) *session.Manager {
	t.Helper()

	sessions, err := session.New(session.Config{Store: session.NewMemoryStore(nil, 100), AllowPlainHTTP: true})
	if err != nil {
		t.Fatalf("session.New() error = %v", err)
	}

	return sessions
}

// seedActiveAccount creates a tenant and an active local account with a
// known password and TOTP secret, returning both plus the account itself.
func seedActiveAccount(t *testing.T, store *fakeStore) (storage.Tenant, string) {
	t.Helper()

	tenant := store.addTenant(storage.Tenant{Slug: "acme", Name: "acme"})

	hash := hashPassword(t, ownerPassword)
	secret := totpSecret(t)

	store.addAccount(storage.Account{
		TenantID:     tenant.ID,
		Email:        ownerEmail,
		PasswordHash: &hash,
		TOTPSecret:   &secret,
		Role:         storage.AccountRoleOwner,
		Status:       storage.AccountStatusActive,
	})

	return tenant, secret
}

// ownerEmail and ownerPassword are the fixture every test that needs one
// active owner account uses: seedActiveAccount, loginAndCSRF's callers and
// the table driven login cases below all share this one fixed account
// rather than each inventing their own.
const (
	ownerEmail    = "owner@acme.test"
	ownerPassword = "correct-password"
)

func TestLogin(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	tenant, totpSecret := seedActiveAccount(t, app.store)

	code := totpCode(t, totpSecret)

	tests := []struct {
		name       string
		tenantSlug string
		body       map[string]string
		wantStatus int
	}{
		{
			name:       "valid credentials",
			tenantSlug: tenant.Slug,
			body:       loginBody("owner@acme.test", "correct-password", code),
			wantStatus: http.StatusOK,
		},
		{
			name:       "wrong password",
			tenantSlug: tenant.Slug,
			body:       loginBody("owner@acme.test", "wrong", code),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "unknown tenant",
			tenantSlug: "no-such-tenant",
			body:       loginBody("owner@acme.test", "correct-password", code),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "unknown email",
			tenantSlug: tenant.Slug,
			body:       loginBody("nobody@acme.test", "correct-password", code),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong totp code",
			tenantSlug: tenant.Slug,
			body:       loginBody("owner@acme.test", "correct-password", "000000"),
			wantStatus: http.StatusUnauthorized,
		},
		{
			// govue's decoder refuses a field it does not name: the old
			// body shape with tenant_slug no longer logs in.
			name:       "unknown body field",
			tenantSlug: tenant.Slug,
			body: map[string]string{
				"login": "owner@acme.test", "password": "correct-password", "code": code, "tenant_slug": tenant.Slug,
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := doJSON(t, app.handler, http.MethodPost, loginPath(tt.tenantSlug), tt.body, nil, "")
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d, body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus == http.StatusOK {
				if len(rec.Result().Cookies()) != 1 {
					t.Error("a successful login must set exactly one cookie")
				}
			} else if len(rec.Result().Cookies()) != 0 {
				t.Error("a refused login must set no cookie")
			}
		})
	}
}

// TestLoginRefusalsAreOneAnswer: every refused login answers the same
// status and the same body, whichever part of the guess was wrong, an
// unknown tenant included. The old handler answered one fixed 401 for the
// same reason.
func TestLoginRefusalsAreOneAnswer(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	tenant, totpSecret := seedActiveAccount(t, app.store)
	code := totpCode(t, totpSecret)

	refusals := []struct {
		slug string
		body map[string]string
	}{
		{tenant.Slug, loginBody("owner@acme.test", "wrong-password", code)},
		{"no-such-tenant", loginBody("owner@acme.test", "correct-password", code)},
		{tenant.Slug, loginBody("nobody@acme.test", "correct-password", code)},
		{tenant.Slug, loginBody("owner@acme.test", "correct-password", "000000")},
	}

	var want string
	for i, r := range refusals {
		rec := doJSON(t, app.handler, http.MethodPost, loginPath(r.slug), r.body, nil, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("refusal %d: status = %d, want %d", i, rec.Code, http.StatusUnauthorized)
		}

		var body struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		got := body.Error.Code + "|" + body.Error.Message
		if i == 0 {
			want = got
		} else if got != want {
			t.Errorf("refusal %d answered %q, want the same answer as the first, %q", i, got, want)
		}
	}
}

func TestMeAndLogout(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	tenant, totpSecret := seedActiveAccount(t, app.store)

	t.Run("unauthenticated", func(t *testing.T) {
		t.Parallel()

		rec := doJSON(t, app.handler, http.MethodGet, "/api/v1/me", nil, nil, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	code := totpCode(t, totpSecret)

	loginRec := doJSON(t, app.handler, http.MethodPost, loginPath(tenant.Slug),
		loginBody("owner@acme.test", "correct-password", code), nil, "")
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d", loginRec.Code, http.StatusOK)
	}
	cookie := sessionCookie(t, loginRec)
	token := csrf.Token(app.csrfKey, cookie.Value)

	// These three subtests share the one session the login above started
	// and run in this declared order, not in parallel with each other:
	// the last of them ends the session the first two still need alive.
	t.Run("authenticated", func(t *testing.T) {
		rec := doJSON(t, app.handler, http.MethodGet, "/api/v1/me", nil, []*http.Cookie{cookie}, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}

		var body struct {
			Email     string `json:"email"`
			Role      string `json:"role"`
			TenantID  string `json:"tenant_id"`
			CSRFToken string `json:"csrf_token"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if body.Email != "owner@acme.test" {
			t.Errorf("email = %q", body.Email)
		}
		if body.Role != string(storage.AccountRoleOwner) {
			t.Errorf("role = %q, want %q", body.Role, storage.AccountRoleOwner)
		}
		if body.TenantID != tenant.ID.String() {
			t.Errorf("tenant_id = %q, want %q", body.TenantID, tenant.ID.String())
		}
		if body.CSRFToken != token {
			t.Error("csrf_token in the response does not match csrf.Token derived the same way")
		}
	})

	t.Run("logout without a csrf token is refused", func(t *testing.T) {
		rec := doJSON(t, app.handler, http.MethodPost, "/api/v1/auth/logout", nil, []*http.Cookie{cookie}, "")
		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("logout ends the session", func(t *testing.T) {
		rec := doJSON(t, app.handler, http.MethodPost, "/api/v1/auth/logout", nil, []*http.Cookie{cookie}, token)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("logout status = %d, want %d, body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
		}

		meRec := doJSON(t, app.handler, http.MethodGet, "/api/v1/me", nil, []*http.Cookie{cookie}, "")
		if meRec.Code != http.StatusUnauthorized {
			t.Errorf("GET /api/v1/me after logout status = %d, want %d", meRec.Code, http.StatusUnauthorized)
		}
	})
}

func TestCreateInvite(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	ownerTenant, ownerSecret := seedActiveAccount(t, app.store)
	_, memberSecret, _ := seedMember(t, app.store, ownerTenant, "member@acme.test", "correct-password")

	ownerCookie, ownerToken := loginAndCSRF(t, app, ownerTenant.Slug, "owner@acme.test", "correct-password", ownerSecret)
	memberCookie, memberToken := loginAndCSRF(t, app, ownerTenant.Slug, "member@acme.test", "correct-password", memberSecret)

	tests := []struct {
		name       string
		cookie     *http.Cookie
		token      string
		body       map[string]string
		wantStatus int
	}{
		{
			name:       "no session",
			body:       map[string]string{"email": "new@acme.test", "role": "member"},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "member may not invite",
			cookie:     memberCookie,
			token:      memberToken,
			body:       map[string]string{"email": "new@acme.test", "role": "member"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "owner may invite",
			cookie:     ownerCookie,
			token:      ownerToken,
			body:       map[string]string{"email": "new@acme.test", "role": "member"},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "invalid role",
			cookie:     ownerCookie,
			token:      ownerToken,
			body:       map[string]string{"email": "new2@acme.test", "role": "superuser"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty email",
			cookie:     ownerCookie,
			token:      ownerToken,
			body:       map[string]string{"email": "", "role": "member"},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var cookies []*http.Cookie
			if tt.cookie != nil {
				cookies = []*http.Cookie{tt.cookie}
			}

			rec := doJSON(t, app.handler, http.MethodPost, "/api/v1/invites", tt.body, cookies, tt.token)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d, body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus == http.StatusCreated {
				var body struct {
					Token string `json:"token"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("unmarshal response: %v", err)
				}
				if body.Token == "" {
					t.Error("created invite carries no token")
				}
			}
		})
	}
}

func TestInviteTOTPSecretAndAccept(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	ownerTenant, ownerSecret := seedActiveAccount(t, app.store)
	ownerCookie, ownerToken := loginAndCSRF(t, app, ownerTenant.Slug, "owner@acme.test", "correct-password", ownerSecret)

	inviteRec := doJSON(t, app.handler, http.MethodPost, "/api/v1/invites", map[string]string{
		"email": "new@acme.test", "role": "member",
	}, []*http.Cookie{ownerCookie}, ownerToken)
	if inviteRec.Code != http.StatusCreated {
		t.Fatalf("create invite status = %d, want %d", inviteRec.Code, http.StatusCreated)
	}
	var inviteBody struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(inviteRec.Body.Bytes(), &inviteBody); err != nil {
		t.Fatalf("unmarshal invite response: %v", err)
	}

	secretRec := doJSON(t, app.handler, http.MethodPost, "/api/v1/invites/totp-secret", map[string]string{
		"tenant_slug": ownerTenant.Slug, "token": inviteBody.Token,
	}, nil, "")
	if secretRec.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/invites/totp-secret status = %d, want %d, body = %s", secretRec.Code, http.StatusOK, secretRec.Body.String())
	}
	var secretBody struct {
		Secret          string `json:"secret"`
		ProvisioningURI string `json:"provisioning_uri"`
	}
	if err := json.Unmarshal(secretRec.Body.Bytes(), &secretBody); err != nil {
		t.Fatalf("unmarshal totp secret response: %v", err)
	}
	if secretBody.Secret == "" || secretBody.ProvisioningURI == "" {
		t.Fatal("totp secret response carries an empty secret or provisioning uri")
	}

	code := totpCode(t, secretBody.Secret)

	t.Run("totp-secret for an unknown tenant", func(t *testing.T) {
		rec := doJSON(t, app.handler, http.MethodPost, "/api/v1/invites/totp-secret", map[string]string{
			"tenant_slug": "no-such-tenant", "token": inviteBody.Token,
		}, nil, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
		}
	})

	t.Run("totp-secret for an unknown token", func(t *testing.T) {
		rec := doJSON(t, app.handler, http.MethodPost, "/api/v1/invites/totp-secret", map[string]string{
			"tenant_slug": ownerTenant.Slug, "token": "not-a-real-token",
		}, nil, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})

	tests := []struct {
		name       string
		body       map[string]string
		wantStatus int
	}{
		{
			name: "unknown tenant",
			body: map[string]string{
				"tenant_slug": "no-such-tenant", "token": inviteBody.Token,
				"password": "a-new-password", "totp_code": code,
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "password too short",
			body: map[string]string{
				"tenant_slug": ownerTenant.Slug, "token": inviteBody.Token,
				"password": "short11char", "totp_code": code,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "password too long",
			body: map[string]string{
				"tenant_slug": ownerTenant.Slug, "token": inviteBody.Token,
				"password": strings.Repeat("a", 129), "totp_code": code,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "wrong totp code",
			body: map[string]string{
				"tenant_slug": ownerTenant.Slug, "token": inviteBody.Token,
				"password": "a-new-password", "totp_code": "000000",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "attacker-supplied secret and matching code is ignored",
			body: func() map[string]string {
				secret, code := attackerSecretAndCode(t)

				return map[string]string{
					"tenant_slug": ownerTenant.Slug, "token": inviteBody.Token,
					"password": "a-new-password", "totp_secret": secret, "totp_code": code,
				}
			}(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid token",
			body: map[string]string{
				"tenant_slug": ownerTenant.Slug, "token": "not-a-real-token",
				"password": "a-new-password", "totp_code": code,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "accept succeeds",
			body: map[string]string{
				"tenant_slug": ownerTenant.Slug, "token": inviteBody.Token,
				"password": "a-new-password", "totp_code": code,
			},
			wantStatus: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doJSON(t, app.handler, http.MethodPost, "/api/v1/invites/accept", tt.body, nil, "")
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d, body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus == http.StatusNoContent && len(rec.Result().Cookies()) != 1 {
				t.Error("a successful accept must start a session")
			}
		})
	}
}

// attackerSecretAndCode builds a self-consistent secret/code pair the
// server never issued, to prove it is ignored: acceptInviteRequest has no
// totp_secret field any more, so that body field is simply extra JSON the
// decoder drops, and only the real, server-issued secret the invite's own
// totp-secret request already attached to it can ever confirm the invite.
func attackerSecretAndCode(t *testing.T) (secret, code string) {
	t.Helper()

	secret = totpSecret(t)

	return secret, totpCode(t, secret)
}

func TestRateLimitedRoutes(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1)
	tenant, totpSecret := seedActiveAccount(t, app.store)
	code := totpCode(t, totpSecret)

	body := loginBody("owner@acme.test", "wrong", code)

	first := doJSON(t, app.handler, http.MethodPost, loginPath(tenant.Slug), body, nil, "")
	if first.Code != http.StatusUnauthorized {
		t.Fatalf("first attempt status = %d, want %d", first.Code, http.StatusUnauthorized)
	}

	second := doJSON(t, app.handler, http.MethodPost, loginPath(tenant.Slug), body, nil, "")
	if second.Code != http.StatusTooManyRequests {
		t.Errorf("second attempt status = %d, want %d", second.Code, http.StatusTooManyRequests)
	}
}

func TestSPAFallback(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)

	tests := []struct {
		name        string
		path        string
		wantStatus  int
		wantContent string
	}{
		{name: "root", path: "/", wantStatus: http.StatusOK, wantContent: "text/html"},
		{name: "unknown client route", path: "/invoices/42", wantStatus: http.StatusOK, wantContent: "text/html"},
		{name: "unknown api route", path: "/api/v1/does-not-exist", wantStatus: http.StatusNotFound, wantContent: "application/json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := doJSON(t, app.handler, http.MethodGet, tt.path, nil, nil, "")
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Content-Type"); !strings.Contains(got, tt.wantContent) {
				t.Errorf("Content-Type = %q, want containing %q", got, tt.wantContent)
			}
		})
	}
}

func TestNewHandlerRequiresItsDependencies(t *testing.T) {
	t.Parallel()

	_, err := httpapi.NewHandler(httpapi.Deps{})
	if err == nil {
		t.Fatal("NewHandler() error = nil, want an error for every field missing")
	}
}

// seedMember creates a second account in tenant with the member role and a
// known password and TOTP secret.
func seedMember(t *testing.T, store *fakeStore, tenant storage.Tenant, email, password string) (storage.Tenant, string, storage.Account) {
	t.Helper()

	hash := hashPassword(t, password)
	secret := totpSecret(t)

	account := store.addAccount(storage.Account{
		TenantID:     tenant.ID,
		Email:        email,
		PasswordHash: &hash,
		TOTPSecret:   &secret,
		Role:         storage.AccountRoleMember,
		Status:       storage.AccountStatusActive,
	})

	return tenant, secret, account
}

// loginAndCSRF logs in and returns the resulting session cookie plus the
// CSRF token a subsequent unsafe request on this app must carry.
func loginAndCSRF(t *testing.T, app testApp, tenantSlug, email, password, totpSecret string) (*http.Cookie, string) {
	t.Helper()

	rec := doJSON(t, app.handler, http.MethodPost, loginPath(tenantSlug),
		loginBody(email, password, totpCode(t, totpSecret)), nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	cookie := sessionCookie(t, rec)

	return cookie, csrf.Token(app.csrfKey, cookie.Value)
}
