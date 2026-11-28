package siteserver_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ostendorf-cloud/govue/logging"

	"github.com/ostendorf-cloud/solobill/internal/frontendconfig"
	"github.com/ostendorf-cloud/solobill/internal/siteserver"
)

// sitemapPaths are the pages of landing/dist/sitemap.xml: the language roots
// with a trailing slash, every other page without one. landing's own test
// (src/entry-server.test.ts) pins that the real build lists exactly these.
var sitemapPaths = []string{
	"/de/", "/en/",
	"/de/imprint", "/en/imprint",
	"/de/privacy", "/en/privacy",
}

// pageHTML is the shape of a prerendered page: no icon links, no theme-color
// and no boot script, the server injects all three.
func pageHTML(path string) string {
	return `<!doctype html>
<html lang="` + path[1:3] + `" data-theme="solobill">
  <head>
    <meta charset="UTF-8" />
    <title>solobill</title>
    <link rel="canonical" href="https://solobill.de` + path + `">
    <script type="module" crossorigin src="/assets/index-BXa3k2_9.js"></script>
  </head>
  <body><div id="app">page ` + path + `</div></body>
</html>
`
}

// sitemapXML lists every path of sitemapPaths under the site origin.
func sitemapXML() string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, p := range sitemapPaths {
		b.WriteString("  <url><loc>https://solobill.de" + p + "</loc></url>\n")
	}
	b.WriteString("</urlset>\n")

	return b.String()
}

// testBuild is a prerendered build in memory: every page of the sitemap, a
// 404 page per language, the favicon trio and the bundle.
func testBuild() fstest.MapFS {
	build := fstest.MapFS{
		"sitemap.xml":              {Data: []byte(sitemapXML())},
		"de/404.html":              {Data: []byte(pageHTML("/de/404"))},
		"en/404.html":              {Data: []byte(pageHTML("/en/404"))},
		"favicon.svg":              {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)},
		"favicon-32x32.png":        {Data: []byte("\x89PNG\r\n\x1a\nx")},
		"apple-touch-icon.png":     {Data: []byte("\x89PNG\r\n\x1a\nx")},
		"assets/index-BXa3k2_9.js": {Data: []byte("console.log('site')\n")},
	}
	for _, p := range sitemapPaths {
		file := strings.TrimPrefix(p, "/") + "index.html"
		if !strings.HasSuffix(p, "/") {
			file = strings.TrimPrefix(p, "/") + "/index.html"
		}
		build[file] = &fstest.MapFile{Data: []byte(pageHTML(p))}
	}

	return build
}

func testConfig() frontendconfig.Config {
	return frontendconfig.Config{
		ListenAddr:      frontendconfig.DefaultListenAddr,
		AdminListenAddr: frontendconfig.DefaultAdminListenAddr,
		BuildDir:        frontendconfig.DefaultSiteBuildDir,
		RateLimitBurst:  1000,
		RateLimitPer:    frontendconfig.DefaultRateLimitPer,
	}
}

