// Package config loads solobill's configuration from the environment once,
// at startup, and validates it fully before anything listens: see Load.
//
// Every value, secret included, is read through govue's envconf.Loader.
// A secret's own variable (for example DATABASE_PASSWORD) may carry the
// value directly, or the same name with a "_FILE" suffix
// (DATABASE_PASSWORD_FILE) may point at a file holding it, and setting both
// at once is an error rather than a silent precedence rule: that is the
// shape a Kubernetes Secret mounted as a file and a plain environment value
// both need to fit through the same configuration surface. That contract is
// envconf.Loader.SecretOrFile and RequiredSecretOrFile, not
// envconf.Loader.Secret: Secret treats the named environment variable as
// unconditionally a file path, which is a different and narrower contract
// than this package needs. This package carried its own, identical
// hand-rolled version of SecretOrFile/RequiredSecretOrFile until govue
// v0.3.0 added them upstream.
package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"time"

	"github.com/ostendorf-cloud/govue/clientip"
	"github.com/ostendorf-cloud/govue/envconf"
	"github.com/ostendorf-cloud/govue/logging"
	"github.com/ostendorf-cloud/govue/session"
)

// minKeyBytes is the shortest decoded key Load accepts for a signing
// secret, matching session.MinKeyBytes: a key shorter than this is a
// deployment mistake, not a value to run with anyway.
const minKeyBytes = session.MinKeyBytes

// defaultRateLimitBurst and defaultRateLimitPer are the token bucket this
// service applies to its brute-force-sensitive endpoints (local login,
// invite accept) when RATE_LIMIT_BURST and RATE_LIMIT_PER are left unset:
// ten attempts a minute, per client, which is generous for a person typing
// a password and a TOTP code and tight for a script guessing either.
const (
	defaultRateLimitBurst = 10
	defaultRateLimitPer   = time.Minute
)

// defaultInviteTTL is CreateInvite's default lifetime when a caller leaves
// an invite's TTL unspecified. It is not itself a config value: the issue's
// plan (internal/auth.CreateInviteParams.TTL) leaves this as the HTTP
// handler's call, see internal/httpapi.
const defaultInviteTTL = 7 * 24 * time.Hour

