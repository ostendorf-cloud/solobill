package frontendserver_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ostendorf-cloud/govue/logging"

	"github.com/ostendorf-cloud/solobill/internal/frontendconfig"
	"github.com/ostendorf-cloud/solobill/internal/frontendserver"
)

// indexHTML is the shape of web/dist/index.html after vite build: the bundle
// as a module script with src, no inline script.
const indexHTML = `<!doctype html>
<html lang="de">
  <head>
    <meta charset="UTF-8" />
    <title>solobill</title>
    <script type="module" crossorigin src="/assets/index-BXa3k2_9.js"></script>
  </head>
  <body><div id="app"></div></body>
</html>
`

// testBuild is a Vite build in memory: index.html and one hashed bundle.
func testBuild() fstest.MapFS {
	return fstest.MapFS{
		"index.html":                {Data: []byte(indexHTML)},
		"assets/index-BXa3k2_9.js":  {Data: []byte("console.log('app')\n")},
		"assets/index-Cq7Lm4pZ.css": {Data: []byte("body{}\n")},
	}
}

func testConfig() frontendconfig.Config {
	return frontendconfig.Config{
		ListenAddr:      frontendconfig.DefaultListenAddr,
		AdminListenAddr: frontendconfig.DefaultAdminListenAddr,
		BuildDir:        frontendconfig.DefaultBuildDir,
		RateLimitBurst:  1000,
		RateLimitPer:    frontendconfig.DefaultRateLimitPer,
	}
}

func testLogger() *slog.Logger {
	return logging.New(io.Discard, slog.LevelError)
}

func newTestHandler(t *testing.T, cfg frontendconfig.Config, o frontendserver.Options) http.Handler {
	t.Helper()

	s, err := frontendserver.New(cfg, testBuild(), testLogger(), o)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return s.Handler()
}

func get(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, path, nil))

	return rec
}

// TestRouting pins what the frontend answers for each kind of path: the
// app's index.html for every route of the Vue router, the bundle from
// assets/, and a JSON 404 under /api/ and the old OIDC prefix, never the
// app's HTML for them.
func TestRouting(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, testConfig(), frontendserver.Options{})

	tests := []struct {
		name        string
		method      string
		path        string
		wantStatus  int
		wantType    string
		wantCache   string
		wantInBody  string
		wantNotBody string
	}{
		{
			name: "root", method: http.MethodGet, path: "/", wantStatus: http.StatusOK, wantType: "text/html",
			wantCache: "no-cache", wantInBody: `<div id="app">`,
		},
		{
			name: "app route", method: http.MethodGet, path: "/documents/42", wantStatus: http.StatusOK,
			wantType: "text/html", wantCache: "no-cache", wantInBody: `<div id="app">`,
		},
		{
			name: "fixed asset route", method: http.MethodGet, path: "/fixed-assets/7", wantStatus: http.StatusOK,
			wantType: "text/html", wantCache: "no-cache", wantInBody: `<div id="app">`,
		},
		{
			name: "login route head", method: http.MethodHead, path: "/login", wantStatus: http.StatusOK,
			wantType: "text/html", wantCache: "no-cache",
		},
		{
			name: "hashed bundle", method: http.MethodGet, path: "/assets/index-BXa3k2_9.js", wantStatus: http.StatusOK,
			wantType: "javascript", wantCache: "immutable", wantInBody: "console.log",
		},
		{
			name: "unknown bundle file", method: http.MethodGet, path: "/assets/missing.js",
			wantStatus: http.StatusNotFound, wantNotBody: `<div id="app">`,
		},
		{
			name: "api", method: http.MethodGet, path: "/api/v1/me", wantStatus: http.StatusNotFound,
			wantType: "application/json", wantNotBody: `<div id="app">`,
		},
		{
			name: "api post", method: http.MethodPost, path: "/api/v1/auth/logout", wantStatus: http.StatusNotFound,
			wantType: "application/json", wantNotBody: `<div id="app">`,
		},
		{
			name: "old oidc callback", method: http.MethodGet, path: "/auth/oidc/callback?code=a&state=b",
			wantStatus: http.StatusNotFound, wantType: "application/json", wantNotBody: `<div id="app">`,
		},
		{
			name: "old oidc prefix", method: http.MethodGet, path: "/auth/oidc", wantStatus: http.StatusNotFound,
			wantType: "application/json",
		},
		{
			name: "robots", method: http.MethodGet, path: "/robots.txt", wantStatus: http.StatusOK,
			wantType: "text/plain", wantInBody: "Disallow: /",
		},
		{name: "post to an app route", method: http.MethodPost, path: "/documents", wantStatus: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := get(t, h, tt.method, tt.path)
			if rec.Code != tt.wantStatus {
				t.Fatalf("%s %s status = %d, want %d", tt.method, tt.path, rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Content-Type"); !strings.Contains(got, tt.wantType) {
				t.Errorf("Content-Type = %q, want containing %q", got, tt.wantType)
			}
			if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, tt.wantCache) {
				t.Errorf("Cache-Control = %q, want containing %q", got, tt.wantCache)
			}
			body := rec.Body.String()
			if tt.wantInBody != "" && !strings.Contains(body, tt.wantInBody) {
				t.Errorf("body = %q, want containing %q", body, tt.wantInBody)
			}
			if tt.wantNotBody != "" && strings.Contains(body, tt.wantNotBody) {
				t.Errorf("body = %q, want without %q", body, tt.wantNotBody)
			}
		})
	}
}

