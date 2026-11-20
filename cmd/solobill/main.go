// Command solobill wires the service: configuration, the database
// connection, session and OIDC construction, the HTTP handler, and
// graceful shutdown. See internal/config, internal/httpapi and
// internal/auth for what each piece actually does; this file only
// assembles them in order.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/health"
	"github.com/ostendorf-cloud/govue/httpserver"
	"github.com/ostendorf-cloud/govue/logging"
	"github.com/ostendorf-cloud/govue/oidc"
	"github.com/ostendorf-cloud/govue/session"

	"github.com/ostendorf-cloud/solobill/internal/auth"
	"github.com/ostendorf-cloud/solobill/internal/config"
	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres"
)

func main() {
	code, msg := exitWith(dispatch(os.Args[1:], os.Stdout, run))
	if msg != "" {
		fmt.Fprintln(os.Stderr, msg)
	}
	if code != 0 {
		os.Exit(code)
	}
}

// maxSessions bounds session.NewMemoryStore's tracked session count,
// deliberately rather than leaving it at govue's own default of 10000
// (the floor session.NewMemoryStore clamps a non-positive value to),
// which is sized for a SaaS with many concurrent users, not for this
// application's actual scale.
//
// A Session is a handful of short strings (an id, a subject, an email, a
// name, a two-entry Data map for tenant_id and role) plus three
// time.Time fields: comfortably under 1 KiB each including map and
// runtime overhead, so even 1000 of them cost on the order of 1 MiB,
// against charts/solobill/values.yaml's 256Mi memory limit. A session is
// only ever created by a login that already succeeded (a verified local
// password plus TOTP code, a verified OIDC identity, or a redeemed
// single-use invite token), never by an anonymous request, so nothing
// short of a credential compromise can drive the store toward this
// bound. docs/concepts/solobill.md frames this application's own scale
// as "a single-user start" where "a second tenant can be added without a
// data migration": an owner plus a handful of invited members, per
// tenant, for however many tenants one deployment serves. 1000 is
// generous headroom over that, with room for a session per device per
// user and for the up-to-a-minute lag before session.MemoryStore's sweep
// reclaims one past session.DefaultIdle (2h) or session.DefaultLifetime
// (12h), while keeping the worst case a known, small number of
// megabytes instead of inheriting a bound sized for a different kind of
// deployment.
const maxSessions = 1000

