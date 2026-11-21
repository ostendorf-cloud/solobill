package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	govueaccount "github.com/ostendorf-cloud/govue/account"
	"github.com/ostendorf-cloud/govue/api"
	"github.com/ostendorf-cloud/govue/csrf"

	"github.com/ostendorf-cloud/solobill/internal/auth"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// sessionTenantIDKey and sessionRoleKey are the session.Session.Data keys
// internal/auth's Identity.Session writes a logged in account's tenant and
// role under (tenantIDDataKey and roleDataKey there, unexported since
// nothing outside that package is meant to construct a Session by hand).
// Reading them back here, by the same literal string, is this package's
// side of that one contract: see internal/auth/identity.go.
const (
	sessionTenantIDKey = "tenant_id"
	sessionRoleKey     = "role"
)

// The password login, the logout and the password change are govue's
// account handlers (internal/auth.Login.Accounts), mounted in
// routes_auth.go. Only what govue does not provide lives here: the "who am
// I" answer with the CSRF token, and the invite routes.

// meResponse is what GET /api/v1/me answers for an authenticated caller.
// CSRFToken is this session's own CSRF token: the one thing a browser
// cannot read off its HttpOnly session cookie, and so the one thing govue's
// Vue client needs handed to it some other way; see the package doc.
type meResponse struct {
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	TenantID  string `json:"tenant_id"`
	CSRFToken string `json:"csrf_token"`
}

// handleMe answers the current session, if any, plus its CSRF token: see
// the package doc for why this endpoint exists at all, govue ships no
// equivalent. A store failure is 503, not 401: session.Manager.Read's own
// contract separates "no session" from "could not tell", and collapsing
// the second into the first would report an outage as everyone being
// logged out, with nothing in the log to see it by.
//
// The message is marked Exposed: it is a static string with no cause
// attached, written on purpose to tell a client "retry, this is not you
// being logged out" instead of the generic message api.WriteError would
// otherwise collapse every 503 to.
func (a *app) handleMe(w http.ResponseWriter, r *http.Request) error {
	s, ok, err := a.sessions.Read(r.Context(), r)
	if err != nil {
		return api.Unavailable("session store is unavailable").Exposed()
	}
	if !ok {
		return api.Unauthenticated("log in first")
	}

	return api.Write(w, http.StatusOK, meResponse{
		Email:     s.Email,
		Name:      s.Name,
		Role:      s.Data[sessionRoleKey],
		TenantID:  s.Data[sessionTenantIDKey],
		CSRFToken: csrf.Token(a.csrfKey, s.ID),
	})
}

// requireOwnerSession reads the caller's session and requires it to carry
// the owner role, for handleCreateInvite: only an owner may invite another
// account into their tenant.
func (a *app) requireOwnerSession(r *http.Request) (uuidPair, error) {
	s, ok, err := a.sessions.Read(r.Context(), r)
	if err != nil {
		// Exposed for the same reason as handleMe's identical message: a
		// static, cause-free string written on purpose for the caller.
		return uuidPair{}, api.Unavailable("session store is unavailable").Exposed()
	}
	if !ok {
		return uuidPair{}, api.Unauthenticated("log in first")
	}
	if s.Data[sessionRoleKey] != string(storage.AccountRoleOwner) {
		return uuidPair{}, api.PermissionDenied("owner role required")
	}

	tenantID, err := uuid.Parse(s.Data[sessionTenantIDKey])
	if err != nil {
		return uuidPair{}, api.Internal(fmt.Errorf("httpapi: parse session tenant id: %w", err))
	}

	accountID, err := uuid.Parse(s.Subject)
	if err != nil {
		return uuidPair{}, api.Internal(fmt.Errorf("httpapi: parse session subject: %w", err))
	}

	return uuidPair{tenantID: tenantID, accountID: accountID}, nil
}

// uuidPair is requireOwnerSession's return shape: the caller's tenant and
// their own account id, both already parsed.
type uuidPair struct {
	tenantID  uuid.UUID
	accountID uuid.UUID
}

// createInviteRequest is handleCreateInvite's body. There is no tenant
// slug field here: the caller is already authenticated into one tenant,
// read off their own session, and an invite they create can only ever be
// for that tenant.
type createInviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
	// TTL is a Go duration string such as "168h". Empty uses
	// defaultInviteTTL.
	TTL string `json:"ttl"`
}

