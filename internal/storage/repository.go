package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// TenantScope is the one mechanism through which AccountRepository and
// InviteRepository methods are allowed to run. WithTenant opens a
// transaction, scopes it to tenantID (set_config('app.tenant_id', ...),
// transaction-local) and runs fn with a context carrying that
// transaction; it commits when fn returns nil and rolls back otherwise.
//
// A tenant-scoped repository method called with a context WithTenant did
// not produce fails with ErrNoTenantScope instead of silently running
// unscoped: row level security would already turn an unscoped query into
// zero rows read and zero rows written, but failing in Go as well means
// that mistake surfaces immediately, as a bug, rather than as a
// confusing empty result.
type TenantScope interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn func(ctx context.Context) error) error
}

// TenantRepository persists and queries tenants themselves. Its methods
// are deliberately not tenant-scoped: resolving a slug to a tenant id is
// exactly the step that has to run before any tenant scope exists.
type TenantRepository interface {
	Create(ctx context.Context, tenant Tenant) (Tenant, error)
	FindByID(ctx context.Context, id uuid.UUID) (Tenant, error)
	FindBySlug(ctx context.Context, slug string) (Tenant, error)
}

// AccountRepository persists and queries local accounts. Every method
// must run inside a context TenantScope.WithTenant produced; see
// ErrNoTenantScope.
type AccountRepository interface {
	Create(ctx context.Context, account Account) (Account, error)
	FindByID(ctx context.Context, id uuid.UUID) (Account, error)
	FindByEmail(ctx context.Context, email string) (Account, error)
	// FindByOIDCSubject looks an account up by the identity provider's
	// stable subject claim, the only lookup internal/auth.ResolveOIDCIdentity
	// performs for an OIDC login: never by email, which can be reassigned
	// to somebody else.
	FindByOIDCSubject(ctx context.Context, subject string) (Account, error)
	List(ctx context.Context) ([]Account, error)
	Update(ctx context.Context, account Account) (Account, error)
}

// InviteRepository persists and queries invites. Every method must run
// inside a context TenantScope.WithTenant produced; see ErrNoTenantScope.
//
// FindByTokenHash is not a cross-tenant lookup, even though a bare token
// carries no tenant by itself: the caller resolves the tenant first (for
// example from a slug in the accept-invite URL, against TenantRepository,
// which carries no tenant scope of its own) and opens its own WithTenant
// scope before calling it, exactly like every other method here.
type InviteRepository interface {
	Create(ctx context.Context, invite Invite) (Invite, error)
	FindByID(ctx context.Context, id uuid.UUID) (Invite, error)
	FindByTokenHash(ctx context.Context, tokenHash string) (Invite, error)

	// MarkUsed marks the invite used at usedAt, atomically guarded against
	// a concurrent second call for the same invite: an implementation must
	// fail the whole call with ErrConflict, rather than silently
	// succeeding, when the invite is already used by the time the mark
	// takes effect, including when another call raced it. internal/auth's
	// invite-accept flow relies on exactly that race having a single
	// winner.
	//
	// A caller that must make this and the resulting Account creation
	// atomic together (invite-accept does) calls both from inside the
	// same TenantScope.WithTenant closure, which already shares one
	// transaction; MarkUsed itself opens no transaction of its own.
	MarkUsed(ctx context.Context, id uuid.UUID, usedAt time.Time) (Invite, error)

	// SetTOTPSecret attaches a freshly generated TOTP secret to the
	// invite, for its enrolment step
	// (internal/auth.InviteManager.IssueTOTPSecret). Guarded the same way
	// MarkUsed is ("WHERE id = $1 AND used_at IS NULL"): an implementation
	// must fail with ErrConflict, not silently succeed, when the invite
	// is already redeemed by the time the write takes effect, so a secret
	// can never be attached to, or swapped under, an already-used invite.
	SetTOTPSecret(ctx context.Context, id uuid.UUID, secret string) (Invite, error)
}
