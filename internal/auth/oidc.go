package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/oidc"
	"github.com/ostendorf-cloud/govue/session"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// KeycloakGroupsClaim is the claim govue's oidc.Config.GroupsClaim is
// fixed to for every solobill OIDC provider. Keycloak nests realm role
// membership under "realm_access.roles" rather than under a flat
// "groups" claim; govue's dotted-path GroupsClaim support exists to read
// exactly that shape, per its own doc comment citing this as the
// motivating case.
const KeycloakGroupsClaim = "realm_access.roles"

// OIDCProviderConfig is solobill's view of govue's oidc.Config: the
// fields an operator supplies for one Keycloak client registration.
// GroupsClaim is deliberately not a field here: NewOIDCProvider always
// sets it to KeycloakGroupsClaim, so a caller cannot configure it to
// something that silently stops reading Keycloak's actual roles.
type OIDCProviderConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string

	// Scopes defaults to oidc.DefaultScopes (openid, profile, email) when
	// left empty. A deployment that wants group membership in the token
	// has to ask Keycloak for whichever scope maps realm roles into the
	// token for its client (commonly "roles") in addition to those.
	Scopes []string

	// AuthParams are extra authorization request parameters, passed
	// through to oidc.Config.AuthParams unchanged.
	AuthParams url.Values

	// HTTPClient and Now are test hooks, passed through to oidc.Config;
	// both are nil, meaning the real client and the real clock, in
	// production.
	HTTPClient *http.Client
	Now        func() time.Time
}

// NewOIDCProvider builds a govue oidc.Provider for solobill's Keycloak
// realm shape.
func NewOIDCProvider(cfg OIDCProviderConfig) (*oidc.Provider, error) {
	return oidc.New(oidc.Config{
		Issuer:       cfg.Issuer,
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Scopes:       cfg.Scopes,
		GroupsClaim:  KeycloakGroupsClaim,
		AuthParams:   cfg.AuthParams,
		HTTPClient:   cfg.HTTPClient,
		Now:          cfg.Now,
	})
}

// ErrOIDCAccountNotProvisioned is returned by ResolveOIDCIdentity when no
// active accounts row exists for the verified identity's email within
// the resolved tenant. There is deliberately no fallback that creates
// one: see the package doc's "OIDC on a first-time subject" section for
// why an OIDC login is held to the same admission gate a local login is.
var ErrOIDCAccountNotProvisioned = errors.New("auth: no active account is provisioned for this identity in this tenant")

// ResolveOIDCIdentity turns a govue oidc.Identity, already verified by
// Provider.Exchange, into an Identity: it looks up the accounts row for
// (tenantID, lower(identity.Email)) and requires Status == "active".
//
// tenantID is resolved by the caller before this runs (a slug or
// subdomain on the login request), exactly like LocalAuthenticator.Login
// resolves a tenant before it looks up an account; neither path ever
// looks an account up without a tenant already fixed.
func ResolveOIDCIdentity(ctx context.Context, store Store, tenantID uuid.UUID, identity oidc.Identity) (Identity, error) {
	var account storage.Account

	err := store.WithTenant(ctx, tenantID, func(ctx context.Context) error {
		found, err := store.Accounts().FindByEmail(ctx, identity.Email)
		if err != nil {
			return err
		}

		account = found

		return nil
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return Identity{}, ErrOIDCAccountNotProvisioned
		}

		return Identity{}, fmt.Errorf("auth: resolve oidc identity: %w", err)
	}

	if account.Status != storage.AccountStatusActive {
		return Identity{}, ErrOIDCAccountNotProvisioned
	}

	return IdentityFromAccount(account, identity.Name, identity.Groups), nil
}

// TenantResolver resolves which tenant an incoming login request is for,
// from whatever the request carries: a subdomain, a path segment, a
// query parameter. internal/httpapi, a later step, supplies the real
// implementation; this package only needs the shape, so that
// NewOIDCHandlers can call it before ResolveOIDCIdentity runs.
type TenantResolver func(r *http.Request) (uuid.UUID, error)

// OIDCHandlerOptions is the subset of govue's oidc.HandlerConfig an
// application customizes beyond Provider, Logins and StartSession, which
// NewOIDCHandlers always wires itself.
type OIDCHandlerOptions struct {
	Success   string
	NextParam string
	NoNext    bool
	OnError   func(w http.ResponseWriter, r *http.Request, err error)
}

// NewOIDCHandlers wires govue's oidc.NewHandlers (the documented,
// less-code path; see package doc) so that a successful callback
// resolves the verified oidc.Identity to an Identity via
// ResolveOIDCIdentity and starts a session with sessions.Start(ctx, w,
// identity.Session()), the exact call a successful local login also ends
// at: see Identity.Session.
//
// resolveTenant runs first, inside StartSession, before anything is
// looked up: an OIDC callback carries no tenant of its own, the login
// request that started it does.
func NewOIDCHandlers(
	provider *oidc.Provider,
	logins oidc.LoginStore,
	sessions *session.Manager,
	store Store,
	resolveTenant TenantResolver,
	opts OIDCHandlerOptions,
) (*oidc.Handlers, error) {
	return oidc.NewHandlers(oidc.HandlerConfig{
		Provider: provider,
		Logins:   logins,
		StartSession: func(ctx context.Context, w http.ResponseWriter, r *http.Request, oidcIdentity oidc.Identity) error {
			tenantID, err := resolveTenant(r)
			if err != nil {
				return fmt.Errorf("auth: resolve tenant for oidc login: %w", err)
			}

			identity, err := ResolveOIDCIdentity(ctx, store, tenantID, oidcIdentity)
			if err != nil {
				return err
			}

			if _, err := sessions.Start(ctx, w, identity.Session()); err != nil {
				return fmt.Errorf("auth: start session for oidc login: %w", err)
			}

			return nil
		},
		Success:   opts.Success,
		NextParam: opts.NextParam,
		NoNext:    opts.NoNext,
		OnError:   opts.OnError,
	})
}
