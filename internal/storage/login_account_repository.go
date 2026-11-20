package storage

import (
	"context"

	"github.com/google/uuid"
)

// LoginAccountRepository is the storage side of govue's account.Store:
// the reads and the conditional writes a password and TOTP login needs on
// accounts. internal/auth.AccountStore adapts it to account.Store.
//
// Every method requires a context TenantScope.WithTenant produced, like
// every other tenant scoped repository, and acts within that tenant only.
// Every write is one conditional statement that compares and writes
// together, and answers ErrNotFound when no row changed: that is what
// refuses a replayed TOTP code and a lost password change under
// concurrency, see govue's account.Store.
type LoginAccountRepository interface {
	// FindActiveByEmail returns the active account with this email,
	// compared case insensitively and exactly (no pattern), or
	// ErrNotFound. A pending or disabled account is not found.
	FindActiveByEmail(ctx context.Context, email string) (LoginAccount, error)

	// CreateMember creates an active member account in the scoped tenant
	// with an explicit TOTP counter of 0, an unconfirmed secret, and
	// returns it. A taken email answers ErrConflict.
	CreateMember(ctx context.Context, email, passwordHash, totpSecret string) (LoginAccount, error)

	// AdvanceTOTPCounter sets the counter to counter only when the stored
	// counter (NULL read as 1) is above zero and below counter.
	AdvanceTOTPCounter(ctx context.Context, id uuid.UUID, counter int64) error

	// ConfirmTOTP sets the counter to counter only when the stored secret
	// is secret, the stored counter is exactly 0 and counter is above 0.
	ConfirmTOTP(ctx context.Context, id uuid.UUID, secret string, counter int64) error

	// ReplaceTOTPSecret replaces the secret only while the stored counter
	// is exactly 0.
	ReplaceTOTPSecret(ctx context.Context, id uuid.UUID, secret string) error

	// SetPasswordHash replaces the hash only when the stored hash is not
	// empty and equals oldHash.
	SetPasswordHash(ctx context.Context, id uuid.UUID, oldHash, newHash string) error
}
