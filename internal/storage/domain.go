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
	// TOTPLastCounter is the RFC 6238 counter the most recently accepted
	// TOTP code matched, nil until a first successful login or invite
	// acceptance. internal/auth.VerifyTOTPCode's caller refuses a code
	// whose counter is not strictly greater than this, so the exact same
	// code cannot be replayed.
	TOTPLastCounter *int64
	// OIDCSubject is the identity provider's stable subject claim this
	// account is provisioned for, nil for a local-only account.
	// internal/auth.ResolveOIDCIdentity matches on this, never on email.
	OIDCSubject *string
	Role        AccountRole
	Status      AccountStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
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
	// TOTPSecret is the server-generated TOTP secret this invite's
	// enrolment step (internal/auth.InviteManager.IssueTOTPSecret) issued,
	// nil until that step runs. AcceptInvite verifies the submitted code
	// against this value, never against anything the client supplies.
	TOTPSecret *string
}

// Customer is a person or company a tenant issues offers and invoices
// to, scoped to one tenant. Every address field is nil until known: a
// customer is routinely entered with only a name, and completeness at
// issuing time is a later business-logic validation, not something this
// package enforces. DeletedAt is nil for an active customer; see
// CustomerRepository for the FindByID-versus-List asymmetry a soft
// delete requires.
type Customer struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	Name     string
	Email    *string
	// VATID is the EU VAT identification number (USt-IdNr.), nil until
	// known. Its format varies by EU member state; this package never
	// validates it.
	VATID      *string
	Street     *string
	PostalCode *string
	City       *string
	// CountryCode is ISO 3166-1 alpha-2, nil until known. There is
	// deliberately no default: picking one is a business-policy decision
	// for a later UI layer, not this package's.
	CountryCode *string
	// DeletedAt is nil for an active customer, set once soft deleted. A
	// customer is never hard deleted: see db/migrations' customers
	// migration for why.
	DeletedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CatalogueItem is a good or service a tenant can bill, with its current
// price, scoped to one tenant. UnitPriceCents is an integer minor unit
// (implicitly EUR cents: multi-currency is an explicit non-goal, see
// docs/concepts/solobill.md), never a float, so "round to the nearest
// cent" is a representation guarantee rather than something arithmetic
// elsewhere has to remember. This struct carries no price history: a
// document line (a later step) copies the price onto itself, and that
// copy, not anything here, is what keeps an issued document's totals
// frozen even after this item's price later changes.
type CatalogueItem struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	Name        string
	Description *string
	// SKU is optional and user-supplied, never generated: a human-assigned
	// mnemonic lookup key, unique per tenant among active items,
	// case-insensitively, when set. nil means none was assigned.
	SKU *string
	// UnitPriceCents is this item's current price, in integer minor
	// units. Never negative; see db/migrations' catalogue_items
	// migration.
	UnitPriceCents int64
	// DeletedAt is nil for an active item, set once soft deleted. A
	// catalogue item is never hard deleted: see db/migrations'
	// catalogue_items migration for why.
	DeletedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}
