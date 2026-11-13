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

// inviteOIDCCookieName carries a pending OIDC invite acceptance from
// internal/httpapi's invite-accept-via-OIDC start endpoint through to the
// OIDC callback's own StartSession closure below: see
// StartOIDCInviteAccept. It is a separate, signed, HttpOnly cookie, not
// govue's own login-in-flight cookie (named by *session.Codec's
// LoginCookieName): an invite acceptance and an ordinary login carry
// different state and must not be confused by sharing one cookie.
const inviteOIDCCookieName = "solobill_invite_oidc"

// inviteOIDCLifetime bounds how long an invite acceptance has to come
// back from the identity provider, the same generosity
// session.DefaultLoginLifetime already gives an ordinary login: long
// enough to read a consent screen and log in, short enough that a signed
// value nothing can revoke is not worth holding onto.
const inviteOIDCLifetime = session.DefaultLoginLifetime

// inviteOIDCState is what StartOIDCInviteAccept signs into
// inviteOIDCCookieName. Token is the raw invite token, not its hash or
// the invite's own row id: AcceptInviteOIDC's admission proof is
// possession of that token, exactly like AcceptInvite's is, and an
// invite's id is not a secret a stranger could not otherwise learn.
type inviteOIDCState struct {
	TenantID uuid.UUID `json:"tid"`
	Token    string    `json:"tok"`
}

// StartOIDCInviteAccept signs tenantID and rawToken into the pending-
// invite cookie and sets it on w. internal/httpapi's invite-accept-via-
// OIDC start endpoint calls this once it has confirmed (via
// InviteManager.PeekInvite) that the token is worth sending the browser
// to the identity provider for, immediately before redirecting it to
// govue's own Handlers.Login() (built by NewOIDCHandlers below).
//
// codec is the same *session.Codec a deployment already built for
// govue's own login-in-flight cookie (internal/config's
// SESSION_CODEC_KEY): reusing it costs no new secret and no new
// configuration field, since Encode/Decode are a plain signing primitive
// with no cookie of their own attached.
func StartOIDCInviteAccept(w http.ResponseWriter, codec *session.Codec, allowPlainHTTP bool, tenantID uuid.UUID, rawToken string) error {
	encoded, err := codec.Encode(inviteOIDCState{TenantID: tenantID, Token: rawToken}, inviteOIDCLifetime)
	if err != nil {
		return fmt.Errorf("auth: encode oidc invite state: %w", err)
	}

	http.SetCookie(w, inviteOIDCCookie(encoded, allowPlainHTTP))

	return nil
}

// inviteOIDCCookie is the pending-invite cookie with whatever value is
// given. HttpOnly and SameSite=Lax match govue's own login cookie, for
// the same reasons: a script never needs to read it, and the identity
// provider's redirect back to the callback is a cross-site top-level
// navigation that SameSite=Strict would withhold it from.
func inviteOIDCCookie(value string, allowPlainHTTP bool) *http.Cookie {
	return &http.Cookie{
		Name:     inviteOIDCCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   !allowPlainHTTP,
		SameSite: http.SameSiteLaxMode,
	}
}

// clearInviteOIDCCookie deletes the pending-invite cookie. The OIDC
// callback below calls this unconditionally, its outcome included,
// mirroring *session.Codec.ReadLoginCookie's own "always clear" doc
// comment: a pending invite is good for exactly one callback.
func clearInviteOIDCCookie(w http.ResponseWriter, allowPlainHTTP bool) {
	cookie := inviteOIDCCookie("", allowPlainHTTP)
	cookie.MaxAge = -1
	http.SetCookie(w, cookie)
}

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
// active accounts row exists for the verified identity's subject within
// the resolved tenant. There is deliberately no fallback that creates
// one here: see the package doc's "OIDC on a first-time subject" section
// for why an OIDC login is held to the same admission gate a local login
// is, and for the two paths (AcceptInviteOIDC, cmd/solobill's startup
// seeding) that do provision one.
var ErrOIDCAccountNotProvisioned = errors.New("auth: no active account is provisioned for this identity in this tenant")

