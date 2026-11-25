package httpapi_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/api"
	"github.com/ostendorf-cloud/govue/health"
	"github.com/ostendorf-cloud/govue/logging"
	"github.com/ostendorf-cloud/govue/oidc"
	"github.com/ostendorf-cloud/govue/session"

	"github.com/ostendorf-cloud/solobill/internal/auth"
	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// The OIDC routes as a browser and the identity provider see them. They
// are spelled out here rather than read from the package, so a change of
// the routes has to change this test too.
const (
	oidcLoginRoute    = "/api/v1/auth/oidc/login"
	oidcCallbackRoute = "/api/v1/auth/oidc/callback"
	oidcClientID      = "solobill"
	oidcTenantSlug    = "acme"
)

// fakeIdP is an identity provider shaped like a Keycloak realm, served
// over a real loopback server. Its authorization endpoint logs in
// whichever subject the test set with loginAs and redirects back to the
// redirect_uri it was given, but only when that is the registered one,
// the way Keycloak refuses an unregistered redirect URL. Its token
// endpoint checks the code, the redirect_uri and the PKCE verifier
// before it signs an ID token for the nonce the authorization request
// carried.
type fakeIdP struct {
	t      *testing.T
	server *httptest.Server
	key    *rsa.PrivateKey

	mu         sync.Mutex
	registered string
	subject    string
	codes      map[string]issuedCode
}

// issuedCode is what the authorization endpoint remembers about one code.
type issuedCode struct {
	redirectURI string
	challenge   string
	nonce       string
	subject     string
}

const fakeIdPKeyID = "fake-idp-key-1"

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	idp := &fakeIdP{t: t, key: key, codes: map[string]issuedCode{}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /realms/solobill/.well-known/openid-configuration", idp.discovery)
	mux.HandleFunc("GET /realms/solobill/protocol/openid-connect/certs", idp.jwks)
	mux.HandleFunc("GET /realms/solobill/protocol/openid-connect/auth", idp.authorize)
	mux.HandleFunc("POST /realms/solobill/protocol/openid-connect/token", idp.token)

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)

	return idp
}

func (idp *fakeIdP) issuer() string { return idp.server.URL + "/realms/solobill" }

// register sets the one redirect URL the client registration accepts.
func (idp *fakeIdP) register(redirectURL string) {
	idp.mu.Lock()
	defer idp.mu.Unlock()

	idp.registered = redirectURL
}

// loginAs sets the subject the next authorization request logs in as.
func (idp *fakeIdP) loginAs(subject string) {
	idp.mu.Lock()
	defer idp.mu.Unlock()

	idp.subject = subject
}

func (idp *fakeIdP) discovery(w http.ResponseWriter, _ *http.Request) {
	idp.writeJSON(w, map[string]string{
		"issuer":                 idp.issuer(),
		"authorization_endpoint": idp.issuer() + "/protocol/openid-connect/auth",
		"token_endpoint":         idp.issuer() + "/protocol/openid-connect/token",
		"jwks_uri":               idp.issuer() + "/protocol/openid-connect/certs",
		"end_session_endpoint":   idp.issuer() + "/protocol/openid-connect/logout",
	})
}

func (idp *fakeIdP) jwks(w http.ResponseWriter, _ *http.Request) {
	idp.writeJSON(w, map[string]any{
		"keys": []map[string]string{{
			"kid": fakeIdPKeyID,
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"n":   base64.RawURLEncoding.EncodeToString(idp.key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(idp.key.E)).Bytes()),
		}},
	})
}

func (idp *fakeIdP) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	idp.mu.Lock()
	defer idp.mu.Unlock()

	redirectURI := q.Get("redirect_uri")
	if q.Get("client_id") != oidcClientID || redirectURI != idp.registered {
		http.Error(w, "invalid redirect uri", http.StatusBadRequest)

		return
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("state") == "" || q.Get("nonce") == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)

		return
	}

	code := uuid.NewString()
	idp.codes[code] = issuedCode{
		redirectURI: redirectURI,
		challenge:   q.Get("code_challenge"),
		nonce:       q.Get("nonce"),
		subject:     idp.subject,
	}

	back, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "invalid redirect uri", http.StatusBadRequest)

		return
	}
	bq := back.Query()
	bq.Set("code", code)
	bq.Set("state", q.Get("state"))
	back.RawQuery = bq.Encode()

	http.Redirect(w, r, back.String(), http.StatusFound)
}