func run() error {
	if err := checkEmbeddedDist(); err != nil {
		return err
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	logger := logging.New(os.Stdout, cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPoolWithCredentials(ctx, cfg.DatabaseURL, cfg.DatabaseUser, cfg.DatabasePassword)
	if err != nil {
		return fmt.Errorf("solobill: open database: %w", err)
	}
	defer pool.Close()

	store := postgres.NewStore(pool)

	if err := seedTenant(ctx, store, cfg.SeedTenantSlug, cfg.SeedTenantName); err != nil {
		return fmt.Errorf("solobill: %w", err)
	}

	if err := seedOwnerAccount(ctx, store, cfg.SeedTenantSlug, cfg.SeedOwnerEmail, cfg.SeedOwnerOIDCSubject); err != nil {
		return fmt.Errorf("solobill: %w", err)
	}

	sessions, err := session.New(session.Config{
		Store:          session.NewMemoryStore(nil, maxSessions),
		AllowPlainHTTP: cfg.SessionAllowPlainHTTP,
	})
	if err != nil {
		return fmt.Errorf("solobill: build session manager: %w", err)
	}

	loginCodec, err := session.NewCodec(session.CodecConfig{
		Key:            cfg.SessionCodecKey,
		AllowPlainHTTP: cfg.SessionAllowPlainHTTP,
	})
	if err != nil {
		return fmt.Errorf("solobill: build login codec: %w", err)
	}

	localAuth := auth.NewLocalAuthenticator(store, nil)
	invites := auth.NewInviteManager(store, nil)

	healthRegistry := health.New()
	healthRegistry.Add("database", 0, pool.Ping)

	oidcHandlers, err := buildOIDCHandlers(cfg, store, invites, loginCodec, sessions)
	if err != nil {
		return fmt.Errorf("solobill: %w", err)
	}

	spaFS, err := fs.Sub(distFS, "dist")
	if err != nil {
		return fmt.Errorf("solobill: %w", err)
	}

	repos, err := buildRepositories(ctx, wireEnv{cfg: cfg, logger: logger, store: store, health: healthRegistry})
	if err != nil {
		return fmt.Errorf("solobill: %w", err)
	}

	handlers, err := httpapi.NewHandler(httpapi.Deps{
		Logger:           logger,
		Store:            store,
		Sessions:         sessions,
		LoginCodec:       oidcLoginCodec(cfg, loginCodec),
		CSRFKey:          cfg.CSRFKey,
		LocalAuth:        localAuth,
		Invites:          invites,
		OIDCHandlers:     oidcHandlers,
		HealthRegistry:   healthRegistry,
		AllowedOrigins:   cfg.AllowedOrigins,
		TrustedProxies:   cfg.TrustedProxies,
		AllowPlainHTTP:   cfg.SessionAllowPlainHTTP,
		RateLimitBurst:   cfg.RateLimitBurst,
		RateLimitPer:     cfg.RateLimitPer,
		DefaultInviteTTL: cfg.DefaultInviteTTL,
		DistFS:           spaFS,
		Repositories:     repos,
	})
	if err != nil {
		return fmt.Errorf("solobill: build http handler: %w", err)
	}

	return httpserver.RunAdmin(ctx, httpserver.Config{
		Addr:    cfg.ListenAddr,
		Handler: handlers.Public,
		Logger:  logger,
		// Failing the readiness probe before the drain delay starts is
		// what lets a load balancer stop sending new requests before
		// either listener stops accepting connections. This runs once,
		// for both listeners together; see httpserver.ServeAdmin's own
		// doc comment.
		BeforeShutdown: healthRegistry.Drain,
	}, httpserver.AdminConfig{
		Addr:    cfg.AdminListenAddr,
		Handler: handlers.Admin,
	})
}

// buildOIDCHandlers builds the OIDC provider and handlers when cfg enables
// OIDC, or returns nil, nil when it does not: internal/httpapi registers no
// OIDC route in that case.
//
// resolveTenant, the TenantResolver internal/auth's OIDCHandlerOptions
// needs, always resolves to the single configured OIDC_DEFAULT_TENANT_SLUG
// tenant rather than reading one off the callback request: see
// internal/config's package doc for why that is this milestone's only
// option, not merely its simplest one.
func buildOIDCHandlers(
	cfg config.Config,
	store auth.Store,
	invites *auth.InviteManager,
	loginCodec *session.Codec,
	sessions *session.Manager,
) (*oidc.Handlers, error) {
	if !cfg.OIDCEnabled {
		return nil, nil //nolint:nilnil // no OIDC configured is not an error, it is this milestone's default
	}

	provider, err := auth.NewOIDCProvider(auth.OIDCProviderConfig{
		Issuer:       cfg.OIDCIssuerURL,
		ClientID:     cfg.OIDCClientID,
		ClientSecret: cfg.OIDCClientSecret,
		RedirectURL:  cfg.OIDCRedirectURL,
		Scopes:       cfg.OIDCScopes,
	})
	if err != nil {
		return nil, fmt.Errorf("build oidc provider: %w", err)
	}

	resolveTenant := func(r *http.Request) (uuid.UUID, error) {
		tenant, err := store.Tenants().FindBySlug(r.Context(), cfg.OIDCDefaultTenantSlug)
		if err != nil {
			return uuid.Nil, fmt.Errorf("resolve default oidc tenant %q: %w", cfg.OIDCDefaultTenantSlug, err)
		}

		return tenant.ID, nil
	}

	handlers, err := auth.NewOIDCHandlers(provider, loginCodec, sessions, store, invites, resolveTenant, auth.OIDCHandlerOptions{
		AllowPlainHTTP: cfg.SessionAllowPlainHTTP,
	})
	if err != nil {
		return nil, fmt.Errorf("build oidc handlers: %w", err)
	}

	return handlers, nil
}

// oidcLoginCodec returns loginCodec when OIDC is enabled, or nil when it is
// not: internal/httpapi.Deps requires its OIDCHandlers and LoginCodec
// fields to agree on whether OIDC is wired up, and building a Codec that
// nothing would ever read from is not worth doing just to fill the field.
func oidcLoginCodec(cfg config.Config, loginCodec *session.Codec) *session.Codec {
	if !cfg.OIDCEnabled {
		return nil
	}

	return loginCodec
}

// tenantSeeder is the one method seedTenant needs from a storage
// backend. Defined here, at the consumer, rather than reusing
// internal/auth.Store: it is a strict subset, and a test fake for it
// needs only a storage.TenantRepository, not also
// storage.TenantScope.WithTenant, Accounts() and Invites(), which every
// other method on internal/auth.Store would otherwise require.
type tenantSeeder interface {
	Tenants() storage.TenantRepository
}

// seedTenant creates the tenant named by slug and name if it does not
// already exist, and does nothing at all when slug is empty: this keeps
// store.Tenants() unread in that case, which is what the common,
// production case (SEED_TENANT_SLUG unset, see internal/config) needs to
// stay a true no-op rather than an extra lookup on every start.
//
// docker-compose.yaml is the only caller that sets slug and name
// (through SEED_TENANT_SLUG/SEED_TENANT_NAME): a from-scratch local
// Postgres has no tenant at all yet, and typing one in by hand through
// the API before anything else works is exactly the chicken-and-egg
// problem this sidesteps.
func seedTenant(ctx context.Context, store tenantSeeder, slug, name string) error {
	if slug == "" {
		return nil
	}

	if _, err := store.Tenants().FindBySlug(ctx, slug); err == nil {
		return nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("seed tenant %q: %w", slug, err)
	}

	if _, err := store.Tenants().Create(ctx, storage.Tenant{Slug: slug, Name: name}); err != nil {
		return fmt.Errorf("seed tenant %q: %w", slug, err)
	}

	return nil
}

// ownerSeeder is what seedOwnerAccount needs from a storage backend: a
// strict subset of internal/auth.Store, the same reasoning tenantSeeder
// above gives for not reusing that interface wholesale. A test fake for
// it needs no InviteRepository at all, since seedOwnerAccount never
// touches one.
type ownerSeeder interface {
	storage.TenantScope

	Tenants() storage.TenantRepository
	Accounts() storage.AccountRepository
}

// seedOwnerAccount creates the first owner account for tenantSlug,
// bound to subject, if subject is empty (the production default) or if
// an account for this subject or this email already exists.
//
// internal/auth's own admission gate (see its package doc's "OIDC on a
// first-time subject" section) requires an existing invite or an
// already-provisioned account before any OIDC login succeeds, and both
// of those in turn require an existing owner: an invite needs one to
// create it, and there is no account to log in with yet either. A brand
// new instance has neither, so this is the one exception, scoped to
// solobill's own startup code and run by whoever deploys it, never
// reachable over the network: it provisions the account directly from
// SEED_OWNER_EMAIL/SEED_OWNER_OIDC_SUBJECT (see internal/config), the
// same way seedTenant above already seeds the first tenant from its own
// configuration.
//
// It never overwrites or rebinds an existing row: an account already
// bound to subject is left alone (already provisioned, nothing to do),
// and an account that already owns email but is bound to a different
// subject, or to none, is also left alone rather than rebound to
// subject here. That second case is a configuration mismatch (the
// deployer's SEED_OWNER_OIDC_SUBJECT no longer matches what was seeded
// last time) for a human to resolve, not something startup silently
// fixes by rebinding an account's identity out from under it, exactly
// the property the vulnerability this issue fixes is built around: see
// internal/auth's package doc.
func seedOwnerAccount(ctx context.Context, store ownerSeeder, tenantSlug, email, subject string) error {
	if subject == "" {
		return nil
	}

	tenant, err := store.Tenants().FindBySlug(ctx, tenantSlug)
	if err != nil {
		return fmt.Errorf("seed owner account: find tenant %q: %w", tenantSlug, err)
	}

	err = store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
		if _, err := store.Accounts().FindByOIDCSubject(ctx, subject); err == nil {
			return nil
		} else if !errors.Is(err, storage.ErrNotFound) {
			return fmt.Errorf("look up account by oidc subject: %w", err)
		}

		if _, err := store.Accounts().FindByEmail(ctx, email); err == nil {
			return nil
		} else if !errors.Is(err, storage.ErrNotFound) {
			return fmt.Errorf("look up account by email: %w", err)
		}

		ownerSubject := subject

		if _, err := store.Accounts().Create(ctx, storage.Account{
			TenantID:    tenant.ID,
			Email:       email,
			OIDCSubject: &ownerSubject,
			Role:        storage.AccountRoleOwner,
			Status:      storage.AccountStatusActive,
		}); err != nil {
			return fmt.Errorf("create account: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("seed owner account %q: %w", email, err)
	}

	return nil
}

// checkEmbeddedDist proves the embedded dist directory is shaped the way
// fs.Sub expects: a build with an empty or mis-shaped frontend build fails
// here instead of deep inside httpapi.NewHandler.
func checkEmbeddedDist() error {
	if _, err := fs.Sub(distFS, "dist"); err != nil {
		return fmt.Errorf("solobill: %w", err)
	}

	return nil
}