func testLogger() *slog.Logger {
	return logging.New(io.Discard, slog.LevelError)
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()

	s, err := siteserver.New(testConfig(), testBuild(), testLogger(), siteserver.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return s.Handler()
}

func get(t *testing.T, h http.Handler, path, acceptLanguage string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	if acceptLanguage != "" {
		req.Header.Set("Accept-Language", acceptLanguage)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

// TestRouting pins what the site answers for each kind of path: a redirect at
// / by Accept-Language, the page at each sitemap path, a 301 from the slash
// form, the German aliases of the legal pages and the 404 page of the
// language of an unknown path.
func TestRouting(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t)

	tests := []struct {
		name         string
		path         string
		acceptLang   string
		wantStatus   int
		wantLocation string
		wantInBody   string
	}{
		{name: "root without a header", path: "/", wantStatus: http.StatusFound, wantLocation: "/de/"},
		{name: "root for English", path: "/", acceptLang: "en-GB,en;q=0.9", wantStatus: http.StatusFound, wantLocation: "/en/"},
		{name: "root for German", path: "/", acceptLang: "de-DE,de;q=0.9", wantStatus: http.StatusFound, wantLocation: "/de/"},
		{name: "root for a language the site lacks", path: "/", acceptLang: "fr", wantStatus: http.StatusFound, wantLocation: "/de/"},
		{name: "German start page", path: "/de/", wantStatus: http.StatusOK, wantInBody: "page /de/"},
		{name: "English start page", path: "/en/", wantStatus: http.StatusOK, wantInBody: "page /en/"},
		{name: "German imprint", path: "/de/imprint", wantStatus: http.StatusOK, wantInBody: "page /de/imprint"},
		{name: "English privacy", path: "/en/privacy", wantStatus: http.StatusOK, wantInBody: "page /en/privacy"},
		{name: "language root without slash", path: "/de", wantStatus: http.StatusMovedPermanently, wantLocation: "/de/"},
		{name: "slash form of a page", path: "/de/imprint/", wantStatus: http.StatusMovedPermanently, wantLocation: "/de/imprint"},
		{name: "impressum for German", path: "/impressum", acceptLang: "de", wantStatus: http.StatusFound, wantLocation: "/de/imprint"},
		// A German alias names the German page, whatever the header says.
		{name: "impressum for English", path: "/impressum", acceptLang: "en", wantStatus: http.StatusFound, wantLocation: "/de/imprint"},
		{name: "datenschutz", path: "/datenschutz", acceptLang: "en", wantStatus: http.StatusFound, wantLocation: "/de/privacy"},
		{name: "datenschutzerklaerung", path: "/datenschutzerklaerung", wantStatus: http.StatusFound, wantLocation: "/de/privacy"},
		{name: "unprefixed privacy", path: "/privacy", acceptLang: "de", wantStatus: http.StatusFound, wantLocation: "/de/privacy"},
		{name: "unprefixed imprint", path: "/imprint", acceptLang: "en", wantStatus: http.StatusFound, wantLocation: "/en/imprint"},
		{name: "unknown German path", path: "/de/nope", wantStatus: http.StatusNotFound, wantInBody: "page /de/404"},
		{name: "unknown English path", path: "/en/nope", wantStatus: http.StatusNotFound, wantInBody: "page /en/404"},
		{name: "unknown path without language", path: "/nope", wantStatus: http.StatusNotFound, wantInBody: "page /de/404"},
		{name: "no terms page", path: "/de/terms", wantStatus: http.StatusNotFound, wantInBody: "page /de/404"},
		{name: "no agb alias", path: "/agb", wantStatus: http.StatusNotFound},
		{name: "unknown bundle file", path: "/assets/missing.js", wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := get(t, h, tt.path, tt.acceptLang)
			if rec.Code != tt.wantStatus {
				t.Fatalf("GET %s status = %d, want %d", tt.path, rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Location"); got != tt.wantLocation {
				t.Errorf("GET %s Location = %q, want %q", tt.path, got, tt.wantLocation)
			}
			if !strings.Contains(rec.Body.String(), tt.wantInBody) {
				t.Errorf("GET %s body lacks %q: %s", tt.path, tt.wantInBody, rec.Body.String())
			}
		})
	}
}

// TestEverySitemapPathIsAPage pins that each path of the sitemap answers 200
// and its slash form, where it differs, a 301 back, so a page listed in the
// sitemap is never a redirect or a 404.
func TestEverySitemapPathIsAPage(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t)

	for _, p := range sitemapPaths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()

			if rec := get(t, h, p, ""); rec.Code != http.StatusOK {
				t.Errorf("GET %s status = %d, want 200", p, rec.Code)
			}
		})
	}

	sitemap := get(t, h, "/sitemap.xml", "")
	if sitemap.Code != http.StatusOK {
		t.Fatalf("GET /sitemap.xml status = %d, want 200", sitemap.Code)
	}
	for _, loc := range regexp.MustCompile(`<loc>https://solobill\.de([^<]+)</loc>`).FindAllStringSubmatch(sitemap.Body.String(), -1) {
		path := loc[1]
		if strings.HasSuffix(path, "/") && len(path) != len("/de/") {
			t.Errorf("sitemap loc %q has a trailing slash but is not a language root", path)
		}
	}
}