func (idp *fakeIdP) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)

		return
	}

	idp.mu.Lock()
	issued, ok := idp.codes[r.PostForm.Get("code")]
	delete(idp.codes, r.PostForm.Get("code"))
	idp.mu.Unlock()

	if !ok ||
		r.PostForm.Get("redirect_uri") != issued.redirectURI ||
		oidc.CodeChallenge(r.PostForm.Get("code_verifier")) != issued.challenge {
		w.WriteHeader(http.StatusBadRequest)
		idp.writeJSON(w, map[string]string{"error": "invalid_grant"})

		return
	}

	now := time.Now()
	idp.writeJSON(w, map[string]string{"id_token": idp.sign(map[string]any{
		"iss":                idp.issuer(),
		"sub":                issued.subject,
		"aud":                oidcClientID,
		"azp":                oidcClientID,
		"exp":                now.Add(5 * time.Minute).Unix(),
		"iat":                now.Unix(),
		"nonce":              issued.nonce,
		"preferred_username": "member",
		"email":              "member@acme.example",
		"email_verified":     true,
		"name":               "Acme Member",
		"realm_access":       map[string]any{"roles": []string{"member"}},
	})})
}

// sign builds an RS256 JWS in compact serialization.
func (idp *fakeIdP) sign(claims map[string]any) string {
	encode := func(v any) string {
		body, err := json.Marshal(v)
		if err != nil {
			idp.t.Errorf("marshal jwt part: %v", err)
		}

		return base64.RawURLEncoding.EncodeToString(body)
	}

	input := encode(map[string]string{"alg": "RS256", "typ": "JWT", "kid": fakeIdPKeyID}) + "." + encode(claims)
	digest := sha256.Sum256([]byte(input))

	signature, err := rsa.SignPKCS1v15(rand.Reader, idp.key, crypto.SHA256, digest[:])
	if err != nil {
		idp.t.Errorf("sign jwt: %v", err)
	}

	return input + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func (idp *fakeIdP) writeJSON(w http.ResponseWriter, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		idp.t.Errorf("marshal fake idp response: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body) // a failed write to the test client shows up as its own error.
}

// oidcFlow is solobill's public handler with OIDC wired the way
// cmd/solobill wires it, served over a real loopback server, plus a
// browser: a client with a cookie jar that does not follow redirects, so
// every hop is checked on its own. The jar applies each cookie's Path the
// way a browser does, which is what makes a cookie Path that does not
// cover the callback fail these tests.
type oidcFlow struct {
	idp     *fakeIdP
	server  *httptest.Server
	store   *fakeStore
	invites *auth.InviteManager
	tenant  storage.Tenant
	other   storage.Tenant
	browser *http.Client
}

func newOIDCFlow(t *testing.T) *oidcFlow {
	t.Helper()

	idp := newFakeIdP(t)

	// The handler is built after the server exists, because the redirect
	// URL names the server's own address.
	var public http.Handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		public.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)

	redirectURL := server.URL + oidcCallbackRoute
	idp.register(redirectURL)

	store := newFakeStore()
	tenant := store.addTenant(storage.Tenant{Slug: oidcTenantSlug, Name: "Acme"})
	other := store.addTenant(storage.Tenant{Slug: "globex", Name: "Globex"})

	sessions, err := session.New(session.Config{
		Store:          session.NewMemoryStore(nil, 100),
		AllowPlainHTTP: true,
	})
	if err != nil {
		t.Fatalf("session.New() error = %v", err)
	}

	loginCodec, err := session.NewCodec(session.CodecConfig{Key: fixedKey(t, 2), AllowPlainHTTP: true})
	if err != nil {
		t.Fatalf("session.NewCodec() error = %v", err)
	}

	provider, err := auth.NewOIDCProvider(auth.OIDCProviderConfig{
		Issuer:       idp.issuer(),
		ClientID:     oidcClientID,
		ClientSecret: "a-test-client-secret",
		RedirectURL:  redirectURL,
		HTTPClient:   idp.server.Client(),
	})
	if err != nil {
		t.Fatalf("auth.NewOIDCProvider() error = %v", err)
	}

	invites := newTestInvites(t, store)

	// The same resolver cmd/solobill's buildOIDCHandlers builds: every
	// OIDC login resolves to the one configured tenant.
	resolveTenant := func(r *http.Request) (uuid.UUID, error) {
		found, err := store.Tenants().FindBySlug(r.Context(), oidcTenantSlug)
		if err != nil {
			return uuid.Nil, err
		}

		return found.ID, nil
	}

	oidcHandlers, err := auth.NewOIDCHandlers(provider, loginCodec, sessions, store, invites, resolveTenant,
		auth.OIDCHandlerOptions{AllowPlainHTTP: true})
	if err != nil {
		t.Fatalf("auth.NewOIDCHandlers() error = %v", err)
	}

	healthRegistry := health.New()
	healthRegistry.Add("fixture", 0, func(context.Context) error { return nil })

	handlers, err := httpapi.NewHandler(httpapi.Deps{
		Logger:           logging.New(io.Discard, slog.LevelError),
		Store:            store,
		Sessions:         sessions,
		LoginCodec:       loginCodec,
		CSRFKey:          fixedKey(t, 3),
		Login:            newTestLogin(t, store, sessions),
		Invites:          invites,
		OIDCHandlers:     oidcHandlers,
		HealthRegistry:   healthRegistry,
		AllowPlainHTTP:   true,
		RateLimitBurst:   100,
		RateLimitPer:     time.Minute,
		DefaultInviteTTL: time.Hour,
		DistFS: fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<!doctype html><html></html>")},
		},
	})
	if err != nil {
		t.Fatalf("httpapi.NewHandler() error = %v", err)
	}
	public = handlers.Public

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}

	browser := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &oidcFlow{idp: idp, server: server, store: store, invites: invites, tenant: tenant, other: other, browser: browser}
}

