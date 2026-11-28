// Package frontendconfig loads and validates the configuration of the
// frontend image, cmd/frontend, from the environment, once, at startup.
// Every problem is collected and returned together. The frontend holds no
// secret: no variable here is a key or a password, so none has a _FILE
// twin.
package frontendconfig

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"time"

	"github.com/ostendorf-cloud/govue/clientip"
	"github.com/ostendorf-cloud/govue/envconf"
	"github.com/ostendorf-cloud/govue/logging"
)

// Defaults applied to a variable left unset. The two listen addresses and
// the log level are the API's defaults (internal/config), so both images
// read the same names the same way.
const (
	DefaultListenAddr      = ":8080"
	DefaultAdminListenAddr = ":8081"
	DefaultLogLevel        = "info"
	// DefaultBuildDir is where the Dockerfile's frontend stage copies the
	// Vite build, web/dist.
	DefaultBuildDir = "/usr/share/solobill/web"
	// DefaultSiteBuildDir is where the Dockerfile's site-frontend stage
	// copies the prerendered marketing site, landing/dist.
	DefaultSiteBuildDir = "/usr/share/solobill/site"
	// DefaultRateLimitBurst and DefaultRateLimitPer bound one client
	// address. A first load of the app fetches index.html and a few dozen
	// bundle files, so the burst stays well above one load.
	DefaultRateLimitBurst = 300
	DefaultRateLimitPer   = time.Minute
)

// EnvDevAPIProxy is the development switch: the URL of the API container
// govue's frontend forwards /api/ to, so docker-compose.yaml has one browser
// origin as the Ingress gives production. Set, it also turns on govue's
// Config.Development, which allows the proxy at all. The Helm chart never
// sets it.
const EnvDevAPIProxy = "FRONTEND_DEV_API_PROXY"

// Config is the frontend image's whole configuration.
type Config struct {
	ListenAddr string
	// AdminListenAddr serves health, readiness and metrics, never the app.
	// govue's frontend refuses an empty one.
	AdminListenAddr string
	LogLevel        slog.Level

	// BuildDir is the directory of the Vite build: index.html and assets/.
	BuildDir string

	// TrustedProxies are the TCP peers whose X-Forwarded-For entry
	// clientip trusts, for the access log and the rate limit. Behind an
	// Ingress it names the ingress controller, or every client shares one
	// rate limit bucket.
	TrustedProxies []netip.Prefix

	// RateLimitBurst and RateLimitPer bound how many requests one client
	// address may make in a row before it has to wait.
	RateLimitBurst int
	RateLimitPer   time.Duration

	// AllowPlainHTTP drops the Strict-Transport-Security header, for a
	// plain HTTP development run.
	AllowPlainHTTP bool

	// DevAPIProxy is FRONTEND_DEV_API_PROXY, empty in production. Only a
	// development stack sets it, see EnvDevAPIProxy.
	DevAPIProxy string
}

// Development reports whether the development switch is set. It is true
// exactly when FRONTEND_DEV_API_PROXY is set, never from another variable.
func (c Config) Development() bool {
	return c.DevAPIProxy != ""
}

// Load reads every setting through getenv and validates the result. A nil
// getenv reads the real process environment.
func Load(getenv func(string) string) (Config, error) {
	return LoadWithBuildDir(getenv, DefaultBuildDir)
}

// LoadWithBuildDir is Load for a frontend image whose build lives elsewhere
// than the app's: the site image (cmd/site-frontend) passes
// DefaultSiteBuildDir. Every variable keeps its name, only the default of
// FRONTEND_BUILD_DIR differs.
func LoadWithBuildDir(getenv func(string) string, defaultBuildDir string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	loader := envconf.New(getenv)

	cfg := Config{
		ListenAddr:      loader.String("LISTEN_ADDR", DefaultListenAddr),
		AdminListenAddr: loader.String("ADMIN_LISTEN_ADDR", DefaultAdminListenAddr),
		BuildDir:        loader.String("FRONTEND_BUILD_DIR", defaultBuildDir),
		RateLimitBurst:  loader.Int("RATE_LIMIT_BURST", DefaultRateLimitBurst, 1, 100_000),
		RateLimitPer:    loader.Duration("RATE_LIMIT_PER", DefaultRateLimitPer, time.Second, time.Hour),
		AllowPlainHTTP:  loader.Bool("ALLOW_PLAIN_HTTP", false),
		// The literal name, so a search for the variable finds it here.
		DevAPIProxy: loader.String("FRONTEND_DEV_API_PROXY", ""),
	}

	var problems []error
	fail := func(err error) {
		if err != nil {
			problems = append(problems, err)
		}
	}

	level, err := logging.Level(loader.String("LOG_LEVEL", DefaultLogLevel))
	fail(wrap("LOG_LEVEL", err))
	cfg.LogLevel = level

	prefixes, err := clientip.ParseCIDRs(getenv("TRUSTED_PROXIES"))
	fail(wrap("TRUSTED_PROXIES", err))
	cfg.TrustedProxies = prefixes

	if cfg.AdminListenAddr == "" {
		fail(wrap("ADMIN_LISTEN_ADDR", errors.New("must not be empty, the probes and the metrics live there")))
	}
	if cfg.BuildDir == "" {
		fail(wrap("FRONTEND_BUILD_DIR", errors.New("must not be empty")))
	}

	if err := errors.Join(append(problems, loader.Err())...); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// wrap names the variable a problem came from, or returns nil.
func wrap(name string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("frontendconfig: %s: %w", name, err)
}