// TestPagesCarryWhatTheServerInjects pins the three things a prerendered
// page does not hold itself: the favicon links, one theme-color per mode and
// the theme boot script, allowed by its hash and never by unsafe-inline.
func TestPagesCarryWhatTheServerInjects(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t)

	for _, p := range []string{"/de/", "/en/imprint", "/de/404"} {
		path := p
		wantStatus := http.StatusOK
		if p == "/de/404" {
			path = "/de/nope"
			wantStatus = http.StatusNotFound
		}

		rec := get(t, h, path, "")
		if rec.Code != wantStatus {
			t.Fatalf("GET %s status = %d, want %d", path, rec.Code, wantStatus)
		}
		body := rec.Body.String()
		for _, want := range []string{
			`rel="icon"`,
			`rel="apple-touch-icon"`,
			`name="theme-color"`,
			siteserver.ThemeColor,
			siteserver.ThemeColorDark,
			"<script>",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s body lacks %q: %s", path, want, body)
			}
		}

		csp := rec.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "'sha256-") || strings.Contains(csp, "unsafe-inline") {
			t.Errorf("GET %s Content-Security-Policy = %q, want the boot script's hash and no unsafe-inline", path, csp)
		}
	}
}

// TestFaviconTrio pins that the three icon files sit at the root of the
// build and are served from there.
func TestFaviconTrio(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t)

	for _, name := range []string{"/favicon.svg", "/favicon-32x32.png", "/apple-touch-icon.png"} {
		if rec := get(t, h, name, ""); rec.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", name, rec.Code)
		}
	}
}

// TestNewRefusesAnIncompleteBuild pins that a build without a page the site
// cannot do without stops the start instead of serving a hole.
func TestNewRefusesAnIncompleteBuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		remove string
	}{
		{name: "no German 404 page", remove: "de/404.html"},
		{name: "no English 404 page", remove: "en/404.html"},
		{name: "no German imprint", remove: "de/imprint/index.html"},
		{name: "no English privacy page", remove: "en/privacy/index.html"},
		{name: "no sitemap", remove: "sitemap.xml"},
		{name: "no favicon", remove: "favicon.svg"},
		{name: "no touch icon", remove: "apple-touch-icon.png"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			build := testBuild()
			delete(build, tt.remove)

			if _, err := siteserver.New(testConfig(), build, testLogger(), siteserver.Options{}); err == nil {
				t.Errorf("New() error = nil without %s, want one", tt.remove)
			}
		})
	}
}

func TestNewFromDir(t *testing.T) {
	t.Parallel()

	t.Run("directory with a build", func(t *testing.T) {
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
		s, err := siteserver.NewFromDir(cfg, testLogger(), siteserver.Options{})
		if err != nil {
			t.Fatalf("NewFromDir() error = %v", err)
		}
		if rec := get(t, s.Handler(), "/de/", ""); rec.Code != http.StatusOK {
			t.Errorf("GET /de/ status = %d, want 200", rec.Code)
		}
	})

	t.Run("missing directory", func(t *testing.T) {
		t.Parallel()

		cfg := testConfig()
		cfg.BuildDir = filepath.Join(t.TempDir(), "absent")
		if _, err := siteserver.NewFromDir(cfg, testLogger(), siteserver.Options{}); err == nil {
			t.Error("NewFromDir() error = nil for a missing directory, want one")
		}
	})
}

// TestThemeColorsMatchTheTheme pins that the browser chrome colours are the
// page background of the theme the site is built with, so a brand change that
// regenerates landing/src/theme/solobill.css fails here until the two
// constants follow.
func TestThemeColorsMatchTheTheme(t *testing.T) {
	t.Parallel()

	css, err := os.ReadFile(filepath.Join("..", "..", "landing", "src", "theme", "solobill.css"))
	if err != nil {
		t.Fatalf("read the theme: %v", err)
	}

	m := regexp.MustCompile(`--color-bg:\s*light-dark\((#[0-9a-fA-F]{3,6}),\s*(#[0-9a-fA-F]{3,6})\)`).FindSubmatch(css)
	if m == nil {
		t.Fatal("solobill.css has no --color-bg: light-dark(light, dark) declaration")
	}
	if got := strings.ToLower(string(m[1])); got != siteserver.ThemeColor {
		t.Errorf("light --color-bg = %s, siteserver.ThemeColor = %s", got, siteserver.ThemeColor)
	}
	if got := strings.ToLower(string(m[2])); got != siteserver.ThemeColorDark {
		t.Errorf("dark --color-bg = %s, siteserver.ThemeColorDark = %s", got, siteserver.ThemeColorDark)
	}
}