// get sends one browser request and returns the status, the Location
// header and the body.
func (f *oidcFlow) get(t *testing.T, target string) (int, string, []byte) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	return f.do(t, req)
}

func (f *oidcFlow) do(t *testing.T, req *http.Request) (int, string, []byte) {
	t.Helper()

	resp, err := f.browser.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}

	return resp.StatusCode, resp.Header.Get("Location"), body
}

// startLogin runs the first two hops: solobill's login route redirects to
// the identity provider with the configured redirect URL, and the
// identity provider redirects back to it with a code and the state. It
// returns that callback URL, not yet requested.
func (f *oidcFlow) startLogin(t *testing.T, subject string) string {
	t.Helper()

	return f.startLoginNext(t, subject, "")
}

// startLoginNext is startLogin with a ?next= target on the login route.
// An empty next sends no parameter at all.
func (f *oidcFlow) startLoginNext(t *testing.T, subject, next string) string {
	t.Helper()

	f.idp.loginAs(subject)

	loginURL := f.server.URL + oidcLoginRoute
	if next != "" {
		loginURL += "?" + url.Values{"next": {next}}.Encode()
	}

	status, location, body := f.get(t, loginURL)
	if status != http.StatusSeeOther {
		t.Fatalf("GET %s status = %d, want %d, body = %s", oidcLoginRoute, status, http.StatusSeeOther, body)
	}

	authURL, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse authorization url: %v", err)
	}
	if !strings.HasPrefix(location, f.idp.issuer()+"/") {
		t.Fatalf("login redirected to %q, want the identity provider at %q", location, f.idp.issuer())
	}
	if got, want := authURL.Query().Get("redirect_uri"), f.server.URL+oidcCallbackRoute; got != want {
		t.Fatalf("redirect_uri = %q, want %q", got, want)
	}

	status, callback, body := f.get(t, location)
	if status != http.StatusFound {
		t.Fatalf("identity provider status = %d, want %d, body = %s", status, http.StatusFound, body)
	}

	return callback
}

