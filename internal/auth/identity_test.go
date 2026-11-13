package auth

import (
	"testing"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// TestIdentitySessionConvergesAcrossBothLoginPaths is the "the identity
// from either path converges on the same session.Session shape" check
// the package doc promises: a local login (no OIDC groups, no display
// name of its own) and an OIDC login (an identity provider's Name and
// Groups) for the very same underlying accounts row build a
// session.Session that agrees on every field neither path has a reason
// to differ on: Subject, Username, Email, and the TenantID/Role carried
// in Data. Only Name and Groups, which legitimately come from different
// places, are allowed to differ.
func TestIdentitySessionConvergesAcrossBothLoginPaths(t *testing.T) {
	t.Parallel()

	account := storage.Account{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Email:    "owner@acme.example",
		Role:     storage.AccountRoleOwner,
		Status:   storage.AccountStatusActive,
	}

	// local.go's Login builds its Identity this way: no display name, no
	// groups, because a local account has no identity provider to carry
	// either from.
	localSession := IdentityFromAccount(account, "", nil).Session()

	// oidc.go's ResolveOIDCIdentity builds its Identity this way: the
	// same account row, plus whatever the identity provider's verified
	// ID token carried as a display name and group membership.
	oidcSession := IdentityFromAccount(account, "Simon Ostendorf", []string{"owner", "default-roles-solobill"}).Session()

	if localSession.Subject != oidcSession.Subject {
		t.Errorf("Subject diverges: local %q, oidc %q", localSession.Subject, oidcSession.Subject)
	}
	if localSession.Subject != account.ID.String() {
		t.Errorf("Subject = %q, want the accounts.id string %q, not an OIDC subject", localSession.Subject, account.ID.String())
	}
	if localSession.Username != oidcSession.Username {
		t.Errorf("Username diverges: local %q, oidc %q", localSession.Username, oidcSession.Username)
	}
	if localSession.Email != oidcSession.Email {
		t.Errorf("Email diverges: local %q, oidc %q", localSession.Email, oidcSession.Email)
	}
	if localSession.Data[tenantIDDataKey] != oidcSession.Data[tenantIDDataKey] {
		t.Errorf("Data[%q] diverges: local %q, oidc %q", tenantIDDataKey, localSession.Data[tenantIDDataKey], oidcSession.Data[tenantIDDataKey])
	}
	if localSession.Data[roleDataKey] != oidcSession.Data[roleDataKey] {
		t.Errorf("Data[%q] diverges: local %q, oidc %q", roleDataKey, localSession.Data[roleDataKey], oidcSession.Data[roleDataKey])
	}

	// Name and Groups are exactly the two fields that are allowed, and
	// expected, to differ: that is what each path actually has to offer
	// and no more.
	if localSession.Name == oidcSession.Name {
		t.Error("Name does not differ between the two fixtures, this assertion needs a real difference to be meaningful")
	}
	if len(localSession.Groups) != 0 {
		t.Errorf("local session Groups = %v, want none: a local account has no identity provider to carry groups from", localSession.Groups)
	}
	if len(oidcSession.Groups) == 0 {
		t.Error("oidc session Groups is empty, want the identity provider's roles")
	}
}
