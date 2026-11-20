// Package httpapi builds the HTTP server solobill answers requests with:
// health and metrics endpoints, the local login, logout, "who am I" and
// invite routes, the OIDC routes when internal/auth's OIDC path is
// configured, and the SPA fallback that serves the embedded Vue build for
// everything else.
//
// Every dependency is constructed by cmd/solobill's main and handed to
// NewHandler through Deps; this package builds no database connection, no
// session store and no OIDC provider of its own. See Deps for the full
// list.
//
// # Two handlers, two listeners
//
// NewHandler builds two handlers, not one: Public, which cmd/solobill runs
// through govue's httpserver.Config on the listener its Ingress routes to,
// and Admin, which it runs through httpserver.AdminConfig on a second
// listener reached only inside the cluster. /healthz, /readyz and
// /metrics live on Admin and nowhere on Public: anything that can reach
// this application over its Ingress could otherwise scrape its metrics or
// poll its probes, which is the vulnerability this split closes. See
// govue's own example/app.go (adminHandler) and docs/concepts/govue.md
// for the shape this reproduces, and cmd/solobill's main for how the two
// listeners are run and drained together.
//
// # The middleware shape
//
// Public gets two chains, not one, the same shape govue's own example
// application documents and that internal/httpapi's task description
// reproduces verbatim: an outer chain wrapping the whole mux (Recover,
// RequestID, the security headers, the HTTP metrics, the access log) and
// one per-route chain applied to every handler the mux dispatches to
// (CORS when configured, the rate limiter, then the CSRF protector, in
// that order: see govue's example/app.go's own handler method). Every
// route goes through the same chain; there is no second, tighter bucket,
// since every route shares one rateLimit.Limiter instance regardless.
//
// Admin gets a deliberately short chain instead, matching govue's own
// adminHandler: Recover and RequestID only. A probe or a scrape is not
// browser traffic, so the security headers and CORS have nothing to
// protect; it is not a request a user can forge, so the CSRF protector
// has nothing to check; and a scraper or kubelet polling this listener
// every few seconds must not be throttled by the rate limiter or fill the
// access log the way a public request would. The HTTP metrics middleware
// is absent too, deliberately: instrumenting a scrape of the metrics
// endpoint itself would feed Prometheus a metric about scraping
// Prometheus, which is not a loop worth closing.
//
// # Tenant resolution for local login and invite accept
//
// Every local login and invite-accept request carries its own tenant slug
// field in its JSON body, resolved against storage.TenantRepository before
// anything else runs: see loginRequest and acceptInviteRequest. A tenant
// slug is not a secret (internal/auth's own LocalAuthenticator.Login doc
// comment makes the same call for the same reason), it is the value a
// login page already has to know to render itself, so resolving it ahead
// of the password and TOTP checks leaks nothing a browser did not already
// know.
//
// OIDC resolves differently, to a single configured tenant; see
// internal/config's package doc and internal/auth's package doc for why a
// tenant slug carried on the callback request itself is not an option this
// milestone has.
//
// POST /api/v1/invites/oidc/start is the one exception: it takes a tenant
// slug and an invite token in its body, the same as acceptInviteRequest,
// and signs them into a short lived cookie (auth.StartOIDCInviteAccept)
// the OIDC callback reads back once the browser returns from the
// identity provider; see handleInviteOIDCStart and
// auth.NewOIDCHandlers's own doc for the rest of that flow.
package httpapi

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/ostendorf-cloud/govue/api"
	"github.com/ostendorf-cloud/govue/clientip"
	"github.com/ostendorf-cloud/govue/csrf"
	"github.com/ostendorf-cloud/govue/health"
	"github.com/ostendorf-cloud/govue/metrics"
	"github.com/ostendorf-cloud/govue/middleware"
	"github.com/ostendorf-cloud/govue/oidc"
	"github.com/ostendorf-cloud/govue/ratelimit"
	"github.com/ostendorf-cloud/govue/session"

	"github.com/ostendorf-cloud/solobill/internal/auth"
)