// me answers GET /api/v1/me for the browser's current cookies.
func (f *oidcFlow) me(t *testing.T) (int, map[string]string) {
	t.Helper()

	status, _, body := f.get(t, f.server.URL+"/api/v1/me")
	if status != http.StatusOK {
		return status, nil
	}

	var got map[string]string
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode /api/v1/me: %v, body = %s", err, body)
	}

	return status, got
}

// startInvite posts the invite token to the OIDC invite start route,
// which sets the pending-invite cookie in the browser's jar.
func (f *oidcFlow) startInvite(t *testing.T, tenantSlug, rawToken string) {
	t.Helper()

	payload, err := json.Marshal(map[string]string{"tenant_slug": tenantSlug, "token": rawToken})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.server.URL+"/api/v1/invites/oidc/start", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	status, _, body := f.do(t, req)
	if status != http.StatusNoContent {
		t.Fatalf("POST /api/v1/invites/oidc/start status = %d, want %d, body = %s", status, http.StatusNoContent, body)
	}
}

func (f *oidcFlow) addOIDCAccount(tenantID uuid.UUID, subject string) storage.Account {
	return f.store.addAccount(storage.Account{
		TenantID:    tenantID,
		Email:       "member@acme.example",
		OIDCSubject: &subject,
		Role:        storage.AccountRoleMember,
		Status:      storage.AccountStatusActive,
	})
}

func (f *oidcFlow) createInvite(t *testing.T, tenantID uuid.UUID) (string, storage.Invite) {
	t.Helper()

	rawToken, invite, err := f.invites.CreateInvite(t.Context(), auth.CreateInviteParams{
		TenantID: tenantID,
		Email:    "invitee@acme.example",
		Role:     storage.AccountRoleMember,
		TTL:      time.Hour,
	})
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	return rawToken, invite
}

// TestOIDCLoginFlowOnAPIPaths drives the whole OIDC login through the
// public handler on /api/v1/auth/oidc: login, the identity provider, the
// callback, then a session /api/v1/me accepts.
func TestOIDCLoginFlowOnAPIPaths(t *testing.T) {
	t.Parallel()

	f := newOIDCFlow(t)
	const subject = "f:realm:member-1"
	f.addOIDCAccount(f.tenant.ID, subject)

	callback := f.startLogin(t, subject)

	status, location, body := f.get(t, callback)
	if status != http.StatusSeeOther {
		t.Fatalf("callback status = %d, want %d, body = %s", status, http.StatusSeeOther, body)
	}
	if location != "/" {
		t.Errorf("callback Location = %q, want %q", location, "/")
	}

	status, me := f.me(t)
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/me status = %d, want %d", status, http.StatusOK)
	}
	if me["tenant_id"] != f.tenant.ID.String() {
		t.Errorf("tenant_id = %q, want %q", me["tenant_id"], f.tenant.ID)
	}
	if me["role"] != string(storage.AccountRoleMember) {
		t.Errorf("role = %q, want %q", me["role"], storage.AccountRoleMember)
	}

	// The login cookie is single use: the same callback again finds no
	// login to finish.
	status, _, _ = f.get(t, callback)
	if status != http.StatusBadRequest {
		t.Errorf("replayed callback status = %d, want %d", status, http.StatusBadRequest)
	}
}

// TestOIDCLoginFlowNext sends ?next= through the login route: a local
// path is where the callback lands, anything that leaves this site lands
// on "/" instead, so the login is no open redirect.
func TestOIDCLoginFlowNext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "local path with a query", input: "/documents?status=draft", want: "/documents?status=draft"},
		{name: "absolute url", input: "https://evil.example/phish", want: "/"},
		{name: "protocol relative url", input: "//evil.example/phish", want: "/"},
		{name: "backslash protocol relative url", input: `/\evil.example/phish`, want: "/"},
		{name: "relative path", input: "documents", want: "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newOIDCFlow(t)
			const subject = "f:realm:member-1"
			f.addOIDCAccount(f.tenant.ID, subject)

			callback := f.startLoginNext(t, subject, tt.input)

			status, location, body := f.get(t, callback)
			if status != http.StatusSeeOther {
				t.Fatalf("callback status = %d, want %d, body = %s", status, http.StatusSeeOther, body)
			}
			if location != tt.want {
				t.Errorf("callback Location = %q, want %q", location, tt.want)
			}
		})
	}
}

