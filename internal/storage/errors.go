package storage

import "errors"

var (
	// ErrNotFound is returned when a repository lookup matches no row.
	ErrNotFound = errors.New("not found")

	// ErrConflict is returned when a write would violate a uniqueness
	// constraint: a tenant slug, a (tenant, email) pair, or a token hash
	// that already exists. InviteRepository.MarkUsed also returns it for
	// an invite that is already used, including one a concurrent call
	// just used: that is a write losing to another write, the same shape
	// of conflict, even though no uniqueness constraint is the mechanism.
	ErrConflict = errors.New("conflict")

	// ErrNoTenantScope is returned by an AccountRepository or
	// InviteRepository method called on a context that TenantScope.WithTenant
	// did not produce. There is deliberately no fallback to an unscoped
	// query; see TenantScope.
	ErrNoTenantScope = errors.New("no tenant scope set on context")
)
