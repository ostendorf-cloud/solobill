package auth

import (
	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/session"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// tenantIDDataKey and roleDataKey are the session.Session.Data keys
// Identity.Session writes the tenant and role under. session.Session
// itself knows nothing about a tenant or a role, govue is not a
// multi-tenant accounting package, so this is where solobill's own two
// facts ride along inside the session it already carries.
const (
	tenantIDDataKey = "tenant_id"
	roleDataKey     = "role"
)

// Identity is what both the OIDC login path and the local account path
// produce: enough to authorize a request against one tenant and role, and
// enough to start a session via [Identity.Session]. See package doc for
// how each path populates one, in particular the judgment call on a
// first-time OIDC login.
type Identity struct {
	// TenantID is the tenant this login is scoped to, resolved before
	// either path runs (a slug or subdomain on the login request, a
	// step 5 concern): never read back out of anything the browser sent
	// unauthenticated and trusted on its own.
	TenantID uuid.UUID

	// AccountID is the accounts.id row backing this login, for both
	// paths: an OIDC login resolves to one exactly like a local login
	// does, see [ResolveOIDCIdentity]. It is what [Identity.Session] uses
	// as the session's Subject, not the OIDC "sub", so that
	// session.Manager.EndAll keys on the one identifier that is stable
	// and revocable regardless of which path a given login took.
	AccountID uuid.UUID

	Email    string
	Name     string
	Username string
	Role     storage.AccountRole

	// Groups is the identity provider's group membership for an OIDC
	// login (Keycloak's realm_access.roles, see NewOIDCProvider), and
	// always nil for a local login: a local account has no identity
	// provider to carry groups from, only the Role column already on its
	// accounts row.
	Groups []string
}

// IdentityFromAccount builds the part of an Identity that both paths read
// off the same accounts row: AccountID, TenantID, Email, Role. groups is
// the OIDC path's group membership, or nil for a local login; name is the
// display name each path has from its own source (an ID token's "name"
// claim for OIDC, empty for local, which has none).
func IdentityFromAccount(account storage.Account, name string, groups []string) Identity {
	return Identity{
		TenantID:  account.TenantID,
		AccountID: account.ID,
		Email:     account.Email,
		Name:      name,
		Username:  account.Email,
		Role:      account.Role,
		Groups:    groups,
	}
}

// Session builds the session.Session a later step passes to
// session.Manager.Start, the one call both login paths end at (see
// package doc). Subject is AccountID, not an OIDC subject, Username and
// Email come straight off the accounts row, and TenantID and Role ride
// along in Data, the one place govue leaves for an application's own
// facts.
//
// The returned value still needs session.Manager.Start to fill in ID,
// IssuedAt, LastSeen and ExpiresAt: those are the manager's to set, not
// this package's, exactly as session.Session itself documents.
func (id Identity) Session() session.Session {
	return session.Session{
		Subject:  id.AccountID.String(),
		Username: id.Username,
		Name:     id.Name,
		Email:    id.Email,
		Groups:   id.Groups,
		Data: map[string]string{
			tenantIDDataKey: id.TenantID.String(),
			roleDataKey:     string(id.Role),
		},
	}
}