// TestIndexIsRevalidated pins that index.html carries a strong ETag and
// answers a matching If-None-Match with 304, so a new deployment reaches
// the browser on its next load.
func TestIndexIsRevalidated(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, testConfig(), frontendserver.Options{})

	first := get(t, h, http.MethodGet, "/documents")
	etag := first.Header().Get("ETag")
	if etag == "" || strings.HasPrefix(etag, "W/") {
		t.Fatalf("ETag = %q, want a strong ETag", etag)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/customers", nil)
	req.Header.Set("If-None-Match", etag)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("revalidation status = %d, want %d", rec.Code, http.StatusNotModified)
	}
}

// TestSecurityHeaders pins the headers every answer carries: a policy with
// no inline script, framing denied, no sniffing, and HSTS unless the plain
// HTTP development switch is on.
func TestSecurityHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		plainHTTP bool
		wantHSTS  bool
	}{
		{name: "default", wantHSTS: true},
		{name: "plain http", plainHTTP: true, wantHSTS: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := testConfig()
			cfg.AllowPlainHTTP = tt.plainHTTP
			h := newTestHandler(t, cfg, frontendserver.Options{})

			for _, path := range []string{"/", "/documents/42", "/assets/index-BXa3k2_9.js", "/api/v1/me"} {
				rec := get(t, h, http.MethodGet, path)
				csp := rec.Header().Get("Content-Security-Policy")
				if !strings.Contains(csp, "script-src 'self'") {
					t.Errorf("%s CSP = %q, want script-src 'self'", path, csp)
				}
				if strings.Contains(scriptSrc(csp), "unsafe-inline") {
					t.Errorf("%s CSP = %q, want no unsafe-inline in script-src", path, csp)
				}
				if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
					t.Errorf("%s X-Frame-Options = %q, want DENY", path, got)
				}
				if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
					t.Errorf("%s X-Content-Type-Options = %q, want nosniff", path, got)
				}
				if got := rec.Header().Get("Strict-Transport-Security") != ""; got != tt.wantHSTS {
					t.Errorf("%s HSTS present = %v, want %v", path, got, tt.wantHSTS)
				}
			}
		})
	}
}

// scriptSrc returns the script-src directive of a policy.
func scriptSrc(csp string) string {
	for directive := range strings.SplitSeq(csp, ";") {
		directive = strings.TrimSpace(directive)
		if strings.HasPrefix(directive, "script-src ") {
			return directive
		}
	}

	return ""
}

// roundTripFunc is an http.RoundTripper that answers in memory.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestDevAPIProxy pins the development switch: with FRONTEND_DEV_API_PROXY
// set, /api/ reaches the API container and its Set-Cookie reaches the
// browser on the frontend's origin. Without it, /api/ is the JSON 404 and
// nothing is forwarded.
func TestDevAPIProxy(t *testing.T) {
	t.Parallel()

	var forwarded []string
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		forwarded = append(forwarded, r.URL.String())
		h := http.Header{}
		h.Set("Content-Type", "application/json")
		h.Set("Set-Cookie", "session=abc; Path=/; HttpOnly; SameSite=Lax")

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     h,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})

	cfg := testConfig()
	cfg.DevAPIProxy = "http://api:8080"
	h := newTestHandler(t, cfg, frontendserver.Options{DevAPITransport: transport})

	rec := get(t, h, http.MethodGet, "/api/v1/me")
	if rec.Code != http.StatusOK {
		t.Fatalf("proxied status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Set-Cookie"); !strings.HasPrefix(got, "session=abc") {
		t.Errorf("Set-Cookie = %q, want the API's cookie", got)
	}
	if len(forwarded) != 1 || forwarded[0] != "http://api:8080/api/v1/me" {
		t.Errorf("forwarded = %q, want one request to http://api:8080/api/v1/me", forwarded)
	}

	// The app itself is still served by the frontend.
	if rec := get(t, h, http.MethodGet, "/documents"); !strings.Contains(rec.Body.String(), `<div id="app">`) {
		t.Errorf("GET /documents with the proxy on did not answer index.html")
	}

	off := newTestHandler(t, testConfig(), frontendserver.Options{DevAPITransport: transport})
	if rec := get(t, off, http.MethodGet, "/api/v1/me"); rec.Code != http.StatusNotFound {
		t.Errorf("without the switch status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if len(forwarded) != 1 {
		t.Errorf("without the switch a request was forwarded: %q", forwarded)
	}
}

// TestNewServerFromDir pins that the build is read from FRONTEND_BUILD_DIR
// on disk, as the image ships it, and that a missing directory or a build
// without index.html refuses to start.
func TestNewServerFromDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for name, f := range testBuild() {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, f.Data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfg := testConfig()
	cfg.BuildDir = dir
	s, err := frontendserver.NewFromDir(cfg, testLogger(), frontendserver.Options{})
	if err != nil {
		t.Fatalf("frontendserver.NewFromDir() error = %v", err)
	}
	if rec := get(t, s.Handler(), http.MethodGet, "/settings"); !strings.Contains(rec.Body.String(), `<div id="app">`) {
		t.Errorf("GET /settings did not answer index.html from the directory")
	}

	cfg.BuildDir = filepath.Join(dir, "missing")
	if _, err := frontendserver.NewFromDir(cfg, testLogger(), frontendserver.Options{}); err == nil {
		t.Error("frontendserver.NewFromDir() with a missing directory error = nil, want an error")
	}

	if err := os.Remove(filepath.Join(dir, "index.html")); err != nil {
		t.Fatal(err)
	}
	cfg.BuildDir = dir
	if _, err := frontendserver.NewFromDir(cfg, testLogger(), frontendserver.Options{}); err == nil {
		t.Error("frontendserver.NewFromDir() without index.html error = nil, want an error")
	}
}
