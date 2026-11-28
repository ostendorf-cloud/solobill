// Package siteserver builds the server of the site image, cmd/site-frontend:
// govue's frontend package in prerendered mode over the build of landing/.
// The build holds one HTML file per page and language, a 404 page per
// language, the favicon set and sitemap.xml, all written by
// `pnpm run prerender`. govue serves each page at its sitemap path, answers /
// with a redirect by Accept-Language and adds the German aliases of the legal
// pages. The site holds no secret and serves no API.
package siteserver

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	"github.com/ostendorf-cloud/govue/clientip"
	"github.com/ostendorf-cloud/govue/frontend"
	"github.com/ostendorf-cloud/govue/language"
	"github.com/ostendorf-cloud/govue/middleware"
	"github.com/ostendorf-cloud/govue/ratelimit"

	"github.com/ostendorf-cloud/solobill/internal/frontendconfig"
)

// Languages and DefaultLanguage are the languages of the site, the same two
// the app has. landing/src/texts.ts holds the texts of both.
var Languages = []string{"de", "en"}

// DefaultLanguage answers a visitor whose cookie and Accept-Language name no
// site language.
const DefaultLanguage = "de"

// ThemeColor and ThemeColorDark are the browser chrome colours of the light
// and the dark mode: the page background of landing/src/theme/solobill.css.
// A test reads them from that file, so the two cannot drift apart.
const (
	ThemeColor     = "#c8d2dd"
	ThemeColorDark = "#02060d"
)

// maxRateLimitKeys bounds the client addresses the rate limiter tracks, the
// bound of the app's frontend.
const maxRateLimitKeys = 100_000

// Options are what a test replaces: the transport that reaches the API in
// development. The zero value is production's.
type Options struct {
	DevAPITransport http.RoundTripper
	// Links are served as /config.json for the landing's buttons. Nil is
	// DefaultLinks, what a site with no variable set serves.
	Links *Links
}

// NewFromDir opens cfg.BuildDir and builds the server from it. os.OpenRoot
// keeps every read inside the directory, a symbolic link out of it
// included, which os.DirFS does not. govue's frontend reads the whole build
// once in New and keeps no handle, so the root is closed after it.
func NewFromDir(cfg frontendconfig.Config, logger *slog.Logger, o Options) (*frontend.Server, error) {
	root, err := os.OpenRoot(cfg.BuildDir)
	if err != nil {
		return nil, fmt.Errorf("siteserver: open FRONTEND_BUILD_DIR: %w", err)
	}
	defer func() {
		// Read only, New has read every file already: nothing is lost.
		_ = root.Close()
	}()

	return New(cfg, root.FS(), logger, o)
}

// New builds govue's frontend server in prerendered mode from build and cfg.
func New(cfg frontendconfig.Config, build fs.FS, logger *slog.Logger, o Options) (*frontend.Server, error) {
	resolver := clientip.NewResolver(cfg.TrustedProxies)

	// govue's default policy: script-src 'self' and no inline script. The
	// one inline script is govue's theme boot script, which New allows by
	// its hash.
	security := middleware.DefaultSecurity()
	if cfg.AllowPlainHTTP {
		// A browser that reached this service over plain HTTP must never
		// be told to upgrade every future visit to HTTPS.
		security.HSTS = 0
	}

	rateLimit, err := middleware.NewRateLimit(middleware.RateLimitConfig{
		Limiter:  ratelimit.New(cfg.RateLimitBurst, cfg.RateLimitPer, maxRateLimitKeys),
		ClientIP: resolver,
	})
	if err != nil {
		return nil, fmt.Errorf("siteserver: rate limit: %w", err)
	}

	links := DefaultLinks()
	if o.Links != nil {
		links = *o.Links
	}
	config, err := links.JSON()
	if err != nil {
		return nil, err
	}

	s, err := frontend.New(frontend.Config{
		Build: withFile(build, ConfigFile, config),
		// The one file the server makes itself. govue serves every public
		// file with Cache-Control: public, max-age=86400. Only the landing's
		// cache: no-cache makes the browser revalidate it by its ETag, so a
		// shared cache in front of the site can hold it for a day.
		PublicFiles: []string{ConfigFile},
		Language: language.Config{
			Languages: Languages,
			Default:   DefaultLanguage,
		},
		ThemeColor:     ThemeColor,
		ThemeColorDark: ThemeColorDark,
		Security:       &security,
		ClientIP:       resolver,
		RateLimit:      rateLimit,
		Logger:         logger,

		Development:     cfg.Development(),
		DevAPIProxy:     cfg.DevAPIProxy,
		DevAPITransport: o.DevAPITransport,
	})
	if err != nil {
		return nil, fmt.Errorf("siteserver: %w", err)
	}

	return s, nil
}
