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
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

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

	dsn, err := cfg.DatabaseDSN()
	if err != nil {
		return fmt.Errorf("solobill: %w", err)
	}

	pool, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		return fmt.Errorf("solobill: open database: %w", err)
	}
	defer pool.Close()

	store := postgres.NewStore(pool)

	if err := seedTenant(ctx, store, cfg.SeedTenantSlug, cfg.SeedTenantName); err != nil {
		return fmt.Errorf("solobill: %w", err)
	}

	sessions, err := session.New(session.Config{
		Store:          session.NewMemoryStore(nil),
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

	oidcHandlers, err := buildOIDCHandlers(cfg, store, loginCodec, sessions)
	if err != nil {
		return fmt.Errorf("solobill: %w", err)
	}

	spaFS, err := fs.Sub(distFS, "dist")
	if err != nil {
		return fmt.Errorf("solobill: %w", err)
	}

	handler, err := httpapi.NewHandler(httpapi.Deps{
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
	})
	if err != nil {
		return fmt.Errorf("solobill: build http handler: %w", err)
	}

	return httpserver.Run(ctx, httpserver.Config{
		Addr:    cfg.ListenAddr,
		Handler: handler,
		Logger:  logger,
		// Failing the readiness probe before the drain delay starts is
		// what lets a load balancer stop sending new requests before the
		// server stops accepting connections at all.
		BeforeShutdown: healthRegistry.Drain,
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

	handlers, err := auth.NewOIDCHandlers(provider, loginCodec, sessions, store, resolveTenant, auth.OIDCHandlerOptions{})
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

// checkEmbeddedDist proves the embedded dist directory is shaped the way
// fs.Sub expects: a build with an empty or mis-shaped frontend build fails
// here instead of deep inside httpapi.NewHandler.
func checkEmbeddedDist() error {
	if _, err := fs.Sub(distFS, "dist"); err != nil {
		return fmt.Errorf("solobill: %w", err)
	}

	return nil
}