// Config is every setting solobill reads from the environment, loaded and
// validated once by Load.
type Config struct {
	ListenAddr string
	// AdminListenAddr is the second, administrative listener
	// cmd/solobill runs through govue's httpserver.RunAdmin: /metrics,
	// /healthz and /readyz are served there and nowhere on ListenAddr,
	// so the Ingress ListenAddr is routed through never reaches them.
	// See internal/httpapi's package doc for the two handlers this
	// splits into.
	AdminListenAddr string
	LogLevel        slog.Level

	// DatabaseURL is the connection URL without credentials: scheme, host,
	// port, database name and query parameters only. DatabaseUser and
	// DatabasePassword are assembled into it by DSN, rather than three
	// different ways of saying the same thing: see the issue's own framing
	// of "the chart takes a connection URL, user and password as
	// configuration".
	DatabaseURL      string
	DatabaseUser     string
	DatabasePassword string

	// SessionCodecKey signs the short-lived login-in-flight cookie an OIDC
	// login uses between the redirect to the identity provider and the
	// callback (session.Codec, passed to internal/auth.NewOIDCHandlers as
	// its LoginStore). CSRFKey derives the CSRF token from a session id.
	// The two are deliberately separate keys: each is an HMAC key for a
	// different purpose, and reusing one key for two purposes is a
	// cryptographic smell with no upside here, since generating a second
	// key costs nothing.
	SessionCodecKey []byte
	CSRFKey         []byte

	// SessionAllowPlainHTTP drops the Secure attribute (and the __Host-
	// cookie prefix it is a precondition of) from both the session and the
	// login-in-flight cookies, which is what lets a session survive a
	// plain HTTP docker-compose run during local development. A real
	// deployment sits behind a TLS-terminating ingress and must leave this
	// false: without Secure, the session id and the signed login state
	// both travel in the clear.
	SessionAllowPlainHTTP bool

	// OIDCEnabled is true exactly when OIDCIssuerURL is set. OIDC is
	// optional as a whole for this milestone: a single sole trader's own
	// tenant does not need an identity provider at all, and the
	// acceptance criteria for `docker compose up` do not require a live
	// Keycloak. When false, every other OIDC* field is empty and
	// internal/httpapi never registers an OIDC route.
	OIDCEnabled      bool
	OIDCIssuerURL    string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string
	// OIDCScopes defaults to oidc.DefaultScopes when empty. A deployment
	// that wants Keycloak's realm roles in the token adds the scope its
	// client mapper maps them under (commonly "roles") here.
	OIDCScopes []string
	// OIDCDefaultTenantSlug is the tenant every OIDC login resolves to.
	// See the package doc section "Resolving a tenant for OIDC" for why a
	// single configured tenant, rather than a tenant carried on the
	// callback request itself, is this milestone's answer.
	OIDCDefaultTenantSlug string

	// AllowedOrigins is the CORS allow-list for a frontend served from a
	// different origin than this API. Empty is the common case for this
	// milestone, where cmd/solobill embeds and serves the built SPA
	// itself: a same-origin deployment needs no CORS middleware at all,
	// and internal/httpapi does not build one when this is empty.
	AllowedOrigins []string
	// TrustedProxies is the set of CIDRs whose X-Forwarded-For entry
	// clientip.Resolver trusts as the real client address. Empty, the
	// default, trusts only the direct TCP peer.
	TrustedProxies []netip.Prefix

	// RateLimitBurst and RateLimitPer configure the token bucket
	// internal/httpapi applies to local login and invite accept.
	RateLimitBurst int
	RateLimitPer   time.Duration

	// DefaultInviteTTL is how long a freshly created invite stays
	// redeemable when the create-invite request does not specify one.
	DefaultInviteTTL time.Duration

	// SeedTenantSlug and SeedTenantName name a tenant cmd/solobill creates
	// on startup if it does not already exist, before it starts serving
	// anything (see cmd/solobill's seedTenant). Both empty, the default,
	// is a no-op: a real deployment leaves these unset, since there is no
	// safe way to seed a tenant's slug or name for a deployment this
	// package knows nothing about. docker-compose.yaml is the only
	// caller that sets them, so a fresh local Postgres has a tenant to
	// log into right after `docker compose up --build` instead of a
	// chicken-and-egg problem: no tenant exists yet, and nothing can
	// create one through the API without a tenant to belong to.
	SeedTenantSlug string
	SeedTenantName string

	// SeedOwnerEmail and SeedOwnerOIDCSubject name the first owner account
	// cmd/solobill creates for SeedTenantSlug on startup if neither an
	// account for this subject nor one for this email already exists (see
	// cmd/solobill's seedOwnerAccount). internal/auth's OIDC admission
	// gate requires an existing invite or an already-provisioned account
	// before any OIDC login succeeds, and a brand new instance has
	// neither: there is no owner yet to create an invite, this is the
	// exception that gets one. Both empty, the default, is a no-op.
	// Setting one without the other, or setting SeedOwnerOIDCSubject
	// without SeedTenantSlug, is an error: seeding an owner with no
	// tenant to belong to is a configuration mistake, not a request this
	// accepts.
	SeedOwnerEmail       string
	SeedOwnerOIDCSubject string
}