// TestOIDCLoginFlowRefusals covers the callbacks that must not start a
// session: a state that does not match the browser's login, and an
// identity whose account lives in another tenant than the configured one.
func TestOIDCLoginFlowRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setup      func(t *testing.T, f *oidcFlow) string
		tamper     func(callback string) string
		wantStatus int
	}{
		{
			name: "state does not match the login",
			setup: func(_ *testing.T, f *oidcFlow) string {
				const subject = "f:realm:member-1"
				f.addOIDCAccount(f.tenant.ID, subject)

				return subject
			},
			tamper: func(callback string) string {
				u, _ := url.Parse(callback) // the identity provider built it from a parsed URL.
				q := u.Query()
				q.Set("state", "not-the-state-of-this-login-xxxxxxxxxxxxxxx")
				u.RawQuery = q.Encode()

				return u.String()
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "state missing",
			setup: func(_ *testing.T, f *oidcFlow) string {
				const subject = "f:realm:member-1"
				f.addOIDCAccount(f.tenant.ID, subject)

				return subject
			},
			tamper: func(callback string) string {
				u, _ := url.Parse(callback) // the identity provider built it from a parsed URL.
				q := u.Query()
				q.Del("state")
				u.RawQuery = q.Encode()

				return u.String()
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "account provisioned in another tenant only",
			setup: func(t *testing.T, f *oidcFlow) string {
				t.Helper()

				const subject = "f:realm:globex-owner"
				account := f.addOIDCAccount(f.other.ID, subject)

				// The account is really there, only not in the tenant
				// OIDC resolves to.
				found, err := f.store.Accounts().FindByOIDCSubject(t.Context(), subject)
				if err != nil || found.ID != account.ID || found.TenantID != f.other.ID {
					t.Fatalf("FindByOIDCSubject = %+v, %v, want the account in the other tenant", found, err)
				}

				return subject
			},
			tamper:     func(callback string) string { return callback },
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newOIDCFlow(t)
			subject := tt.setup(t, f)

			callback := f.startLogin(t, subject)

			status, _, body := f.get(t, tt.tamper(callback))
			if status != tt.wantStatus {
				t.Fatalf("callback status = %d, want %d, body = %s", status, tt.wantStatus, body)
			}

			if status, _ := f.me(t); status != http.StatusUnauthorized {
				t.Errorf("GET /api/v1/me after a refused callback status = %d, want %d", status, http.StatusUnauthorized)
			}
		})
	}
}

// TestOIDCInviteFlowOnAPIPaths redeems an invite through the OIDC login:
// the pending-invite cookie set by /api/v1/invites/oidc/start has to
// reach /api/v1/auth/oidc/callback, so a cookie Path that does not cover
// the callback leaves the invite unredeemed and fails this test.
func TestOIDCInviteFlowOnAPIPaths(t *testing.T) {
	t.Parallel()

	f := newOIDCFlow(t)
	rawToken, invite := f.createInvite(t, f.tenant.ID)
	const subject = "f:realm:brand-new-member"

	f.startInvite(t, f.tenant.Slug, rawToken)
	callback := f.startLogin(t, subject)

	status, _, body := f.get(t, callback)
	if status != http.StatusSeeOther {
		t.Fatalf("callback status = %d, want %d, body = %s", status, http.StatusSeeOther, body)
	}

	status, me := f.me(t)
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/me status = %d, want %d", status, http.StatusOK)
	}
	if me["email"] != invite.Email {
		t.Errorf("email = %q, want the invite's %q", me["email"], invite.Email)
	}
	if me["tenant_id"] != f.tenant.ID.String() {
		t.Errorf("tenant_id = %q, want %q", me["tenant_id"], f.tenant.ID)
	}

	stored, err := f.store.Invites().FindByID(t.Context(), invite.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if stored.UsedAt == nil {
		t.Error("the invite is still unused after the callback")
	}
}

