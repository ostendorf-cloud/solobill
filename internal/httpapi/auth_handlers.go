package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
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

// loginRequest is handleLogin's body. TenantSlug is this request's own
// tenant resolution, not read back out of anything else: see the package
// doc's "Tenant resolution" section.
type loginRequest struct {
	TenantSlug string `json:"tenant_slug"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	TOTPCode   string `json:"totp_code"`
}

// handleLogin runs the local login path and starts a session on success.
// Every failure, whatever the actual cause inside internal/auth, is
// answered as the one 401 LocalAuthenticator.Login already collapses every
// failure into; see its own doc comment for why.
func (a *app) handleLogin(w http.ResponseWriter, r *http.Request) error {
	var body loginRequest
	if err := api.Decode(r, &body); err != nil {
		return err
	}

	identity, err := a.local.Login(r.Context(), body.TenantSlug, body.Email, body.Password, body.TOTPCode)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			return api.Unauthenticated("invalid tenant, email, password or totp code")
		}

		return api.Internal(err)
	}

	if _, err := a.sessions.Start(r.Context(), w, identity.Session()); err != nil {
		return api.Internal(fmt.Errorf("httpapi: start session: %w", err))
	}

	api.NoContent(w)

	return nil
}

// handleLogout ends whatever session the request carries. Ending a request
// with no session is not an error, matching session.Manager.End's own
// contract: a logout always leaves the client with nothing either way.
func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) error {
	if err := a.sessions.End(r.Context(), w, r); err != nil {
		return api.Internal(fmt.Errorf("httpapi: end session: %w", err))
	}

	api.NoContent(w)

	return nil
}

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
func (a *app) handleMe(w http.ResponseWriter, r *http.Request) error {
	s, ok, err := a.sessions.Read(r.Context(), r)
	if err != nil {
		return api.Unavailable("session store is unavailable")
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
		return uuidPair{}, api.Unavailable("session store is unavailable")
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

// inviteTOTPSecretResponse is what GET /api/v1/invites/totp-secret answers.
type inviteTOTPSecretResponse struct {
	Secret          string `json:"secret"`
	ProvisioningURI string `json:"provisioning_uri"`
}

// inviteTOTPIssuer names this application in the QR code an authenticator
// app shows next to the enrolled entry.
const inviteTOTPIssuer = "solobill"

// handleInviteTOTPSecret generates a fresh TOTP secret and its provisioning
// URI, for an invitee enrolling a second factor before accepting their
// invite.
//
// This is the simpler of the two flows the issue's plan leaves open (see
// its own text on the invite-accept endpoint): nothing is persisted here,
// so this endpoint needs no authentication and no tenant at all. The
// secret this call hands back is only real once the invitee's authenticator
// app and this same value both reach handleAcceptInvite together: that call
// is what confirms totp_code against totp_secret and, only on a match,
// creates the account. An invitee who never finishes enrolling has created
// nothing and leaked nothing by having requested a secret.
func (a *app) handleInviteTOTPSecret(w http.ResponseWriter, r *http.Request) error {
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		return api.Internal(err)
	}

	accountName := r.URL.Query().Get("email")
	if accountName == "" {
		accountName = "invitee"
	}

	return api.Write(w, http.StatusOK, inviteTOTPSecretResponse{
		Secret:          secret,
		ProvisioningURI: auth.TOTPProvisioningURI(secret, accountName, inviteTOTPIssuer),
	})
}

// acceptInviteRequest is handleAcceptInvite's body. TenantSlug is this
// request's own tenant resolution, the same call loginRequest makes: see
// the package doc. TOTPSecret is the value a prior call to
// handleInviteTOTPSecret already handed the invitee; TOTPCode is what their
// authenticator app now computes from it.
type acceptInviteRequest struct {
	TenantSlug string `json:"tenant_slug"`
	Token      string `json:"token"`
	Password   string `json:"password"`
	TOTPSecret string `json:"totp_secret"`
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

	tenant, err := a.store.Tenants().FindBySlug(r.Context(), body.TenantSlug)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return api.NotFound("unknown tenant")
		}

		return api.Internal(err)
	}

	account, err := a.invites.AcceptInvite(r.Context(), auth.AcceptInviteParams{
		TenantID:   tenant.ID,
		RawToken:   body.Token,
		Password:   body.Password,
		TOTPSecret: body.TOTPSecret,
		TOTPCode:   body.TOTPCode,
	})
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInviteInvalid):
			return api.Invalid("invite token is invalid, expired or already used")
		case errors.Is(err, auth.ErrTOTPEnrollmentNotConfirmed):
			return api.Invalid("totp code does not match the secret just enrolled")
		default:
			return api.Internal(err)
		}
	}

	identity := auth.IdentityFromAccount(account, "", nil)
	if _, err := a.sessions.Start(r.Context(), w, identity.Session()); err != nil {
		return api.Internal(fmt.Errorf("httpapi: start session: %w", err))
	}

	api.NoContent(w)

	return nil
}
