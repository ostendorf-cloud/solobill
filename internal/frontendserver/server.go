// Package frontendserver builds the server of the frontend image,
// cmd/frontend: govue's frontend package in app mode over the Vite build of
// web/. index.html answers every route of the Vue router, the bundle is
// served from assets/, /api/ and the reserved prefixes answer a JSON 404,
// and every response carries govue's security headers. It holds no secret
// and serves no API: the Ingress routes /api to the API image, and only a
// development stack forwards /api/ through this server
// (FRONTEND_DEV_API_PROXY).
package frontendserver

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	"github.com/ostendorf-cloud/govue/clientip"
	"github.com/ostendorf-cloud/govue/frontend"
	"github.com/ostendorf-cloud/govue/middleware"
	"github.com/ostendorf-cloud/govue/ratelimit"

	"github.com/ostendorf-cloud/solobill/internal/frontendconfig"
)

// maxRateLimitKeys bounds the client addresses the rate limiter tracks, the
// bound of the API (internal/httpapi) and of govue's own example.
const maxRateLimitKeys = 100_000

// reservedPrefixes are the prefixes beside /api/, which govue always
// reserves, that answer a JSON 404 instead of index.html. /auth/oidc/ is
// where the OIDC routes lived before they moved under /api/v1/auth/oidc
// (internal/httpapi's legacyOIDCPrefix): an identity provider still
// configured with the old redirect URL fails loudly instead of landing on
// the app with a 200.
var reservedPrefixes = []string{"/auth/oidc/"}

// Options are what a test replaces: the transport that reaches the API in
// development. The zero value is production's.
type Options struct {
	DevAPITransport http.RoundTripper
}

// NewFromDir opens cfg.BuildDir and builds the server from it. os.OpenRoot
// keeps every read inside the directory, a symbolic link out of it
// included, which os.DirFS does not. govue's frontend reads the whole build
// once in New and keeps no handle, so the root is closed after it.
func NewFromDir(cfg frontendconfig.Config, logger *slog.Logger, o Options) (*frontend.Server, error) {
	root, err := os.OpenRoot(cfg.BuildDir)
	if err != nil {
		return nil, fmt.Errorf("frontendserver: open FRONTEND_BUILD_DIR: %w", err)
	}
	defer func() {
		// Read only, New has read every file already: nothing is lost.
		_ = root.Close()
	}()

	return New(cfg, root.FS(), logger, o)
}

// New builds govue's frontend server in app mode from build and cfg.
func New(cfg frontendconfig.Config, build fs.FS, logger *slog.Logger, o Options) (*frontend.Server, error) {
	resolver := clientip.NewResolver(cfg.TrustedProxies)

	// govue's default policy: script-src 'self' and no inline script. The
	// Vite build loads its bundle as a module script with src, so
	// index.html needs none. The one inline script is govue's theme boot
	// script (AppConfig.ThemeBoot below), which app mode allows by its hash.
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
		return nil, fmt.Errorf("frontendserver: rate limit: %w", err)
	}

	s, err := frontend.New(frontend.Config{
		Build: build,
		App: &frontend.AppConfig{
			ReservedPrefixes: reservedPrefixes,
			// Applies the stored colour mode before the first paint, so a
			// visitor who chose dark never sees a flash of the light page.
			ThemeBoot: true,
		},
		Security:  &security,
		ClientIP:  resolver,
		RateLimit: rateLimit,
		Logger:    logger,

		Development:     cfg.Development(),
		DevAPIProxy:     cfg.DevAPIProxy,
		DevAPITransport: o.DevAPITransport,
	})
	if err != nil {
		return nil, fmt.Errorf("frontendserver: %w", err)
	}

	return s, nil
}
