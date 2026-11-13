package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	govueoidc "github.com/ostendorf-cloud/govue/oidc"
	"github.com/ostendorf-cloud/govue/session"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// keycloakIDP is a fake identity provider shaped like a real Keycloak
// realm: its discovery document and its ID tokens use Keycloak's actual
// endpoint layout ("/realms/<realm>/protocol/openid-connect/...") and
// claim names (sub, preferred_username, email, email_verified,
// realm_access.roles, iss, aud, azp, exp, iat), based on Keycloak's
// publicly documented token and endpoint structure, not a generic,
// invented shape. It serves over a real loopback httptest.Server rather
// than a RoundTripper, so oidc.New's own https-or-loopback endpoint check
// runs unmodified against it.
type keycloakIDP struct {
	t       *testing.T
	server  *httptest.Server
	key     *rsa.PrivateKey
	kid     string
	idToken string // set by each test before it calls Exchange or Callback.
}

func newKeycloakIDP(t *testing.T) *keycloakIDP {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	idp := &keycloakIDP{t: t, key: key, kid: "keycloak-key-1"}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /realms/solobill/.well-known/openid-configuration", idp.discovery)
	mux.HandleFunc("GET /realms/solobill/protocol/openid-connect/certs", idp.jwks)
	mux.HandleFunc("POST /realms/solobill/protocol/openid-connect/token", idp.token)

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)

	return idp
}

func (idp *keycloakIDP) issuer() string { return idp.server.URL + "/realms/solobill" }

func (idp *keycloakIDP) discovery(w http.ResponseWriter, _ *http.Request) {
	idp.writeJSON(w, map[string]string{
		"issuer":                 idp.issuer(),
		"authorization_endpoint": idp.issuer() + "/protocol/openid-connect/auth",
		"token_endpoint":         idp.issuer() + "/protocol/openid-connect/token",
		"jwks_uri":               idp.issuer() + "/protocol/openid-connect/certs",
		"end_session_endpoint":   idp.issuer() + "/protocol/openid-connect/logout",
	})
}

func (idp *keycloakIDP) jwks(w http.ResponseWriter, _ *http.Request) {
	idp.writeJSON(w, map[string]any{
		"keys": []map[string]string{
			{
				"kid": idp.kid,
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(idp.key.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(idp.key.E)).Bytes()),
			},
		},
	})
}

func (idp *keycloakIDP) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		idp.t.Errorf("fake keycloak token endpoint: parse form: %v", err)
	}

	idp.writeJSON(w, map[string]string{"id_token": idp.idToken})
}