// maxRateLimitKeys bounds the number of distinct client addresses the rate
// limiter tracks at once, matching govue's own example application: a
// flood of one-shot addresses must not grow that map without bound.
const maxRateLimitKeys = 100_000

// defaultInviteTTL is Deps.DefaultInviteTTL's fallback when left zero.
const defaultInviteTTL = 7 * 24 * time.Hour

// oidcLoginPath and oidcCallbackPath are where the OIDC routes are mounted
// when OIDC is enabled. OIDC_REDIRECT_URL's path (see internal/config) must
// equal oidcCallbackPath: the identity provider is told to redirect back to
// exactly that URL, and this is the one route that answers it.
const (
	oidcLoginPath    = "/auth/oidc/login"
	oidcCallbackPath = "/auth/oidc/callback"
)

// Deps is everything NewHandler needs, constructed by cmd/solobill's main:
// nothing in this package opens a database connection, builds a session
// store or constructs an OIDC provider.
type Deps struct {
	Logger *slog.Logger

	// Store is read directly by this package only for Tenants().FindBySlug
	// (resolving a login or invite-accept request's tenant slug, and
	// resolving the invite's tenant when a request reaches
	// /api/v1/invites/accept with one). It is otherwise reached through
	// LocalAuth and Invites.
	Store auth.Store

	Sessions   *session.Manager
	LoginCodec *session.Codec // nil when OIDCHandlers is nil.
	CSRFKey    []byte

	LocalAuth *auth.LocalAuthenticator
	Invites   *auth.InviteManager

	// OIDCHandlers is nil when OIDC is disabled (config.Config.OIDCEnabled
	// false): no OIDC route is registered in that case.
	OIDCHandlers *oidc.Handlers

	// HealthRegistry already carries every readiness check main wants
	// (the database ping); this package only mounts it, on Admin, never
	// on Public.
	HealthRegistry *health.Registry

	AllowedOrigins []string
	TrustedProxies []netip.Prefix

	// AllowPlainHTTP mirrors config.Config.SessionAllowPlainHTTP: it only
	// affects the security headers' HSTS field here, the cookies
	// themselves are already built accordingly by whatever constructed
	// Sessions and LoginCodec.
	AllowPlainHTTP bool

	RateLimitBurst int
	RateLimitPer   time.Duration

	// DefaultInviteTTL is how long a freshly created invite stays
	// redeemable when a create-invite request does not specify its own
	// ttl field (see createInviteRequest).
	DefaultInviteTTL time.Duration

	// DistFS is the built SPA (cmd/solobill's embedded dist/, already
	// stripped to its own root via fs.Sub), served as the fallback for any
	// unmatched GET that is not under /api/.
	DistFS fs.FS

	// Now is the clock the access log timestamps with. Nil is time.Now;
	// tests replace it.
	Now func() time.Time

	// Repositories is every repository of the year end milestone. Each
	// field may be nil until the issue owning it merges; see
	// Repositories.
	Repositories Repositories
}

// Handlers is what NewHandler builds: Public, served on the listener the
// application's Ingress routes to, and Admin, served on govue's second,
// administrative listener. See the package doc's "Two handlers, two
// listeners" section for why these are never the same handler.
type Handlers struct {
	Public http.Handler
	Admin  http.Handler

	// reserved maps every pattern still answered by a 501 stub to the
	// routes_<resource>.go that registered it, read by the tests through
	// export_test.go.
	reserved map[string]string

	// repos is the Repositories the handlers were built with, read by
	// the tests through export_test.go to prove the test hooks reach
	// NewHandler.
	repos Repositories
}

