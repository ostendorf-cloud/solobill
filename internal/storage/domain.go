// Package storage defines the domain types and repository interfaces that
// later business logic (internal/auth and beyond) calls to persist and
// query tenants, local accounts and invites. internal/storage/postgres is
// the only implementation; nothing here leaks a pgx or sqlc type.
package storage

import (
	"time"

	"github.com/google/uuid"
)

// Tenant is the root of multi-tenancy: every tenant-scoped row carries a
// TenantID referencing one. Tenant itself carries no tenant_id column and
// no row level security policy, because a row here IS a tenant, not
// something owned by one; see db/migrations for the full reasoning.
type Tenant struct {
	ID        uuid.UUID
	Slug      string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AccountRole is the role a local account or invite carries within its
// tenant. The set is deliberately minimal for this step; internal/auth, a
// later step, owns the full role and permission model.
type AccountRole string

const (
	// AccountRoleOwner can invite other accounts.
	AccountRoleOwner AccountRole = "owner"
	// AccountRoleMember cannot invite other accounts.
	AccountRoleMember AccountRole = "member"
)

// AccountStatus is the lifecycle state of a local account.
type AccountStatus string

const (
	// AccountStatusPending is an invited account that has not yet set a
	// password and enrolled TOTP.
	AccountStatusPending AccountStatus = "pending"
	// AccountStatusActive has both a password and a TOTP secret set.
	AccountStatusActive AccountStatus = "active"
	// AccountStatusDisabled had its access revoked.
	AccountStatusDisabled AccountStatus = "disabled"
)

// Account is a local (non-OIDC) login, scoped to one tenant. PasswordHash
// and TOTPSecret are nil until internal/auth, a later step, computes and
// sets them; this package never hashes a password or generates a TOTP
// secret, it only stores and returns whatever internal/auth already
// computed.
type Account struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	Email        string
	PasswordHash *string
	TOTPSecret   *string
	Role         AccountRole
	Status       AccountStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Invite is a single-use, time-limited invitation to create an Account
// under one tenant and role. TokenHash is the hash of the invite token,
// never the token itself: internal/auth, a later step, generates the
// token and computes and verifies the hash; this package only stores and
// looks up the hash.
type Invite struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Email     string
	Role      AccountRole
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedBy *uuid.UUID
	CreatedAt time.Time
}
