package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ostendorf-cloud/govue/account"
	"github.com/ostendorf-cloud/govue/csrf"
	"github.com/ostendorf-cloud/govue/health"
	"github.com/ostendorf-cloud/govue/logging"
	"github.com/ostendorf-cloud/govue/passkey"
	"github.com/ostendorf-cloud/govue/passkey/passkeytest"

	"github.com/ostendorf-cloud/solobill/internal/auth"
	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// Fake values only: the passkey relying party of these tests.
const (
	testRPID   = "localhost"
	testOrigin = "http://localhost"
)

// buildAuthApp is buildTestApp with the login limits and passkeys under
// the test's control. It leaves buildTestApp alone (AGENTS.md's conflict
// zones).
func buildAuthApp(t *testing.T, limits httpapi.LoginLimits, withPasskeys bool) testApp {
	t.Helper()

	store := newFakeStore()
	sessions := mustSessions(t)

	cfg := auth.LoginConfig{
		Store:    store,
		Sessions: sessions,
		Hasher:   testHasher(t),
		Logger:   slog.New(slog.DiscardHandler),
	}
	if withPasskeys {
		cfg.Passkey = &auth.PasskeyConfig{
			RPID:           testRPID,
			RPDisplayName:  "solobill test",
			Origins:        []string{testOrigin},
			AllowPlainHTTP: true,
		}
	}

	login, err := auth.NewLogin(cfg)
	if err != nil {
		t.Fatalf("auth.NewLogin: %v", err)
	}

	csrfKey := fixedKey(t, 9)

	handlers, err := httpapi.NewHandler(httpapi.Deps{
		Logger:           logging.New(io.Discard, slog.LevelError),
		Store:            store,
		Sessions:         sessions,
		CSRFKey:          csrfKey,
		Login:            login,
		Invites:          newTestInvites(t, store),
		LoginLimits:      limits,
		HealthRegistry:   health.New(),
		AllowPlainHTTP:   true,
		RateLimitBurst:   1000,
		RateLimitPer:     time.Minute,
		DefaultInviteTTL: time.Hour,
		DistFS:           fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	})
	if err != nil {
		t.Fatalf("httpapi.NewHandler: %v", err)
	}

	return testApp{handler: handlers.Public, store: store, csrfKey: csrfKey}
}

var roomyLimits = httpapi.LoginLimits{ClientBurst: 1000, LoginBurst: 1000, CeilingBurst: 1000}

