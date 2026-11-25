package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ostendorf-cloud/govue/csrf"
	"github.com/ostendorf-cloud/govue/logging"

	"github.com/ostendorf-cloud/solobill/internal/frontendconfig"
	"github.com/ostendorf-cloud/solobill/internal/frontendserver"
)

// TestSessionAndCSRFThroughTheFrontend runs the browser's side of issue
// 149 in process: the browser talks to the frontend server only, which
// answers the app and forwards /api/ to this API over real HTTP, the way
// docker-compose.yaml runs the two images. Login, GET /api/v1/me and a
// CSRF protected POST all work on the frontend's one origin, with the
// session cookie the API sets, and a POST without the token is refused.
// In the cluster the Ingress does the forwarding instead of govue's
// development proxy; the cookie and the CSRF check see the same origin.
func TestSessionAndCSRFThroughTheFrontend(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	tenant, secret := seedActiveAccount(t, app.store)

	apiServer := httptest.NewServer(app.handler)
	t.Cleanup(apiServer.Close)

	build := fstest.MapFS{
		"index.html": {Data: []byte(`<!doctype html><html><head><title>solobill</title>` +
			`<script type="module" src="/assets/index-BXa3k2_9.js"></script></head>` +
			`<body><div id="app"></div></body></html>`)},
		"assets/index-BXa3k2_9.js": {Data: []byte("console.log('app')\n")},
	}
	front, err := frontendserver.New(frontendconfig.Config{
		RateLimitBurst: 1000,
		RateLimitPer:   time.Minute,
		AllowPlainHTTP: true,
		DevAPIProxy:    apiServer.URL,
	}, build, logging.New(io.Discard, slog.LevelError), frontendserver.Options{})
	if err != nil {
		t.Fatalf("frontendserver.New() error = %v", err)
	}
	frontServer := httptest.NewServer(front.Handler())
	t.Cleanup(frontServer.Close)

	origin, err := url.Parse(frontServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

	// send is a fetch from the app's page: same origin, the jar's cookies.
	send := func(method, path string, body any, token string) (*http.Response, []byte) {
		t.Helper()

		var reader io.Reader
		if body != nil {
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(data)
		}
		req, err := http.NewRequestWithContext(t.Context(), method, frontServer.URL+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Origin", frontServer.URL)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		if token != "" {
			req.Header.Set(csrf.HeaderName, token)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer func() { _ = res.Body.Close() }()
		data, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}

		return res, data
	}

	// The frontend answers the app's routes itself.
	if res, body := send(http.MethodGet, "/documents/42", nil, ""); res.StatusCode != http.StatusOK ||
		!strings.Contains(string(body), `id="app"`) {
		t.Fatalf("GET /documents/42 = %d, want the app's index.html", res.StatusCode)
	}

	if res, _ := send(http.MethodGet, "/api/v1/me", nil, ""); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/me before login = %d, want %d", res.StatusCode, http.StatusUnauthorized)
	}

	res, body := send(http.MethodPost, loginPath(tenant.Slug), loginBody(ownerEmail, ownerPassword, totpCode(t, secret)), "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login through the frontend = %d %s, want %d", res.StatusCode, body, http.StatusOK)
	}
	if len(jar.Cookies(origin)) == 0 {
		t.Fatal("the login set no cookie for the frontend's origin")
	}

	res, body = send(http.MethodGet, "/api/v1/me", nil, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/me after login = %d %s, want %d", res.StatusCode, body, http.StatusOK)
	}
	var me struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(body, &me); err != nil || me.CSRFToken == "" {
		t.Fatalf("GET /api/v1/me has no csrf_token: %v, body %s", err, body)
	}

	if res, _ := send(http.MethodPost, "/api/v1/auth/logout", nil, ""); res.StatusCode != http.StatusForbidden {
		t.Errorf("POST /api/v1/auth/logout without the token = %d, want %d", res.StatusCode, http.StatusForbidden)
	}
	if res, _ := send(http.MethodGet, "/api/v1/me", nil, ""); res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/me after the refused POST = %d, want %d", res.StatusCode, http.StatusOK)
	}

	if res, body := send(http.MethodPost, "/api/v1/auth/logout", nil, me.CSRFToken); res.StatusCode >= http.StatusBadRequest {
		t.Fatalf("POST /api/v1/auth/logout with the token = %d %s, want success", res.StatusCode, body)
	}
	if res, _ := send(http.MethodGet, "/api/v1/me", nil, ""); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /api/v1/me after logout = %d, want %d", res.StatusCode, http.StatusUnauthorized)
	}
}