// NewHandler builds both handlers, returning every construction problem at
// once rather than stopping at the first one, the same pattern config.Load
// and govue's own example application both use.
func NewHandler(d Deps) (Handlers, error) {
	var problems []error
	fail := func(err error) {
		if err != nil {
			problems = append(problems, err)
		}
	}

	if d.Logger == nil {
		fail(errors.New("httpapi: logger is required"))
	}
	if d.Sessions == nil {
		fail(errors.New("httpapi: sessions is required"))
	}
	if d.LocalAuth == nil {
		fail(errors.New("httpapi: local authenticator is required"))
	}
	if d.Invites == nil {
		fail(errors.New("httpapi: invite manager is required"))
	}
	if d.Store == nil {
		fail(errors.New("httpapi: store is required"))
	}
	if d.HealthRegistry == nil {
		fail(errors.New("httpapi: health registry is required"))
	}
	if d.DistFS == nil {
		fail(errors.New("httpapi: dist filesystem is required"))
	}
	if (d.OIDCHandlers == nil) != (d.LoginCodec == nil) {
		fail(errors.New("httpapi: oidc handlers and login codec must both be set or both be nil"))
	}

	clientIPs := clientip.NewResolver(d.TrustedProxies)

	protector, err := csrf.New(func(r *http.Request) (string, bool) {
		s, ok, err := d.Sessions.Read(r.Context(), r)
		if err != nil || !ok {
			return "", false
		}

		return csrf.Token(d.CSRFKey, s.ID), true
	})
	fail(err)
	if protector != nil {
		protector.OnRefusal(func(w http.ResponseWriter, r *http.Request, _ error) {
			api.WriteError(w, r, api.PermissionDenied("cross origin or stale request, please reload"))
		})
		for _, origin := range d.AllowedOrigins {
			fail(protector.TrustOrigin(origin))
		}
	}

	// A same-origin deployment, the default for this milestone (the SPA
	// is served by this same process, see Deps.DistFS), needs no CORS
	// middleware at all; middleware.NewCORS itself refuses to be built
	// with zero origins. See the package doc.
	var cors *middleware.CORS
	if len(d.AllowedOrigins) > 0 {
		cors, err = middleware.NewCORS(middleware.CORSConfig{
			Origins:     d.AllowedOrigins,
			Headers:     []string{"Content-Type", csrf.HeaderName},
			Credentials: true,
		})
		fail(err)
	}

	limiter := ratelimit.New(d.RateLimitBurst, d.RateLimitPer, maxRateLimitKeys)
	rateLimit, err := middleware.NewRateLimit(middleware.RateLimitConfig{Limiter: limiter, ClientIP: clientIPs})
	fail(err)

	security := middleware.DefaultSecurity()
	if d.AllowPlainHTTP {
		// A browser reached over plain HTTP must never be told to
		// upgrade every future visit to HTTPS.
		security.HSTS = 0
	}

	accessLog := middleware.AccessLog{ClientIP: clientIPs, Now: d.Now}

	metricsRegistry := metrics.NewRegistry()
	httpMetrics := metrics.NewHTTP(metricsRegistry)
	metricsHandler := metrics.Handler(metricsRegistry)

	var spaHandler http.Handler
	if d.DistFS != nil {
		spaHandler, err = newSPAHandler(d.DistFS)
		fail(err)
	}

	if err := errors.Join(problems...); err != nil {
		return Handlers{}, fmt.Errorf("httpapi: build handler: %w", err)
	}

	inviteTTL := d.DefaultInviteTTL
	if inviteTTL <= 0 {
		inviteTTL = defaultInviteTTL
	}

	a := &app{
		logger:           d.Logger,
		store:            d.Store,
		sessions:         d.Sessions,
		csrfKey:          d.CSRFKey,
		local:            d.LocalAuth,
		invites:          d.Invites,
		defaultInviteTTL: inviteTTL,
		loginCodec:       d.LoginCodec,
		allowPlainHTTP:   d.AllowPlainHTTP,
		oidcHandlers:     d.OIDCHandlers,
		repos:            d.Repositories,
		reserved:         map[string]string{},
	}

	// route is the one per-handler chain, outermost first: CORS (when
	// configured), the rate limiter, then the CSRF protector, applied to
	// every route alike. It must run on the handler the mux dispatches
	// to, not in the outer chain: a refusal answered ahead of the mux
	// would be logged and counted against "unmatched" instead of the
	// real route. See middleware.RateLimit's and metrics.HTTP.Middleware's
	// own doc comments, and govue's example/app.go for the canonical
	// order this reproduces.
	route := func(h http.Handler) http.Handler {
		mws := corsMiddleware(cors)
		mws = append(mws, rateLimit.Middleware, protector.Middleware)

		return middleware.Chain(h, mws...)
	}

	mux := http.NewServeMux()

	// One register function per resource file (routes_*.go), listed in
	// routes.go's registrations. This function is not edited again by a
	// feature issue: see AGENTS.md's conflict zone rule.
	for _, register := range registrations {
		register(mux, route, a)
	}

	// /metrics, /healthz and /readyz are deliberately registered here as
	// a 404, not left to fall through to the SPA catch-all below: Admin
	// serves the real answers instead (see the package doc's "Two
	// handlers, two listeners" section), and the SPA fallback answers
	// every unrecognised path outside /api/ with its own shell and a 200,
	// which would otherwise hide the fact that nothing real is behind
	// these three paths on this listener. Registered with no method, so
	// every method gets the 404, not only GET, and as both the exact path
	// and its trailing-slash subtree, so a path under it (e.g.
	// /metrics/anything) cannot fall through to the catch-all either.
	for _, path := range [...]string{"/metrics", "/healthz", "/readyz"} {
		mux.Handle(path, route(api.NotFoundHandler()))
		mux.Handle(path+"/", route(api.NotFoundHandler()))
	}

	// The SPA fallback. It answers an unmatched /api/ path with the same
	// JSON error shape every other endpoint uses, and everything else
	// with the built Vue app, for client-side routing to take over.
	mux.Handle("/", route(spaHandler))

	// The outer chain, outermost first: see the package doc for why each
	// position is forced. RequestID and the security middleware both call
	// (*http.Request).WithContext, which both httpMetrics and accessLog
	// need to not have happened yet, since they read r.Pattern off the
	// exact request value http.ServeMux matched against.
	public := middleware.Chain(mux,
		middleware.Recover(d.Logger),
		middleware.RequestID(d.Logger),
		security.Middleware,
		httpMetrics.Middleware,
		accessLog.Middleware,
	)

	// adminMux carries /healthz, /readyz and /metrics, and nothing else:
	// see the package doc's "Two handlers, two listeners" section for why
	// these never reach Public. metricsHandler reads from the same
	// metricsRegistry httpMetrics.Middleware above records into, so a
	// scrape here reports the traffic Public actually served.
	adminMux := http.NewServeMux()
	d.HealthRegistry.Handle(adminMux)
	adminMux.Handle("GET /metrics", metricsHandler)

	// The admin chain, deliberately short: see the package doc's
	// "The middleware shape" section for why only Recover and RequestID
	// belong here.
	admin := middleware.Chain(adminMux,
		middleware.Recover(d.Logger),
		middleware.RequestID(d.Logger),
	)

	return Handlers{Public: public, Admin: admin, reserved: a.reserved, repos: a.repos}, nil
}

