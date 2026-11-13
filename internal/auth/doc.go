// Package auth is the business logic behind solobill's two login paths: an
// OIDC login against Keycloak through govue's oidc package, and a local
// account path (password plus TOTP) behind the same Identity shape, per
// docs/concepts/solobill.md's "Auth through govue's OIDC package, with a
// local account path behind the same interface, so a later SaaS
// self-signup does not need a second auth model."
//
// Nothing here is an HTTP handler, except the two govue already hands back
// as http.Handler values (Handlers.Login, Handlers.Callback): routing,
// middleware and the local-login and invite-accept endpoints are
// internal/httpapi's job, a later step. This package is called by that
// step; it does not call out to it.
//
// # The unified identity model
//
// [Identity] is what both paths converge on: enough to know which tenant
// and role a login is for, and enough to build a session.Session via
// [Identity.Session]. Its Subject is always a local accounts.id, the same
// field whatever the login path, which is what makes
// session.Manager.EndAll(subject) revoke every session of an account
// regardless of how it signed in; see [Identity] for the full reasoning.
//
// # OIDC on a first-time subject: the judgment call
//
// docs/concepts/solobill.md does not say what happens the first time a
// given OIDC subject appears, because a sole trader's single tenant was
// never spelled out for the multi-login-path case. The call this package
// makes: an OIDC login requires the same admission gate a local login
// does, an existing accounts row, looked up by (tenant, oidc_subject) once
// the tenant is resolved from the login request (slug or subdomain, a
// step 5 concern), with Status == "active". Matching is on the identity
// provider's stable subject claim, never on email or username, both of
// which can be reassigned to somebody else; see [ResolveOIDCIdentity].
// There is no implicit account creation on first OIDC login.
//
// The alternative, creating an accounts row the first time any subject
// the identity provider vouches for shows up, was rejected: it would let
// anyone the Keycloak realm trusts into a tenant's books with no admission
// step at all, which is exactly the gate the invite flow exists to
// enforce for the local path. Requiring a pre-existing row keeps one
// authorization model for both paths: whether you can see a tenant's data
// is decided by an accounts row existing for you in it, never by which
// login path you used to prove who you are.
//
// What differs between the two paths is only how that row comes to exist.
// A local member is admitted through [InviteManager.AcceptInvite]: the
// invitee redeems a single-use token and sets a password and a TOTP
// secret in the same step, see [InviteManager] for why both at once. An
// OIDC member has no password or TOTP of their own to set, Keycloak
// already verified them, so they are admitted through
// [InviteManager.AcceptInviteOIDC] instead: the same single-use token an
// owner already created and delivered out of band, redeemed the moment
// the identity provider vouches for a subject, which is bound to the new
// account as OIDCSubject in the same transaction that marks the invite
// used. The email the account is created under is always the invite's
// own, never anything the identity provider's token claims: that claim
// is a hint an administrator already acted on when they created the
// invite for that address, not an authorization check performed again
// here. [NewOIDCHandlers] wires the OIDC callback to take this path when
// the request carries a pending-invite cookie (see
// [StartOIDCInviteAccept]) and [ResolveOIDCIdentity]'s ordinary lookup
// otherwise.
//
// The one case neither an invite nor a login can start from is the very
// first account in a brand new tenant: there is no owner yet to create an
// invite. cmd/solobill's own startup seeding covers that one case
// directly, from configuration the person deploying the instance
// controls, the same way it already seeds the first tenant; see its
// seedOwnerAccount.
package auth