// TestOIDCInviteFlowRefusesAnotherTenantsInvite starts an invite of
// another tenant and logs in through OIDC, which resolves to the
// configured tenant: the callback refuses, binds no subject and leaves
// the invite redeemable.
func TestOIDCInviteFlowRefusesAnotherTenantsInvite(t *testing.T) {
	t.Parallel()

	f := newOIDCFlow(t)
	rawToken, _ := f.createInvite(t, f.other.ID)
	const subject = "f:realm:cross-tenant"

	f.startInvite(t, f.other.Slug, rawToken)
	callback := f.startLogin(t, subject)

	status, _, body := f.get(t, callback)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("callback status = %d, want %d, body = %s", status, http.StatusServiceUnavailable, body)
	}

	if status, _ := f.me(t); status != http.StatusUnauthorized {
		t.Errorf("GET /api/v1/me status = %d, want %d", status, http.StatusUnauthorized)
	}
	if _, err := f.store.Accounts().FindByOIDCSubject(t.Context(), subject); err == nil {
		t.Error("the subject was bound to an account, want none")
	}
	if err := f.invites.PeekInvite(t.Context(), f.other.ID, rawToken); err != nil {
		t.Errorf("PeekInvite: %v, want the other tenant's invite still pending", err)
	}
}

// TestOIDCCookiesCoverTheCallback checks the cookies the browser carries
// to the callback directly: the login cookie and the pending-invite
// cookie each need a Path the callback path lies under, and SameSite=Lax,
// since the identity provider's redirect back is a cross site top level
// navigation that Strict withholds a cookie from.
func TestOIDCCookiesCoverTheCallback(t *testing.T) {
	t.Parallel()

	f := newOIDCFlow(t)
	rawToken, _ := f.createInvite(t, f.tenant.ID)

	login := httptest.NewRecorder()
	f.server.Config.Handler.ServeHTTP(login, httptest.NewRequestWithContext(t.Context(), http.MethodGet, oidcLoginRoute, nil))
	if login.Code != http.StatusSeeOther {
		t.Fatalf("GET %s status = %d, want %d, body = %s", oidcLoginRoute, login.Code, http.StatusSeeOther, login.Body.String())
	}

	invite := doJSON(t, f.server.Config.Handler, http.MethodPost, "/api/v1/invites/oidc/start",
		map[string]string{"tenant_slug": f.tenant.Slug, "token": rawToken}, nil, "")
	if invite.Code != http.StatusNoContent {
		t.Fatalf("POST /api/v1/invites/oidc/start status = %d, want %d, body = %s", invite.Code, http.StatusNoContent, invite.Body.String())
	}

	tests := []struct {
		name string
		rec  *httptest.ResponseRecorder
	}{
		{name: "login cookie", rec: login},
		{name: "pending-invite cookie", rec: invite},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cookies := tt.rec.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("response set %d cookies, want exactly 1", len(cookies))
			}

			for _, c := range cookies {
				if c.MaxAge < 0 || c.Value == "" {
					t.Errorf("cookie %q is a deletion, want a live value", c.Name)
				}
				if !cookiePathMatches(c.Path, oidcCallbackRoute) {
					t.Errorf("cookie %q Path = %q, does not cover %s", c.Name, c.Path, oidcCallbackRoute)
				}
				if c.SameSite != http.SameSiteLaxMode {
					t.Errorf("cookie %q SameSite = %v, want Lax", c.Name, c.SameSite)
				}
				if !c.HttpOnly {
					t.Errorf("cookie %q is not HttpOnly", c.Name)
				}
			}
		})
	}
}

