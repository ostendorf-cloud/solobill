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
// does, an existing accounts row, looked up by (tenant, lower(email)) once
// the tenant is resolved from the login request (slug or subdomain, a
// step 5 concern), with Status == "active". There is no implicit
// account creation on first OIDC login.
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
// already verified them, so their row is provisioned directly by whoever
// is onboarding them (an owner, through a later step's admin action) with
// Status already "active" and PasswordHash and TOTPSecret left nil; see
// [ResolveOIDCIdentity].
package auth