// Load reads every setting through getenv, collecting every problem into
// one error so a misconfigured deployment reports all of its mistakes on
// the first start instead of one per restart. A nil getenv reads the real
// process environment through os.Getenv.
//
// # Resolving a tenant for OIDC
//
// Every local login and every invite-accept request carries an explicit
// tenant slug field in its own body (see internal/httpapi): that request is
// entirely under this application's control, so adding a field costs
// nothing. An OIDC callback is different. govue's oidc.HandlerConfig calls
// StartSession, and through it internal/auth's TenantResolver, with only
// the callback's own *http.Request: the one value that round-trips a login
// attempt, oidc.Login.Next, is read and consumed by govue's own
// Handlers.Callback after StartSession already returned, never passed into
// it. There is no field on the callback request a tenant slug could travel
// in at all, encoding it into Next or anywhere else included: by the time
// TenantResolver would need it, it is already gone. A single configured
// tenant for every OIDC login is therefore not the simpler of two working
// options, it is the only one: see internal/auth's package doc and
// NewOIDCHandlers for the rest of this reasoning. OIDC_DEFAULT_TENANT_SLUG
// is that tenant, required whenever OIDC_ISSUER_URL is set.
func Load(getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	l := envconf.New(getenv)

	var problems []error
	fail := func(err error) {
		if err != nil {
			problems = append(problems, err)
		}
	}

	cfg := Config{
		ListenAddr:      l.String("LISTEN_ADDR", ":8080"),
		AdminListenAddr: l.String("ADMIN_LISTEN_ADDR", ":8081"),
	}

	level, err := logging.Level(l.String("LOG_LEVEL", "info"))
	fail(err)
	cfg.LogLevel = level

	cfg.DatabaseURL = l.Required("DATABASE_URL")
	cfg.DatabaseUser = l.Required("DATABASE_USER")

	cfg.DatabasePassword = l.RequiredSecretOrFile("DATABASE_PASSWORD")

	sessionKey, err := decodeHexKey("SESSION_CODEC_KEY", l.SecretOrFile("SESSION_CODEC_KEY"))
	fail(err)
	cfg.SessionCodecKey = sessionKey

	csrfKey, err := decodeHexKey("CSRF_KEY", l.SecretOrFile("CSRF_KEY"))
	fail(err)
	cfg.CSRFKey = csrfKey

	cfg.SessionAllowPlainHTTP = l.Bool("SESSION_ALLOW_PLAIN_HTTP", false)

	cfg.OIDCIssuerURL = l.String("OIDC_ISSUER_URL", "")
	cfg.OIDCEnabled = cfg.OIDCIssuerURL != ""
	if cfg.OIDCEnabled {
		cfg.OIDCClientID = l.Required("OIDC_CLIENT_ID")
		cfg.OIDCClientSecret = l.RequiredSecretOrFile("OIDC_CLIENT_SECRET")
		cfg.OIDCRedirectURL = l.Required("OIDC_REDIRECT_URL")
		cfg.OIDCDefaultTenantSlug = l.Required("OIDC_DEFAULT_TENANT_SLUG")
		cfg.OIDCScopes = l.List("OIDC_SCOPES")
	}

	cfg.AllowedOrigins = l.List("ALLOWED_ORIGINS")

	proxies, err := clientip.ParseCIDRs(l.String("TRUSTED_PROXIES", ""))
	fail(err)
	cfg.TrustedProxies = proxies

	cfg.RateLimitBurst = l.Int("RATE_LIMIT_BURST", defaultRateLimitBurst, 0, 100_000)
	cfg.RateLimitPer = l.Duration("RATE_LIMIT_PER", defaultRateLimitPer, time.Second, time.Hour)

	cfg.DefaultInviteTTL = l.Duration("INVITE_DEFAULT_TTL", defaultInviteTTL, time.Minute, 90*24*time.Hour)

	cfg.SeedTenantSlug = l.String("SEED_TENANT_SLUG", "")
	cfg.SeedTenantName = l.String("SEED_TENANT_NAME", "")
	if cfg.SeedTenantSlug != "" && cfg.SeedTenantName == "" {
		fail(errors.New("SEED_TENANT_NAME: is required when SEED_TENANT_SLUG is set"))
	}

	cfg.SeedOwnerEmail = l.String("SEED_OWNER_EMAIL", "")
	cfg.SeedOwnerOIDCSubject = l.String("SEED_OWNER_OIDC_SUBJECT", "")
	if (cfg.SeedOwnerEmail == "") != (cfg.SeedOwnerOIDCSubject == "") {
		fail(errors.New("SEED_OWNER_EMAIL and SEED_OWNER_OIDC_SUBJECT must either both be set or both be empty"))
	}
	if cfg.SeedOwnerOIDCSubject != "" && cfg.SeedTenantSlug == "" {
		fail(errors.New("SEED_TENANT_SLUG: is required when SEED_OWNER_OIDC_SUBJECT is set"))
	}

	if err := errors.Join(append(problems, l.Err())...); err != nil {
		return Config{}, fmt.Errorf("config: load: %w", err)
	}

	return cfg, nil
}

// decodeHexKey decodes raw as hex and checks its length, the same
// convention govue's own example application uses for a `*_KEY` secret: the
// key is generated, not typed, so a short one is a deployment mistake
// rather than a length to stretch to. An empty raw (neither the variable
// nor its _FILE twin set) decodes to zero bytes and fails the length check
// below with a message naming the right variable, so no separate "is
// required" check is needed for these two keys.
func decodeHexKey(name, raw string) ([]byte, error) {
	key, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: must be hex encoded: %w", name, err)
	}

	if len(key) < minKeyBytes {
		return nil, fmt.Errorf("%s: decodes to %d bytes, at least %d are required", name, len(key), minKeyBytes)
	}

	return key, nil
}