// corsMiddleware returns cors.Middleware as a one element slice, or an
// empty slice when cors is nil (no origins configured): a helper so route
// and limited above do not each repeat the nil check.
func corsMiddleware(cors *middleware.CORS) []middleware.Middleware {
	if cors == nil {
		return nil
	}

	return []middleware.Middleware{cors.Middleware}
}

// app holds the dependencies the handlers in this package call.
type app struct {
	logger *slog.Logger

	store auth.Store

	sessions *session.Manager
	csrfKey  []byte

	local   *auth.LocalAuthenticator
	invites *auth.InviteManager

	defaultInviteTTL time.Duration

	// loginCodec and allowPlainHTTP back handleInviteOIDCStart's pending-
	// invite cookie (auth.StartOIDCInviteAccept): nil loginCodec, exactly
	// like Deps.LoginCodec itself, means OIDC is disabled, and that
	// handler is not mounted in that case either.
	loginCodec     *session.Codec
	allowPlainHTTP bool

	// oidcHandlers is Deps.OIDCHandlers, nil when OIDC is disabled.
	oidcHandlers *oidc.Handlers

	// repos is Deps.Repositories: every repository the year end
	// milestone's handlers call. See Repositories.
	repos Repositories

	// reserved maps every pattern stub registered to the file that
	// registered it, for Handlers.reserved.
	reserved map[string]string
}