func (idp *keycloakIDP) writeJSON(w http.ResponseWriter, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		idp.t.Fatalf("marshal fake keycloak response: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// keycloakClaims is a realistic Keycloak ID token payload: the claim
// names and nesting (realm_access.roles for realm role membership) match
// Keycloak's own documented token structure, not a generic claim set.
func keycloakClaims(issuer, clientID, subject, nonce string, at time.Time, roles []string) map[string]any {
	return map[string]any{
		"iss":                issuer,
		"sub":                subject,
		"aud":                clientID,
		"azp":                clientID,
		"exp":                at.Add(5 * time.Minute).Unix(),
		"iat":                at.Unix(),
		"nonce":              nonce,
		"preferred_username": "simon",
		"email":              "owner@acme.example",
		"email_verified":     true,
		"name":               "Simon Ostendorf",
		"realm_access": map[string]any{
			"roles": roles,
		},
	}
}

// signRS256 builds a JWS in compact serialization, RS256, the only
// algorithm Keycloak issues ID tokens with by default.
func signRS256(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()

	encode := func(v any) string {
		body, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal jwt part: %v", err)
		}

		return base64.RawURLEncoding.EncodeToString(body)
	}

	signingInput := encode(map[string]string{"alg": "RS256", "typ": "JWT", "kid": kid}) + "." + encode(claims)

	digest := sha256.Sum256([]byte(signingInput))

	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

const oidcTestClientID = "solobill"

func newTestOIDCProvider(t *testing.T, idp *keycloakIDP, now time.Time) *govueoidc.Provider {
	t.Helper()

	provider, err := NewOIDCProvider(OIDCProviderConfig{
		Issuer:       idp.issuer(),
		ClientID:     oidcTestClientID,
		ClientSecret: "a test client secret",
		RedirectURL:  "http://127.0.0.1/auth/callback",
		HTTPClient:   idp.server.Client(),
		Now:          func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	return provider
}

// TestNewOIDCProviderReadsKeycloakRealmRoles exercises NewOIDCProvider
// end to end (discovery, JWKS fetch, signature verification, claim
// parsing) against the Keycloak-shaped fixture, and checks specifically
// that GroupsClaim's "realm_access.roles" wiring (KeycloakGroupsClaim)
// reads Keycloak's nested realm roles into Identity.Groups, which a flat
// "groups" claim name would not reach.
func TestNewOIDCProviderReadsKeycloakRealmRoles(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	idp := newKeycloakIDP(t)
	provider := newTestOIDCProvider(t, idp, now)

	login, err := govueoidc.NewLogin("/dashboard")
	if err != nil {
		t.Fatalf("NewLogin: %v", err)
	}

	idp.idToken = signRS256(t, idp.key, idp.kid, keycloakClaims(
		idp.issuer(), oidcTestClientID, "f:realm:subject-1", login.Nonce, now,
		[]string{"owner", "default-roles-solobill"},
	))

	identity, err := provider.Exchange(t.Context(), "test-authorization-code", login)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	if identity.Subject != "f:realm:subject-1" {
		t.Errorf("Subject = %q, want %q", identity.Subject, "f:realm:subject-1")
	}
	if identity.Username != "simon" {
		t.Errorf("Username = %q, want %q", identity.Username, "simon")
	}
	if identity.Email != "owner@acme.example" {
		t.Errorf("Email = %q, want %q", identity.Email, "owner@acme.example")
	}
	if !identity.In("owner") {
		t.Errorf("Groups = %v, want it to contain %q (read from realm_access.roles)", identity.Groups, "owner")
	}
}

func TestResolveOIDCIdentity(t *testing.T) {
	t.Parallel()

	const subject = "f:realm:subject-1"

	tests := []struct {
		name    string
		setup   func(store *fakeStore, tenantID uuid.UUID)
		wantErr error
	}{
		{
			name: "active account provisioned for this subject",
			setup: func(store *fakeStore, tenantID uuid.UUID) {
				s := subject
				store.addAccount(storage.Account{
					TenantID:    tenantID,
					Email:       "owner@acme.example",
					OIDCSubject: &s,
					Role:        storage.AccountRoleOwner,
					Status:      storage.AccountStatusActive,
				})
			},
		},
		{
			name:    "no account at all",
			setup:   func(_ *fakeStore, _ uuid.UUID) {},
			wantErr: ErrOIDCAccountNotProvisioned,
		},
		{
			name: "account exists but is pending",
			setup: func(store *fakeStore, tenantID uuid.UUID) {
				s := subject
				store.addAccount(storage.Account{
					TenantID:    tenantID,
					Email:       "owner@acme.example",
					OIDCSubject: &s,
					Role:        storage.AccountRoleOwner,
					Status:      storage.AccountStatusPending,
				})
			},
			wantErr: ErrOIDCAccountNotProvisioned,
		},
		{
			name: "account exists but is disabled",
			setup: func(store *fakeStore, tenantID uuid.UUID) {
				s := subject
				store.addAccount(storage.Account{
					TenantID:    tenantID,
					Email:       "owner@acme.example",
					OIDCSubject: &s,
					Role:        storage.AccountRoleOwner,
					Status:      storage.AccountStatusDisabled,
				})
			},
			wantErr: ErrOIDCAccountNotProvisioned,
		},
		{
			// This is the case that would have slipped through the old,
			// email-based matching: an active account exists with the
			// same email the identity carries, but it was provisioned
			// for a different OIDC subject (or none at all). Matching on
			// subject, never on email, must still refuse it.
			name: "active account with a matching email but a different subject",
			setup: func(store *fakeStore, tenantID uuid.UUID) {
				other := "f:realm:someone-else"
				store.addAccount(storage.Account{
					TenantID:    tenantID,
					Email:       "owner@acme.example",
					OIDCSubject: &other,
					Role:        storage.AccountRoleOwner,
					Status:      storage.AccountStatusActive,
				})
			},
			wantErr: ErrOIDCAccountNotProvisioned,
		},
		{
			// Same as above, but the matching-email account has no
			// oidc_subject at all (the common case today, since nothing
			// provisions one yet): still refused, never silently matched
			// by email.
			name: "active account with a matching email but no subject provisioned",
			setup: func(store *fakeStore, tenantID uuid.UUID) {
				store.addAccount(storage.Account{
					TenantID: tenantID,
					Email:    "owner@acme.example",
					Role:     storage.AccountRoleOwner,
					Status:   storage.AccountStatusActive,
				})
			},
			wantErr: ErrOIDCAccountNotProvisioned,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newFakeStore()
			tenant := store.addTenant(storage.Tenant{Slug: "acme", Name: "Acme"})
			tt.setup(store, tenant.ID)

			identity, err := ResolveOIDCIdentity(t.Context(), store, tenant.ID, govueoidc.Identity{
				Subject: subject,
				Email:   "owner@acme.example",
				Name:    "Simon Ostendorf",
				Groups:  []string{"owner"},
			})

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want errors.Is(err, %v)", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("ResolveOIDCIdentity: %v", err)
			}
			if identity.TenantID != tenant.ID {
				t.Errorf("TenantID = %v, want %v", identity.TenantID, tenant.ID)
			}
			if identity.Role != storage.AccountRoleOwner {
				t.Errorf("Role = %v, want %v", identity.Role, storage.AccountRoleOwner)
			}
			if identity.Name != "Simon Ostendorf" {
				t.Errorf("Name = %q, want %q", identity.Name, "Simon Ostendorf")
			}
		})
	}
}

// oidcHandlerFixture wires NewOIDCHandlers end to end against the
// Keycloak fixture: a session manager with a memory store, a session
// codec as the login store (LoginStore, with no adapter, exactly as
// govue's own doc describes), and a fakeStore for tenant and account
// resolution.
type oidcHandlerFixture struct {
	idp      *keycloakIDP
	provider *govueoidc.Provider
	codec    *session.Codec
	sessions *session.Manager
	store    *fakeStore
	invites  *InviteManager
	tenant   storage.Tenant
	now      time.Time
}

func newOIDCHandlerFixture(t *testing.T) *oidcHandlerFixture {
	t.Helper()

	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	idp := newKeycloakIDP(t)
	provider := newTestOIDCProvider(t, idp, now)

	key := make([]byte, session.MinKeyBytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("read codec key: %v", err)
	}

	codec, err := session.NewCodec(session.CodecConfig{
		Key:            key,
		AllowPlainHTTP: true,
		Now:            func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("session.NewCodec: %v", err)
	}

	sessions, err := session.New(session.Config{
		Store:          session.NewMemoryStore(func() time.Time { return now }, 100),
		AllowPlainHTTP: true,
		Now:            func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}

	store := newFakeStore()
	tenant := store.addTenant(storage.Tenant{Slug: "acme", Name: "Acme"})
	invites := NewInviteManager(store, func() time.Time { return now })

	return &oidcHandlerFixture{
		idp: idp, provider: provider, codec: codec, sessions: sessions,
		store: store, invites: invites, tenant: tenant, now: now,
	}
}

// callback drives handlers.Callback() for a login whose ID token carries
// subject/roles, as if the browser had already been round tripped to
// the fake IDP and back. It returns the recorded response.
func (f *oidcHandlerFixture) callback(t *testing.T, handlers *govueoidc.Handlers, subject string, roles []string) *httptest.ResponseRecorder {
	t.Helper()

	login, err := govueoidc.NewLogin("/dashboard")
	if err != nil {
		t.Fatalf("NewLogin: %v", err)
	}

	f.idp.idToken = signRS256(t, f.idp.key, f.idp.kid, keycloakClaims(
		f.idp.issuer(), oidcTestClientID, subject, login.Nonce, f.now, roles,
	))

	setCookieRec := httptest.NewRecorder()
	if err := f.codec.SetLoginCookie(setCookieRec, login); err != nil {
		t.Fatalf("SetLoginCookie: %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/auth/callback?state="+login.State+"&code=test-code", nil)
	for _, c := range setCookieRec.Result().Cookies() {
		req.AddCookie(c)
	}

	rec := httptest.NewRecorder()
	handlers.Callback().ServeHTTP(rec, req)

	return rec
}

func TestNewOIDCHandlersCallbackStartsASessionForAProvisionedAccount(t *testing.T) {
	t.Parallel()

	f := newOIDCHandlerFixture(t)
	subject := "f:realm:subject-1"
	f.store.addAccount(storage.Account{
		TenantID:    f.tenant.ID,
		Email:       "owner@acme.example",
		OIDCSubject: &subject,
		Role:        storage.AccountRoleOwner,
		Status:      storage.AccountStatusActive,
	})

	handlers, err := NewOIDCHandlers(f.provider, f.codec, f.sessions, f.store, f.invites,
		func(*http.Request) (uuid.UUID, error) { return f.tenant.ID, nil },
		OIDCHandlerOptions{},
	)
	if err != nil {
		t.Fatalf("NewOIDCHandlers: %v", err)
	}

	rec := f.callback(t, handlers, "f:realm:subject-1", []string{"owner"})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/dashboard" {
		t.Errorf("Location = %q, want %q", got, "/dashboard")
	}

	var sessionCookie *http.Cookie

	for _, c := range rec.Result().Cookies() {
		if c.Name == f.sessions.CookieName() {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("Callback did not set a session cookie")
	}

	checkReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	checkReq.AddCookie(sessionCookie)

	started, ok, err := f.sessions.Read(t.Context(), checkReq)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !ok {
		t.Fatal("no session was found for the cookie Callback set")
	}
	if started.Email != "owner@acme.example" {
		t.Errorf("session Email = %q, want %q", started.Email, "owner@acme.example")
	}
	if started.Subject == "" {
		t.Error("session Subject is empty, want the accounts.id string")
	}
	if started.Data[roleDataKey] != string(storage.AccountRoleOwner) {
		t.Errorf("session Data[%q] = %q, want %q", roleDataKey, started.Data[roleDataKey], storage.AccountRoleOwner)
	}
	if started.Data[tenantIDDataKey] != f.tenant.ID.String() {
		t.Errorf("session Data[%q] = %q, want %q", tenantIDDataKey, started.Data[tenantIDDataKey], f.tenant.ID.String())
	}
}

func TestNewOIDCHandlersCallbackRefusesAnUnprovisionedAccount(t *testing.T) {
	t.Parallel()

	f := newOIDCHandlerFixture(t)
	// Deliberately no accounts row for this email at all: the "no
	// implicit creation on first OIDC login" judgment call, exercised
	// end to end through the handler.

	handlers, err := NewOIDCHandlers(f.provider, f.codec, f.sessions, f.store, f.invites,
		func(*http.Request) (uuid.UUID, error) { return f.tenant.ID, nil },
		OIDCHandlerOptions{},
	)
	if err != nil {
		t.Fatalf("NewOIDCHandlers: %v", err)
	}

	rec := f.callback(t, handlers, "f:realm:subject-1", []string{"owner"})

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d (the account-not-provisioned failure is reported as ErrProvider, never detailed to the browser); body: %s",
			rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	if rec.Body.String() == "" {
		t.Error("response body is empty, want the opaque refusal message")
	}
}

// callbackWithInvite drives handlers.Callback() exactly like f.callback
// does, with one addition: the request also carries the pending-invite
// cookie StartOIDCInviteAccept signs for rawToken, as if
// internal/httpapi's invite-accept-via-OIDC start endpoint had already
// run and the browser had already been round tripped to the fake IDP
// and back.
func (f *oidcHandlerFixture) callbackWithInvite(t *testing.T, handlers *govueoidc.Handlers, rawToken, subject string, roles []string) *httptest.ResponseRecorder {
	t.Helper()

	login, err := govueoidc.NewLogin("/dashboard")
	if err != nil {
		t.Fatalf("NewLogin: %v", err)
	}

	f.idp.idToken = signRS256(t, f.idp.key, f.idp.kid, keycloakClaims(
		f.idp.issuer(), oidcTestClientID, subject, login.Nonce, f.now, roles,
	))

	cookieRec := httptest.NewRecorder()
	if err := f.codec.SetLoginCookie(cookieRec, login); err != nil {
		t.Fatalf("SetLoginCookie: %v", err)
	}
	if err := StartOIDCInviteAccept(cookieRec, f.codec, true, f.tenant.ID, rawToken); err != nil {
		t.Fatalf("StartOIDCInviteAccept: %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/auth/callback?state="+login.State+"&code=test-code", nil)
	for _, c := range cookieRec.Result().Cookies() {
		req.AddCookie(c)
	}

	rec := httptest.NewRecorder()
	handlers.Callback().ServeHTTP(rec, req)

	return rec
}

// TestNewOIDCHandlersCallbackRedeemsAPendingInviteAndBindsTheSubject
// drives the whole invite-carried OIDC binding shape end to end: an
// owner creates an invite, the browser arrives at the callback carrying
// the pending-invite cookie StartOIDCInviteAccept sets, and the
// identity provider vouches for a subject that has never been seen
// before. The callback must bind that subject to a brand new account
// for the invite's own email and role, start a session for it (an OIDC
// login by a subject bound to an account succeeds), and leave the
// invite used so it cannot be redeemed again.
func TestNewOIDCHandlersCallbackRedeemsAPendingInviteAndBindsTheSubject(t *testing.T) {
	t.Parallel()

	f := newOIDCHandlerFixture(t)

	rawToken, invite, err := f.invites.CreateInvite(t.Context(), CreateInviteParams{
		TenantID: f.tenant.ID,
		Email:    "invitee@acme.example",
		Role:     storage.AccountRoleMember,
		TTL:      time.Hour,
	})
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	handlers, err := NewOIDCHandlers(f.provider, f.codec, f.sessions, f.store, f.invites,
		func(*http.Request) (uuid.UUID, error) { return f.tenant.ID, nil },
		OIDCHandlerOptions{AllowPlainHTTP: true},
	)
	if err != nil {
		t.Fatalf("NewOIDCHandlers: %v", err)
	}

	const newSubject = "f:realm:brand-new-member"

	rec := f.callbackWithInvite(t, handlers, rawToken, newSubject, []string{"member"})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	var (
		sessionCookie *http.Cookie
		inviteCookie  *http.Cookie
	)

	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case f.sessions.CookieName():
			sessionCookie = c
		case inviteOIDCCookieName:
			inviteCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("Callback did not set a session cookie")
	}
	if inviteCookie == nil || inviteCookie.MaxAge >= 0 {
		t.Error("the pending-invite cookie was not cleared by the callback")
	}

	checkReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	checkReq.AddCookie(sessionCookie)

	started, ok, err := f.sessions.Read(t.Context(), checkReq)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !ok {
		t.Fatal("no session was found for the cookie Callback set")
	}
	if started.Email != invite.Email {
		t.Errorf("session Email = %q, want %q", started.Email, invite.Email)
	}
	if started.Data[roleDataKey] != string(storage.AccountRoleMember) {
		t.Errorf("session Data[%q] = %q, want %q", roleDataKey, started.Data[roleDataKey], storage.AccountRoleMember)
	}

	account, err := f.store.Accounts().FindByOIDCSubject(t.Context(), newSubject)
	if err != nil {
		t.Fatalf("FindByOIDCSubject: %v", err)
	}
	if started.Subject != account.ID.String() {
		t.Errorf("session Subject = %q, want the newly bound account's id %q", started.Subject, account.ID.String())
	}

	stored, err := f.store.Invites().FindByID(t.Context(), invite.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if stored.UsedAt == nil {
		t.Error("the invite's UsedAt is still nil after a successful callback accept")
	}

	// A second callback for the same invite token, even with a fresh
	// login and a fresh pending-invite cookie signed for it, must not
	// succeed: the invite is already used. This is the single-threaded
	// half of the single-use guarantee at this layer; the genuinely
	// concurrent half is TestAcceptInviteOIDCIsSingleUseUnderConcurrentAccept.
	replay := f.callbackWithInvite(t, handlers, rawToken, "f:realm:a-second-subject", []string{"member"})
	if replay.Code != http.StatusServiceUnavailable {
		t.Errorf("replay status = %d, want %d (an already-used invite is refused the same opaque way); body: %s",
			replay.Code, http.StatusServiceUnavailable, replay.Body.String())
	}
}