// ResolveOIDCIdentity turns a govue oidc.Identity, already verified by
// Provider.Exchange, into an Identity: it looks up the accounts row for
// (tenantID, oidc_subject) and requires Status == "active".
//
// Matching is on identity.Subject, the identity provider's stable id for
// the account, never on identity.Email or identity.Username: govue's own
// oidc.Identity doc comment is explicit that both of those can be
// reassigned to somebody else, so a lookup keyed on either would bind a
// session to whoever currently holds an email address rather than to the
// account the provider actually authenticated. Email is still read back
// off the resolved accounts row by IdentityFromAccount, as a display
// attribute only, never as a key.
//
// Provisioning oidc_subject onto an account is never this function's
// job: AcceptInviteOIDC does it for an invited member, and
// cmd/solobill's startup seeding does it for the very first owner. A
// subject with no account yet, or with one that is pending or disabled,
// correctly answers ErrOIDCAccountNotProvisioned regardless of which of
// those two paths a real deployment uses, which is strictly safer than
// the email-based matching this function replaced.
//
// tenantID is resolved by the caller before this runs (a slug or
// subdomain on the login request), exactly like LocalAuthenticator.Login
// resolves a tenant before it looks up an account; neither path ever
// looks an account up without a tenant already fixed.
func ResolveOIDCIdentity(ctx context.Context, store Store, tenantID uuid.UUID, identity oidc.Identity) (Identity, error) {
	var account storage.Account

	err := store.WithTenant(ctx, tenantID, func(ctx context.Context) error {
		found, err := store.Accounts().FindByOIDCSubject(ctx, identity.Subject)
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

	// AllowPlainHTTP mirrors config.Config.SessionAllowPlainHTTP: it only
	// affects the pending-invite cookie's Secure attribute (see
	// inviteOIDCCookie); govue's own login cookie already gets this from
	// whatever built the *session.Codec passed as logins.
	AllowPlainHTTP bool
}

// NewOIDCHandlers wires govue's oidc.NewHandlers (the documented,
// less-code path; see package doc) so that a successful callback resolves
// the verified oidc.Identity to an Identity and starts a session with
// sessions.Start(ctx, w, identity.Session()), the exact call a successful
// local login also ends at: see Identity.Session.
//
// Resolving that Identity takes one of two paths, decided by whether the
// callback request carries a pending-invite cookie (see
// StartOIDCInviteAccept, set by internal/httpapi's invite-accept-via-OIDC
// start endpoint immediately before it sends the browser here):
//
//   - present and valid: invites.AcceptInviteOIDC binds oidcIdentity.
//     Subject to the invite's account, see that method's own doc for why
//     this is the one place an OIDC subject is ever provisioned.
//   - absent (the ordinary login case): ResolveOIDCIdentity looks the
//     subject up against an already-provisioned account, exactly as
//     before this cookie existed.
//
// resolveTenant runs first either way, before anything is looked up: an
// OIDC callback carries no tenant of its own, the login request that
// started it does.
//
// logins is *session.Codec rather than the narrower oidc.LoginStore
// govue's own HandlerConfig.Logins field declares: it still satisfies
// that field with no adapter, and this function additionally needs its
// Decode method to read the pending-invite cookie, which oidc.LoginStore
// does not expose.
func NewOIDCHandlers(
	provider *oidc.Provider,
	logins *session.Codec,
	sessions *session.Manager,
	store Store,
	invites *InviteManager,
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

			identity, err := startOIDCSession(ctx, store, invites, logins, w, r, opts.AllowPlainHTTP, tenantID, oidcIdentity)
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

// startOIDCSession resolves oidcIdentity to an Identity, either by
// redeeming a pending invite accept (the pending-invite cookie present
// and valid) or by looking up an already-provisioned account
// (ResolveOIDCIdentity): see NewOIDCHandlers's own doc for which runs
// when.
//
// The pending-invite cookie is always cleared before this returns,
// success or failure, the same single-use guarantee
// *session.Codec.ReadLoginCookie gives govue's own login cookie: an
// invite acceptance is good for exactly one callback.
func startOIDCSession(
	ctx context.Context,
	store Store,
	invites *InviteManager,
	logins *session.Codec,
	w http.ResponseWriter,
	r *http.Request,
	allowPlainHTTP bool,
	tenantID uuid.UUID,
	oidcIdentity oidc.Identity,
) (Identity, error) {
	state, hasInvite := readInviteOIDCState(logins, r)
	clearInviteOIDCCookie(w, allowPlainHTTP)

	if !hasInvite {
		return ResolveOIDCIdentity(ctx, store, tenantID, oidcIdentity)
	}

	if state.TenantID != tenantID {
		// The cookie was signed for a different tenant than this login
		// resolved to: refuse rather than guess which one the caller
		// meant, the same opaque refusal an unknown token already gets.
		return Identity{}, ErrInviteInvalid
	}

	account, err := invites.AcceptInviteOIDC(ctx, AcceptInviteOIDCParams{
		TenantID: tenantID,
		RawToken: state.Token,
		Subject:  oidcIdentity.Subject,
	})
	if err != nil {
		return Identity{}, err
	}

	return IdentityFromAccount(account, oidcIdentity.Name, oidcIdentity.Groups), nil
}

// readInviteOIDCState reads and decodes the pending-invite cookie, and
// reports whether one was present and valid. Any failure (no cookie, a
// signature that does not verify, an expired value) is treated the same
// as no cookie at all: this is cookie, never a token the client controls
// the shape of, so there is nothing to tell apart for a caller.
func readInviteOIDCState(logins *session.Codec, r *http.Request) (inviteOIDCState, bool) {
	cookie, err := r.Cookie(inviteOIDCCookieName)
	if err != nil || cookie.Value == "" {
		return inviteOIDCState{}, false
	}

	var state inviteOIDCState
	if err := logins.Decode(cookie.Value, &state); err != nil {
		return inviteOIDCState{}, false
	}

	return state, true
}