// cookiePathMatches is the path-match of RFC 6265 section 5.1.4: whether
// a browser sends a cookie with cookiePath on a request for requestPath.
// An empty Path defaults to the directory of the request that set it,
// which is never what a cookie meant for another route wants.
func cookiePathMatches(cookiePath, requestPath string) bool {
	switch {
	case cookiePath == "":
		return false
	case cookiePath == requestPath:
		return true
	case !strings.HasPrefix(requestPath, cookiePath):
		return false
	default:
		return strings.HasSuffix(cookiePath, "/") || requestPath[len(cookiePath)] == '/'
	}
}

// TestLegacyOIDCPathsAnswerJSONNotFound proves the old paths fail loudly:
// an identity provider still sending the browser to /auth/oidc/callback
// gets a JSON 404, never the SPA shell, with OIDC on and off.
func TestLegacyOIDCPathsAnswerJSONNotFound(t *testing.T) {
	t.Parallel()

	withOIDC, _ := buildTestAppWithOIDC(t)
	withoutOIDC := buildTestApp(t, 100)

	apps := []struct {
		name    string
		handler http.Handler
	}{
		{name: "oidc enabled", handler: withOIDC.handler},
		{name: "oidc disabled", handler: withoutOIDC.handler},
	}

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "login", method: http.MethodGet, path: "/auth/oidc/login"},
		{name: "callback", method: http.MethodGet, path: "/auth/oidc/callback?code=a-code&state=a-state"},
		{name: "callback head", method: http.MethodHead, path: "/auth/oidc/callback"},
		{name: "callback post", method: http.MethodPost, path: "/auth/oidc/callback"},
		{name: "bare prefix", method: http.MethodGet, path: "/auth/oidc"},
		{name: "path below the prefix", method: http.MethodGet, path: "/auth/oidc/anything/else"},
	}

	for _, app := range apps {
		for _, tt := range tests {
			t.Run(app.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()

				rec := doJSON(t, app.handler, tt.method, tt.path, nil, nil, "")
				if rec.Code != http.StatusNotFound {
					t.Fatalf("%s %s status = %d, want %d", tt.method, tt.path, rec.Code, http.StatusNotFound)
				}
				if rec.Header().Get("Location") != "" {
					t.Errorf("Location = %q, want no redirect", rec.Header().Get("Location"))
				}
				if tt.method == http.MethodHead {
					return
				}

				var body struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("body is not JSON: %v, body = %s", err, rec.Body.String())
				}
				if body.Error.Code != api.CodeNotFound {
					t.Errorf("error code = %q, want %q", body.Error.Code, api.CodeNotFound)
				}
			})
		}
	}
}

// TestAuthMethods covers GET /api/v1/auth/methods, which the login view
// reads before any session exists to decide whether it shows the single
// sign-on link.
func TestAuthMethods(t *testing.T) {
	t.Parallel()

	withOIDC, _ := buildTestAppWithOIDC(t)
	withoutOIDC := buildTestApp(t, 100)

	tests := []struct {
		name    string
		handler http.Handler
		want    bool
	}{
		{name: "oidc enabled", handler: withOIDC.handler, want: true},
		{name: "oidc disabled", handler: withoutOIDC.handler, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// No session cookie: the login view has none yet.
			rec := doJSON(t, tt.handler, http.MethodGet, "/api/v1/auth/methods", nil, nil, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want %q", got, "no-store")
			}

			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v, body = %s", err, rec.Body.String())
			}
			if len(body) != 1 || body["oidc"] != tt.want {
				t.Errorf("body = %v, want exactly {\"oidc\": %v}", body, tt.want)
			}
		})
	}
}

// TestOIDCRoutesAbsentWithoutConfiguration proves the new routes are not
// mounted when OIDC is off: the /api fallback answers a JSON 404.
func TestOIDCRoutesAbsentWithoutConfiguration(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)

	for _, path := range []string{oidcLoginRoute, oidcCallbackRoute} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			rec := doJSON(t, app.handler, http.MethodGet, path, nil, nil, "")
			if rec.Code != http.StatusNotFound {
				t.Errorf("GET %s status = %d, want %d", path, rec.Code, http.StatusNotFound)
			}
		})
	}
}
