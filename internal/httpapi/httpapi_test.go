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

	"github.com/ostendorf-cloud/solobill/internal/auth"
	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// testApp is what buildTestApp hands every test: the handler under test
// plus the pieces a test needs to build a fixture or check a session.
type testApp struct {
	handler http.Handler
	store   *fakeStore
	csrfKey []byte
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

	handler, err := httpapi.NewHandler(httpapi.Deps{
		Logger:           logging.New(io.Discard, slog.LevelError),
		Store:            store,
		Sessions:         sessions,
		CSRFKey:          csrfKey,
		LocalAuth:        auth.NewLocalAuthenticator(store, nil),
		Invites:          auth.NewInviteManager(store, nil),
		HealthRegistry:   healthRegistry,
		RateLimitBurst:   rateLimitBurst,
		RateLimitPer:     time.Minute,
		DefaultInviteTTL: time.Hour,
		DistFS:           distFS,
	})
	if err != nil {
		t.Fatalf("httpapi.NewHandler() error = %v", err)
	}

	return testApp{handler: handler, store: store, csrfKey: csrfKey}
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

func TestHealthAndMetricsEndpoints(t *testing.T) {
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

			rec := doJSON(t, app.handler, http.MethodGet, tt.path, nil, nil, "")
			if rec.Code != tt.wantStatus {
				t.Errorf("GET %s status = %d, want %d", tt.path, rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestReadinessFailsWhenACheckFails(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	healthRegistry := health.New()
	healthRegistry.Add("database", 0, func(context.Context) error { return errors.New("connection refused") })

	handler, err := httpapi.NewHandler(httpapi.Deps{
		Logger:    logging.New(io.Discard, slog.LevelError),
		Store:     store,
		Sessions:  mustSessions(t),
		CSRFKey:   fixedKey(t, 2),
		LocalAuth: auth.NewLocalAuthenticator(store, nil),
		Invites:   auth.NewInviteManager(store, nil),

		HealthRegistry:   healthRegistry,
		RateLimitBurst:   100,
		RateLimitPer:     time.Minute,
		DefaultInviteTTL: time.Hour,
		DistFS:           fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	})
	if err != nil {
		t.Fatalf("httpapi.NewHandler() error = %v", err)
	}

	rec := doJSON(t, handler, http.MethodGet, "/readyz", nil, nil, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /readyz status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
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

	handler, err := httpapi.NewHandler(httpapi.Deps{
		Logger:           logging.New(io.Discard, slog.LevelError),
		Store:            store,
		Sessions:         sessions,
		CSRFKey:          fixedKey(t, 3),
		LocalAuth:        auth.NewLocalAuthenticator(store, nil),
		Invites:          auth.NewInviteManager(store, nil),
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
	rec := doJSON(t, handler, http.MethodGet, "/api/v1/me", nil, []*http.Cookie{cookie}, "")

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

	hash, err := auth.HashPassword(ownerPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret() error = %v", err)
	}

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

	code, err := auth.GenerateTOTPCode(totpSecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateTOTPCode() error = %v", err)
	}

	tests := []struct {
		name       string
		body       map[string]string
		wantStatus int
	}{
		{
			name: "valid credentials",
			body: map[string]string{
				"tenant_slug": tenant.Slug, "email": "owner@acme.test", "password": "correct-password", "totp_code": code,
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "wrong password",
			body: map[string]string{
				"tenant_slug": tenant.Slug, "email": "owner@acme.test", "password": "wrong", "totp_code": code,
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "unknown tenant",
			body: map[string]string{
				"tenant_slug": "no-such-tenant", "email": "owner@acme.test", "password": "correct-password", "totp_code": code,
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "unknown email",
			body: map[string]string{
				"tenant_slug": tenant.Slug, "email": "nobody@acme.test", "password": "correct-password", "totp_code": code,
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "wrong totp code",
			body: map[string]string{
				"tenant_slug": tenant.Slug, "email": "owner@acme.test", "password": "correct-password", "totp_code": "000000",
			},
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := doJSON(t, app.handler, http.MethodPost, "/api/v1/auth/login", tt.body, nil, "")
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d, body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus == http.StatusNoContent {
				if len(rec.Result().Cookies()) != 1 {
					t.Error("a successful login must set exactly one cookie")
				}
			}
		})
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

	code, err := auth.GenerateTOTPCode(totpSecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateTOTPCode() error = %v", err)
	}

	loginRec := doJSON(t, app.handler, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"tenant_slug": tenant.Slug, "email": "owner@acme.test", "password": "correct-password", "totp_code": code,
	}, nil, "")
	if loginRec.Code != http.StatusNoContent {
		t.Fatalf("login status = %d, want %d", loginRec.Code, http.StatusNoContent)
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

	code, err := auth.GenerateTOTPCode(secretBody.Secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateTOTPCode() error = %v", err)
	}

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

	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret() error = %v", err)
	}

	code, err = auth.GenerateTOTPCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateTOTPCode() error = %v", err)
	}

	return secret, code
}

func TestRateLimitedRoutes(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1)
	tenant, totpSecret := seedActiveAccount(t, app.store)
	code, err := auth.GenerateTOTPCode(totpSecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateTOTPCode() error = %v", err)
	}

	body := map[string]string{
		"tenant_slug": tenant.Slug, "email": "owner@acme.test", "password": "wrong", "totp_code": code,
	}

	first := doJSON(t, app.handler, http.MethodPost, "/api/v1/auth/login", body, nil, "")
	if first.Code != http.StatusUnauthorized {
		t.Fatalf("first attempt status = %d, want %d", first.Code, http.StatusUnauthorized)
	}

	second := doJSON(t, app.handler, http.MethodPost, "/api/v1/auth/login", body, nil, "")
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

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret() error = %v", err)
	}

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

	code, err := auth.GenerateTOTPCode(totpSecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateTOTPCode() error = %v", err)
	}

	rec := doJSON(t, app.handler, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"tenant_slug": tenantSlug, "email": email, "password": password, "totp_code": code,
	}, nil, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("login status = %d, want %d, body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	cookie := sessionCookie(t, rec)

	return cookie, csrf.Token(app.csrfKey, cookie.Value)
}
