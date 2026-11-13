package httpapi_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ostendorf-cloud/govue/health"
	"github.com/ostendorf-cloud/govue/logging"
	"github.com/ostendorf-cloud/govue/oidc"
	"github.com/ostendorf-cloud/govue/session"

	"github.com/ostendorf-cloud/solobill/internal/auth"
	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// buildTestAppWithOIDC is buildTestApp plus a non-nil OIDCHandlers and
// LoginCodec, the two Deps fields handleInviteOIDCStart requires (see
// NewHandler's "must both be set or both be nil" check): the fixture
// never actually drives handlers.Login() or handlers.Callback(), see
// internal/auth's own oidc_test.go for the end-to-end callback test, it
// only needs a real, non-nil *oidc.Handlers for the OIDC route group
// (oidc/start included) to be mounted at all.
func buildTestAppWithOIDC(t *testing.T) (testApp, *auth.InviteManager) {
	t.Helper()

	store := newFakeStore()

	sessions, err := session.New(session.Config{
		Store:          session.NewMemoryStore(nil),
		AllowPlainHTTP: true,
	})
	if err != nil {
		t.Fatalf("session.New() error = %v", err)
	}

	loginCodec, err := session.NewCodec(session.CodecConfig{
		Key:            fixedKey(t, 2),
		AllowPlainHTTP: true,
	})
	if err != nil {
		t.Fatalf("session.NewCodec() error = %v", err)
	}

	provider, err := auth.NewOIDCProvider(auth.OIDCProviderConfig{
		Issuer:       "http://127.0.0.1:1/realms/solobill",
		ClientID:     "solobill",
		ClientSecret: "a-test-client-secret",
		RedirectURL:  "http://127.0.0.1:1/auth/oidc/callback",
	})
	if err != nil {
		t.Fatalf("auth.NewOIDCProvider() error = %v", err)
	}

	invites := auth.NewInviteManager(store, nil)

	oidcHandlers, err := oidc.NewHandlers(oidc.HandlerConfig{
		Provider: provider,
		Logins:   loginCodec,
		StartSession: func(context.Context, http.ResponseWriter, *http.Request, oidc.Identity) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("oidc.NewHandlers() error = %v", err)
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
		LoginCodec:       loginCodec,
		CSRFKey:          fixedKey(t, 3),
		LocalAuth:        auth.NewLocalAuthenticator(store, nil),
		Invites:          invites,
		OIDCHandlers:     oidcHandlers,
		HealthRegistry:   healthRegistry,
		AllowPlainHTTP:   true,
		RateLimitBurst:   100,
		RateLimitPer:     time.Minute,
		DefaultInviteTTL: time.Hour,
		DistFS:           distFS,
	})
	if err != nil {
		t.Fatalf("httpapi.NewHandler() error = %v", err)
	}

	return testApp{handler: handler, store: store, csrfKey: fixedKey(t, 3)}, invites
}

// TestInviteOIDCStart covers handleInviteOIDCStart: a valid, pending
// invite gets a pending-invite cookie and a 204; an unknown, expired or
// already-used token is refused before any cookie is set, so the
// browser is never sent to the identity provider for nothing.
func TestInviteOIDCStart(t *testing.T) {
	t.Parallel()

	app, invites := buildTestAppWithOIDC(t)
	tenant := app.store.addTenant(storage.Tenant{Slug: "acme", Name: "Acme"})

	rawToken, _, err := invites.CreateInvite(t.Context(), auth.CreateInviteParams{
		TenantID: tenant.ID,
		Email:    "invitee@acme.test",
		Role:     storage.AccountRoleMember,
		TTL:      time.Hour,
	})
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	tests := []struct {
		name       string
		body       map[string]string
		wantStatus int
		wantCookie bool
	}{
		{
			name:       "valid pending invite",
			body:       map[string]string{"tenant_slug": tenant.Slug, "token": rawToken},
			wantStatus: http.StatusNoContent,
			wantCookie: true,
		},
		{
			name:       "unknown token",
			body:       map[string]string{"tenant_slug": tenant.Slug, "token": "not-the-right-token"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown tenant",
			body:       map[string]string{"tenant_slug": "no-such-tenant", "token": rawToken},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "empty tenant slug",
			body:       map[string]string{"tenant_slug": "", "token": rawToken},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := doJSON(t, app.handler, http.MethodPost, "/api/v1/invites/oidc/start", tt.body, nil, "")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}

			cookies := rec.Result().Cookies()
			if tt.wantCookie && len(cookies) != 1 {
				t.Errorf("response set %d cookies, want exactly 1", len(cookies))
			}
			if !tt.wantCookie && len(cookies) != 0 {
				t.Errorf("response set %d cookies, want none", len(cookies))
			}
		})
	}

	// The invite is still unused: handleInviteOIDCStart is advisory only,
	// it never marks anything used, AcceptInviteOIDC does.
	if err := invites.PeekInvite(t.Context(), tenant.ID, rawToken); err != nil {
		t.Errorf("PeekInvite after start: %v, want the invite still pending", err)
	}
}

// TestInviteOIDCStartNotFoundWhenOIDCDisabled proves the route is not
// even mounted when OIDC is disabled: the same "oidc is not configured"
// shape a bare buildTestApp fixture (no OIDCHandlers, no LoginCodec)
// already answers for every other OIDC-only path.
func TestInviteOIDCStartNotFoundWhenOIDCDisabled(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)

	rec := doJSON(t, app.handler, http.MethodPost, "/api/v1/invites/oidc/start",
		map[string]string{"tenant_slug": "acme", "token": "whatever"}, nil, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