// createInviteResponse carries the raw invite token exactly once: this
// step does not send an email, that is a later integration, so the token
// is returned in the response body as a foundation-only placeholder for
// it. The caller (today, whoever is driving this endpoint by hand or
// through a later admin UI) is responsible for delivering it.
type createInviteResponse struct {
	Token     string    `json:"token"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
}

// handleCreateInvite creates an invite for the caller's own tenant.
// Owner-only: see requireOwnerSession.
func (a *app) handleCreateInvite(w http.ResponseWriter, r *http.Request) error {
	caller, err := a.requireOwnerSession(r)
	if err != nil {
		return err
	}

	var body createInviteRequest
	if err := api.Decode(r, &body); err != nil {
		return err
	}
	if body.Email == "" {
		return api.Invalid("email must not be empty")
	}

	role := storage.AccountRole(body.Role)
	if role != storage.AccountRoleOwner && role != storage.AccountRoleMember {
		return api.Invalid("role must be owner or member")
	}

	ttl := a.defaultInviteTTL
	if body.TTL != "" {
		parsed, err := time.ParseDuration(body.TTL)
		if err != nil {
			return api.Invalid("ttl must be a duration such as 168h")
		}
		ttl = parsed
	}

	createdBy := caller.accountID
	rawToken, invite, err := a.invites.CreateInvite(r.Context(), auth.CreateInviteParams{
		TenantID:  caller.tenantID,
		Email:     body.Email,
		Role:      role,
		CreatedBy: &createdBy,
		TTL:       ttl,
	})
	if err != nil {
		return api.Internal(err)
	}

	return api.Write(w, http.StatusCreated, createInviteResponse{
		Token:     rawToken,
		Email:     invite.Email,
		Role:      string(invite.Role),
		ExpiresAt: invite.ExpiresAt,
	})
}

// inviteTOTPSecretRequest is handleInviteTOTPSecret's body: the same two
// fields acceptInviteRequest already carries, resolved the same way.
type inviteTOTPSecretRequest struct {
	TenantSlug string `json:"tenant_slug"`
	Token      string `json:"token"`
}

// inviteTOTPSecretResponse is what POST /api/v1/invites/totp-secret answers.
// Email is the invite's address, the login the account will have: the
// SPA hands it to govue's TOTP enrolment component, which shows it to the
// password manager and sends it back on confirm.
type inviteTOTPSecretResponse struct {
	Secret          string `json:"secret"`
	ProvisioningURI string `json:"provisioning_uri"`
	Email           string `json:"email"`
}

// The invite cookie carries the tenant slug and the raw invite token from
// the TOTP secret request to POST /api/v1/invites/confirm, whose body is
// govue's account.CredentialsRequest and so has no field for either. It
// holds nothing the browser did not already have: the invite link carried
// both values in its query string.
//
// It follows the session cookie: __Host- prefixed, Secure, Path=/ and no
// Domain, so only this exact origin over HTTPS can set or read it, plus
// HttpOnly and SameSite=Strict. Plain HTTP development
// (SESSION_ALLOW_PLAIN_HTTP) drops Secure and with it the prefix, which a
// browser refuses without Secure, the same way govue's session does.
const (
	inviteCookieSecureName = "__Host-solobill_invite"
	inviteCookiePlainName  = "solobill_invite"
	inviteCookieMaxAge     = 60 * 60
)

func (a *app) inviteCookieName() string {
	if a.allowPlainHTTP {
		return inviteCookiePlainName
	}

	return inviteCookieSecureName
}

func (a *app) inviteCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     a.inviteCookieName(),
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   !a.allowPlainHTTP,
		SameSite: http.SameSiteStrictMode,
	}
}

// startInviteSession starts the session of a freshly accepted account,
// after ending the session the request already carries, as a login does
// (auth.Login.StartSession): a browser never holds two.
func (a *app) startInviteSession(w http.ResponseWriter, r *http.Request, accepted storage.Account) error {
	if c, err := r.Cookie(a.sessions.CookieName()); err == nil && c.Value != "" {
		if err := a.sessions.End(r.Context(), w, r); err != nil {
			return api.Internal(fmt.Errorf("httpapi: end the previous session: %w", err))
		}
	}

	if _, err := a.sessions.Start(r.Context(), w, auth.IdentityFromAccount(accepted, "", nil).Session()); err != nil {
		return api.Internal(fmt.Errorf("httpapi: start session: %w", err))
	}

	return nil
}

// handleInviteTOTPSecret generates a fresh TOTP secret for the invite the
// request names, persists it on that invite's own row
// (auth.InviteManager.IssueTOTPSecret), and returns it along with its
// provisioning URI, for an invitee enrolling a second factor before
// accepting their invite.
//
// This call now has a real side effect, unlike the version it replaces:
// it persists a secret rather than only generating one, which is why it
// is a POST, not a GET, and why it needs the invite's own tenant and
// token to know which invite to attach the secret to; handleAcceptInvite
// later verifies totp_code against that same, server-held secret, never
// against anything the client supplies. An invitee who never finishes
// enrolling has created no account and leaked nothing by having
// requested a secret; calling this again before accepting simply
// reissues and overwrites it, which is safe if the invitee reloads the
// enrolment page.
func (a *app) handleInviteTOTPSecret(w http.ResponseWriter, r *http.Request) error {
	var body inviteTOTPSecretRequest
	if err := api.Decode(r, &body); err != nil {
		return err
	}
	if body.TenantSlug == "" {
		return api.Invalid("tenant_slug must not be empty")
	}

	tenant, err := a.store.Tenants().FindBySlug(r.Context(), body.TenantSlug)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return api.NotFound("unknown tenant")
		}

		return api.Internal(err)
	}

	enrolment, err := a.invites.IssueTOTPEnrolment(r.Context(), tenant.ID, body.Token)
	if err != nil {
		if errors.Is(err, auth.ErrInviteInvalid) {
			return api.Invalid("invite token is invalid, expired or already used")
		}

		return api.Internal(err)
	}

	http.SetCookie(w, a.inviteCookie(url.Values{"tenant": {tenant.Slug}, "token": {body.Token}}.Encode(), inviteCookieMaxAge))

	return api.Write(w, http.StatusOK, inviteTOTPSecretResponse{
		Secret:          enrolment.Secret,
		ProvisioningURI: enrolment.ProvisioningURI,
		Email:           enrolment.Email,
	})
}

// handleInviteConfirm is handleAcceptInvite in the shape of govue's TOTP
// confirm: the body is account.CredentialsRequest ({login, password,
// code}), the tenant and the token come from the invite cookie the TOTP
// secret request set, and the answer is account.LoginResult. govue's
// GovueTotpEnrolment component posts exactly that. Password and code are
// still checked in one step: the account is created only with a confirmed
// second factor, there is no password only state in between.
//
// login has to be the invite's email: the component sends the login it
// showed the password manager, and a different one is refused like an
// invalid invite.
func (a *app) handleInviteConfirm(w http.ResponseWriter, r *http.Request) error {
	var body govueaccount.CredentialsRequest
	if err := api.Decode(r, &body); err != nil {
		return err
	}

	cookie, err := r.Cookie(a.inviteCookieName())
	if err != nil || cookie.Value == "" {
		return api.Invalid("invite token is invalid, expired or already used")
	}

	values, err := url.ParseQuery(cookie.Value)
	if err != nil || values.Get("tenant") == "" || values.Get("token") == "" {
		return api.Invalid("invite token is invalid, expired or already used")
	}

	if err := checkInvitePassword(body.Password); err != nil {
		return err
	}

	tenant, err := a.store.Tenants().FindBySlug(r.Context(), values.Get("tenant"))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return api.NotFound("unknown tenant")
		}

		return api.Internal(err)
	}

	account, err := a.invites.AcceptInvite(r.Context(), auth.AcceptInviteParams{
		TenantID: tenant.ID,
		RawToken: values.Get("token"),
		Login:    &body.Login,
		Password: body.Password,
		TOTPCode: body.Code,
	})
	if err != nil {
		return acceptInviteError(err)
	}

	// The invite is used: the cookie has nothing left to carry.
	http.SetCookie(w, a.inviteCookie("", -1))

	if err := a.startInviteSession(w, r, account); err != nil {
		return err
	}

	return api.Write(w, http.StatusOK, govueaccount.LoginResult{Login: account.Email})
}

// checkInvitePassword applies the invite password bounds before any
// hashing.
func checkInvitePassword(password string) error {
	if len(password) < minInvitePasswordLength {
		return api.Invalid(fmt.Sprintf("password must be at least %d characters", minInvitePasswordLength))
	}
	if len(password) > maxInvitePasswordLength {
		return api.Invalid(fmt.Sprintf("password must be at most %d characters", maxInvitePasswordLength))
	}

	return nil
}

// acceptInviteError maps what InviteManager.AcceptInvite answers.
func acceptInviteError(err error) error {
	switch {
	case errors.Is(err, auth.ErrInviteInvalid):
		return api.Invalid("invite token is invalid, expired or already used")
	case errors.Is(err, auth.ErrTOTPEnrollmentNotConfirmed):
		return api.Invalid("totp code does not match the secret just enrolled")
	case errors.Is(err, govueaccount.ErrPasswordTooShort), errors.Is(err, govueaccount.ErrPasswordTooLong):
		// govue's policy, checked again by the hasher. It counts
		// characters where the bounds above count bytes, so a short
		// password of multibyte characters lands here.
		return api.Invalid(fmt.Sprintf("password must be at least %d characters", govueaccount.MinPasswordLength))
	case errors.Is(err, govueaccount.ErrHashingBusy):
		busy := api.Unavailable("the server is busy, please try again").Exposed()
		busy.RetryAfter = govueaccount.HashingBusyRetryAfter

		return busy
	default:
		return api.Internal(err)
	}
}

// minInvitePasswordLength and maxInvitePasswordLength bound the password
// an invite-accept request may set. The minimum is govue's own policy
// (account.MinPasswordLength, 12), the one every password of an account
// is held to. The maximum, 128, is generous for a real passphrase and
// bounds the argon2id hashing cost this otherwise unauthenticated route
// can be made to pay per request, far below govue's own 1024 byte limit.
const (
	minInvitePasswordLength = govueaccount.MinPasswordLength
	maxInvitePasswordLength = 128
)

// acceptInviteRequest is handleAcceptInvite's body. TenantSlug is this
// request's own tenant resolution: see
// the package doc. There is no totp_secret field: the secret is
// server-owned, issued by a prior call to handleInviteTOTPSecret and
// stored on the invite itself; TOTPCode is only what the invitee's
// authenticator app now computes from it.
type acceptInviteRequest struct {
	TenantSlug string `json:"tenant_slug"`
	Token      string `json:"token"`
	Password   string `json:"password"`
	TOTPCode   string `json:"totp_code"`
}

// handleAcceptInvite redeems an invite token into a new, active local
// account, and starts a session for it: the same outcome a successful
// local login reaches, so an invitee who just finished signing up is not
// asked to log in again immediately afterward.
//
// The tenant slug is resolved first, and its own failure (no such tenant)
// is answered distinctly from an invalid token: a tenant slug is not part
// of the invite's own secret, see loginRequest's doc, so there is nothing
// to protect by collapsing the two.
func (a *app) handleAcceptInvite(w http.ResponseWriter, r *http.Request) error {
	var body acceptInviteRequest
	if err := api.Decode(r, &body); err != nil {
		return err
	}
	if body.TenantSlug == "" {
		return api.Invalid("tenant_slug must not be empty")
	}

	if err := checkInvitePassword(body.Password); err != nil {
		return err
	}

	tenant, err := a.store.Tenants().FindBySlug(r.Context(), body.TenantSlug)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return api.NotFound("unknown tenant")
		}

		return api.Internal(err)
	}

	account, err := a.invites.AcceptInvite(r.Context(), auth.AcceptInviteParams{
		TenantID: tenant.ID,
		RawToken: body.Token,
		Password: body.Password,
		TOTPCode: body.TOTPCode,
	})
	if err != nil {
		return acceptInviteError(err)
	}

	if err := a.startInviteSession(w, r, account); err != nil {
		return err
	}

	api.NoContent(w)

	return nil
}

// inviteOIDCStartRequest is handleInviteOIDCStart's body: the same two
// fields inviteTOTPSecretRequest and acceptInviteRequest already carry,
// resolved the same way.
type inviteOIDCStartRequest struct {
	TenantSlug string `json:"tenant_slug"`
	Token      string `json:"token"`
}

// handleInviteOIDCStart checks that the invite the request names is
// worth sending a browser to the identity provider for
// (auth.InviteManager.PeekInvite, advisory only) and, if so, signs it
// into the pending-invite cookie (auth.StartOIDCInviteAccept). The real
// redemption happens later, atomically, inside the OIDC callback
// (auth.NewOIDCHandlers's StartSession), once the provider has actually
// vouched for a subject; nothing here marks the invite used.
//
// A successful response does not itself redirect anywhere: the caller
// (the invite-accept page) follows it with a browser navigation to GET
// /auth/oidc/login, which an XHR/fetch response cannot trigger on its
// own.
func (a *app) handleInviteOIDCStart(w http.ResponseWriter, r *http.Request) error {
	if a.loginCodec == nil {
		return api.NotFound("oidc is not configured")
	}

	var body inviteOIDCStartRequest
	if err := api.Decode(r, &body); err != nil {
		return err
	}
	if body.TenantSlug == "" {
		return api.Invalid("tenant_slug must not be empty")
	}

	tenant, err := a.store.Tenants().FindBySlug(r.Context(), body.TenantSlug)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return api.NotFound("unknown tenant")
		}

		return api.Internal(err)
	}

	if err := a.invites.PeekInvite(r.Context(), tenant.ID, body.Token); err != nil {
		if errors.Is(err, auth.ErrInviteInvalid) {
			return api.Invalid("invite token is invalid, expired or already used")
		}

		return api.Internal(err)
	}

	if err := auth.StartOIDCInviteAccept(w, a.loginCodec, a.allowPlainHTTP, tenant.ID, body.Token); err != nil {
		return api.Internal(err)
	}

	api.NoContent(w)

	return nil
}