func TestPasswordChangeEndsEverySessionAndTheNewPasswordLogsIn(t *testing.T) {
	t.Parallel()

	app := buildAuthApp(t, roomyLimits, false)
	tenant, secret := seedActiveAccount(t, app.store)
	cookie, token := loginAndCSRF(t, app, tenant.Slug, ownerEmail, ownerPassword, secret)

	t.Run("without a session", func(t *testing.T) {
		rec := doJSON(t, app.handler, http.MethodPost, "/api/v1/auth/password",
			map[string]string{"current_password": ownerPassword, "new_password": "a new long passphrase"}, nil, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("without a csrf token", func(t *testing.T) {
		rec := doJSON(t, app.handler, http.MethodPost, "/api/v1/auth/password",
			map[string]string{"current_password": ownerPassword, "new_password": "a new long passphrase"},
			[]*http.Cookie{cookie}, "")
		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("with a wrong current password", func(t *testing.T) {
		rec := doJSON(t, app.handler, http.MethodPost, "/api/v1/auth/password",
			map[string]string{"current_password": "not the password", "new_password": "a new long passphrase"},
			[]*http.Cookie{cookie}, token)
		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
		}
	})

	rec := doJSON(t, app.handler, http.MethodPost, "/api/v1/auth/password",
		map[string]string{"current_password": ownerPassword, "new_password": "a new long passphrase"},
		[]*http.Cookie{cookie}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("password change status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var result account.PasswordChangeResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !result.PasswordChanged || !result.SessionsEnded {
		t.Errorf("result = %+v, want the password changed and every session ended", result)
	}

	if me := doJSON(t, app.handler, http.MethodGet, "/api/v1/me", nil, []*http.Cookie{cookie}, ""); me.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/v1/me with the old session = %d, want %d", me.Code, http.StatusUnauthorized)
	}

	// The code of the first login is consumed; the next step's code is
	// inside the window and newer.
	next, err := account.TOTPCode(secret, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatalf("TOTPCode: %v", err)
	}

	if old := doJSON(t, app.handler, http.MethodPost, loginPath(tenant.Slug),
		loginBody(ownerEmail, ownerPassword, next), nil, ""); old.Code != http.StatusUnauthorized {
		t.Errorf("login with the old password = %d, want %d", old.Code, http.StatusUnauthorized)
	}
	if fresh := doJSON(t, app.handler, http.MethodPost, loginPath(tenant.Slug),
		loginBody(ownerEmail, "a new long passphrase", next), nil, ""); fresh.Code != http.StatusOK {
		t.Errorf("login with the new password = %d, want %d, body = %s", fresh.Code, http.StatusOK, fresh.Body.String())
	}
}

func TestLoginLimitPerLoginAndClient(t *testing.T) {
	t.Parallel()

	app := buildAuthApp(t, httpapi.LoginLimits{ClientBurst: 1000, LoginBurst: 2, CeilingBurst: 2}, false)
	tenant, secret := seedActiveAccount(t, app.store)
	code := totpCode(t, secret)

	attempt := func(remoteAddr, login string) int {
		req := jsonRequest(t, http.MethodPost, loginPath(tenant.Slug), loginBody(login, "wrong password", code))
		req.RemoteAddr = remoteAddr
		rec := httptest.NewRecorder()
		app.handler.ServeHTTP(rec, req)

		return rec.Code
	}

	for i := range 2 {
		if got := attempt("192.0.2.1:1234", ownerEmail); got != http.StatusUnauthorized {
			t.Fatalf("attempt %d from the first address = %d, want %d", i, got, http.StatusUnauthorized)
		}
	}
	if got := attempt("192.0.2.1:1234", ownerEmail); got != http.StatusTooManyRequests {
		t.Errorf("third attempt on one login from one address = %d, want %d", got, http.StatusTooManyRequests)
	}

	// The letter case of the login does not open a new bucket.
	if got := attempt("192.0.2.1:1234", strings.ToUpper(ownerEmail)); got != http.StatusTooManyRequests {
		t.Errorf("attempt with the login in upper case = %d, want %d", got, http.StatusTooManyRequests)
	}

	// Another login from the same address has its own bucket.
	if got := attempt("192.0.2.1:1234", "someone-else@acme.test"); got != http.StatusUnauthorized {
		t.Errorf("attempt on another login = %d, want %d", got, http.StatusUnauthorized)
	}

	// Another address has its own per login bucket, but the ceiling per
	// login counts every address: the first two attempts used it up.
	if got := attempt("198.51.100.7:1234", ownerEmail); got != http.StatusTooManyRequests {
		t.Errorf("attempt from a second address after the ceiling = %d, want %d", got, http.StatusTooManyRequests)
	}
}

func TestLoginClientLimit(t *testing.T) {
	t.Parallel()

	app := buildAuthApp(t, httpapi.LoginLimits{ClientBurst: 1, LoginBurst: 1000, CeilingBurst: 1000}, false)
	tenant, secret := seedActiveAccount(t, app.store)
	code := totpCode(t, secret)

	first := doJSON(t, app.handler, http.MethodPost, loginPath(tenant.Slug), loginBody("a@acme.test", "wrong", code), nil, "")
	if first.Code != http.StatusUnauthorized {
		t.Fatalf("first attempt = %d, want %d", first.Code, http.StatusUnauthorized)
	}

	// A different login from the same address: the per client limit,
	// not the per login one, refuses it.
	second := doJSON(t, app.handler, http.MethodPost, loginPath(tenant.Slug), loginBody("b@acme.test", "wrong", code), nil, "")
	if second.Code != http.StatusTooManyRequests {
		t.Errorf("second attempt = %d, want %d", second.Code, http.StatusTooManyRequests)
	}
}

func TestPasskeyRoutesAreAbsentWithoutConfiguration(t *testing.T) {
	t.Parallel()

	app := buildAuthApp(t, roomyLimits, false)

	for _, path := range []string{
		"/api/v1/tenants/acme/auth/passkeys/login/begin",
		"/api/v1/auth/passkeys/register/begin",
	} {
		rec := doJSON(t, app.handler, http.MethodPost, path, map[string]any{}, nil, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want %d with passkeys off", path, rec.Code, http.StatusNotFound)
		}
	}
}

// TestPasskeyRegisterAndLogin runs both WebAuthn ceremonies end to end
// through solobill's routes, with govue's test authenticator: register a
// passkey on a password session, then log in with it on the login page of
// the tenant, which starts the same session a password login does.
func TestPasskeyRegisterAndLogin(t *testing.T) {
	t.Parallel()

	app := buildAuthApp(t, roomyLimits, true)
	tenant, secret := seedActiveAccount(t, app.store)
	cookie, token := loginAndCSRF(t, app, tenant.Slug, ownerEmail, ownerPassword, secret)

	authn, err := passkeytest.NewAuthenticator(testOrigin, testRPID)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	begin := doJSON(t, app.handler, http.MethodPost, "/api/v1/auth/passkeys/register/begin", map[string]any{}, []*http.Cookie{cookie}, token)
	if begin.Code != http.StatusOK {
		t.Fatalf("register begin = %d, body = %s", begin.Code, begin.Body.String())
	}

	created, err := authn.Create(begin.Body.Bytes())
	if err != nil {
		t.Fatalf("authenticator create: %v", err)
	}

	finish := doJSON(t, app.handler, http.MethodPost, "/api/v1/auth/passkeys/register/finish",
		map[string]any{"credential": created, "name": "laptop"},
		[]*http.Cookie{cookie, challengeCookie(t, begin)}, token)
	if finish.Code != http.StatusCreated {
		t.Fatalf("register finish = %d, body = %s", finish.Code, finish.Body.String())
	}

	list := doJSON(t, app.handler, http.MethodGet, "/api/v1/auth/passkeys", nil, []*http.Cookie{cookie}, "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"name":"laptop"`) {
		t.Fatalf("list = %d %s, want the new passkey", list.Code, list.Body.String())
	}

	t.Run("an unknown tenant refuses the passkey", func(t *testing.T) {
		rec := passkeyLogin(t, app, authn, "no-such-tenant")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("login finish = %d, want %d, body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
		}
	})

	rec := passkeyLogin(t, app, authn, tenant.Slug)
	if rec.Code != http.StatusOK {
		t.Fatalf("login finish = %d, body = %s", rec.Code, rec.Body.String())
	}

	session := sessionCookieNamed(t, rec)
	me := doJSON(t, app.handler, http.MethodGet, "/api/v1/me", nil, []*http.Cookie{session}, "")
	if me.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/me after a passkey login = %d, body = %s", me.Code, me.Body.String())
	}

	var body struct {
		Email     string `json:"email"`
		Role      string `json:"role"`
		TenantID  string `json:"tenant_id"`
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Email != ownerEmail || body.Role != string(storage.AccountRoleOwner) || body.TenantID != tenant.ID.String() {
		t.Errorf("me = %+v, want the owner of %s", body, tenant.ID)
	}
	if body.CSRFToken != csrf.Token(app.csrfKey, session.Value) {
		t.Error("the passkey session's csrf token does not match")
	}
}

func passkeyLogin(t *testing.T, app testApp, authn *passkeytest.Authenticator, tenantSlug string) *httptest.ResponseRecorder {
	t.Helper()

	prefix := "/api/v1/tenants/" + tenantSlug + "/auth/passkeys/login"

	begin := doJSON(t, app.handler, http.MethodPost, prefix+"/begin", map[string]any{}, nil, "")
	if begin.Code != http.StatusOK {
		t.Fatalf("login begin = %d, body = %s", begin.Code, begin.Body.String())
	}

	assertion, err := authn.Get(begin.Body.Bytes())
	if err != nil {
		t.Fatalf("authenticator get: %v", err)
	}

	return doJSON(t, app.handler, http.MethodPost, prefix+"/finish",
		map[string]any{"credential": assertion}, []*http.Cookie{challengeCookie(t, begin)}, "")
}

func challengeCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()

	for _, c := range rec.Result().Cookies() {
		if (c.Name == passkey.ChallengeCookieName || c.Name == passkey.PlainChallengeCookieName) && c.Value != "" {
			return c
		}
	}

	t.Fatalf("no challenge cookie in %v", rec.Result().Header.Values("Set-Cookie"))

	return nil
}

// sessionCookieNamed is the session cookie a response set, ignoring the
// challenge cookie a passkey finish clears in the same answer.
func sessionCookieNamed(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()

	for _, c := range rec.Result().Cookies() {
		if c.Name != passkey.ChallengeCookieName && c.Name != passkey.PlainChallengeCookieName && c.Value != "" {
			return c
		}
	}

	t.Fatalf("no session cookie in %v", rec.Result().Header.Values("Set-Cookie"))

	return nil
}

// jsonRequest is doJSON's request, for a test that has to set more on it.
func jsonRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")

	return req
}

// TestInviteConfirmInGovuesShape runs the invite flow the SPA runs with
// govue's TOTP enrolment component: the TOTP secret request sets the
// invite cookie and names the login, and the confirm takes govue's
// {login, password, code} body, creates the account with both factors and
// starts its session.
func TestInviteConfirmInGovuesShape(t *testing.T) {
	t.Parallel()

	app := buildAuthApp(t, roomyLimits, false)
	tenant, ownerSecret := seedActiveAccount(t, app.store)
	ownerCookie, ownerToken := loginAndCSRF(t, app, tenant.Slug, ownerEmail, ownerPassword, ownerSecret)

	inviteRec := doJSON(t, app.handler, http.MethodPost, "/api/v1/invites",
		map[string]string{"email": "new@acme.test", "role": "member"}, []*http.Cookie{ownerCookie}, ownerToken)
	if inviteRec.Code != http.StatusCreated {
		t.Fatalf("create invite = %d", inviteRec.Code)
	}
	var invite struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(inviteRec.Body.Bytes(), &invite); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	confirm := func(cookies []*http.Cookie, login, password, code string) *httptest.ResponseRecorder {
		return doJSON(t, app.handler, http.MethodPost, "/api/v1/invites/confirm",
			loginBody(login, password, code), cookies, "")
	}

	if rec := confirm(nil, "new@acme.test", "a-new-password", "123456"); rec.Code != http.StatusBadRequest {
		t.Errorf("confirm without the invite cookie = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	secretRec := doJSON(t, app.handler, http.MethodPost, "/api/v1/invites/totp-secret",
		map[string]string{"tenant_slug": tenant.Slug, "token": invite.Token}, nil, "")
	if secretRec.Code != http.StatusOK {
		t.Fatalf("totp-secret = %d, body = %s", secretRec.Code, secretRec.Body.String())
	}
	var enrolment struct {
		Secret string `json:"secret"`
		Email  string `json:"email"`
	}
	if err := json.Unmarshal(secretRec.Body.Bytes(), &enrolment); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if enrolment.Email != "new@acme.test" {
		t.Errorf("totp-secret email = %q, want the invite's address", enrolment.Email)
	}

	var inviteCookie *http.Cookie
	for _, c := range secretRec.Result().Cookies() {
		if c.Name == "solobill_invite" {
			inviteCookie = c
		}
	}
	if inviteCookie == nil || !inviteCookie.HttpOnly || inviteCookie.SameSite != http.SameSiteStrictMode ||
		inviteCookie.Path != "/api/v1/invites" {
		t.Fatalf("invite cookie = %+v, want HttpOnly, SameSite=Strict on /api/v1/invites", inviteCookie)
	}

	code := totpCode(t, enrolment.Secret)
	cookies := []*http.Cookie{inviteCookie}

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"another login":      confirm(cookies, "someone-else@acme.test", "a-new-password", code),
		"a short password":   confirm(cookies, "new@acme.test", "short11char", code),
		"a wrong totp code":  confirm(cookies, "new@acme.test", "a-new-password", "000000"),
		"an unknown field":   doJSON(t, app.handler, http.MethodPost, "/api/v1/invites/confirm", map[string]string{"login": "new@acme.test", "password": "a-new-password", "code": code, "token": invite.Token}, cookies, ""),
		"a missing password": confirm(cookies, "new@acme.test", "", code),
	} {
		if rec.Code != http.StatusBadRequest {
			t.Errorf("confirm with %s = %d, want %d, body = %s", name, rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	}

	rec := confirm(cookies, "new@acme.test", "a-new-password", code)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm = %d, body = %s", rec.Code, rec.Body.String())
	}

	var result account.LoginResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Login != "new@acme.test" {
		t.Errorf("confirm login = %q, want the invite's address", result.Login)
	}

	session := sessionCookieNamedNot(t, rec, "solobill_invite")
	me := doJSON(t, app.handler, http.MethodGet, "/api/v1/me", nil, []*http.Cookie{session}, "")
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"role":"member"`) {
		t.Errorf("GET /api/v1/me after the confirm = %d %s, want the new member", me.Code, me.Body.String())
	}

	if again := confirm(cookies, "new@acme.test", "a-new-password", code); again.Code != http.StatusBadRequest {
		t.Errorf("a second confirm of the used invite = %d, want %d", again.Code, http.StatusBadRequest)
	}
}

// sessionCookieNamedNot is the one cookie with a value a response set
// whose name is not skip.
func sessionCookieNamedNot(t *testing.T, rec *httptest.ResponseRecorder, skip string) *http.Cookie {
	t.Helper()

	for _, c := range rec.Result().Cookies() {
		if c.Name != skip && c.Value != "" {
			return c
		}
	}

	t.Fatalf("no session cookie in %v", rec.Result().Header.Values("Set-Cookie"))

	return nil
}
