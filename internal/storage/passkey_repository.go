package storage

import (
	"context"

	"github.com/google/uuid"
)

// PasskeyRepository is the storage side of govue's passkey.Store.
// internal/auth.PasskeyStore adapts it. Every method requires a context
// TenantScope.WithTenant produced and acts within that tenant only.
type PasskeyRepository interface {
	// Add stores cred. It locks the account row first, so two first
	// registrations of one account racing with two new handles are
	// serialised. The account's handle is set from cred.UserHandle when it
	// has none. ErrConflict, with nothing changed, for a taken credential
	// id, a handle that is not the account's own, or a handle another
	// account holds. ErrNotFound for an account that does not exist in the
	// scoped tenant.
	Add(ctx context.Context, cred PasskeyCredential) error

	// List returns every credential of the account, oldest first. An
	// unknown account returns an empty slice.
	List(ctx context.Context, accountID uuid.UUID) ([]PasskeyCredential, error)

	// FindByUserHandle returns the active account the handle belongs to,
	// its email and its credentials, or ErrNotFound. The match is exact on
	// the bytes. A pending or disabled account is not found.
	FindByUserHandle(ctx context.Context, handle []byte) (accountID uuid.UUID, email string, creds []PasskeyCredential, err error)

	// Update writes SignCount, LastUsedAt and Data of cred.ID of
	// cred.AccountID only when the stored sign count equals oldSignCount,
	// and answers ErrNotFound when no row changed.
	Update(ctx context.Context, cred PasskeyCredential, oldSignCount uint32) error

	// Delete deletes credential id of accountID. With keepOne it deletes
	// only while the account holds another credential, decided under the
	// same account row lock Add takes. The handle goes with the last
	// credential. ErrNotFound when nothing was deleted.
	Delete(ctx context.Context, accountID uuid.UUID, id []byte, keepOne bool) error
}
