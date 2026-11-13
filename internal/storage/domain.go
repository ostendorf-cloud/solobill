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
	// Unit is never empty once read back: the column is NOT NULL. Create
	// and Update treat the Go zero value differently, deliberately:
	// Create defaults it to "piece" when the caller leaves it unset,
	// mirroring the column's own DEFAULT, for a genuinely new item not
	// yet given a unit. Update is a full-column replace, the same
	// contract Description and SKU already carry, so it does not
	// default an empty Unit; the caller must supply the item's current
	// Unit, and leaving it empty fails against the column's CHECK
	// constraint instead of silently rewriting the item to "piece". See
	// db/migrations' catalogue_items migration for the full set and
	// reasoning, and postgres.catalogueItemRepository.Update for why the
	// two methods disagree on purpose.
	Unit string
	// DeletedAt is nil for an active item, set once soft deleted. A
	// catalogue item is never hard deleted: see db/migrations'
	// catalogue_items migration for why.
	DeletedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DocumentType is what a Document actually is. This step (issue #15,
// step 2) only ever writes DocumentTypeInvoice and DocumentTypeCorrection:
// offers are a later milestone on the same model, per the issue's own
// scope, and are deliberately not a value here yet; see db/migrations'
// documents migration for why adding one later is a cheap, additive
// change rather than a reason to add it speculatively now.
type DocumentType string

const (
	// DocumentTypeInvoice is an ordinary, independently issued invoice.
	DocumentTypeInvoice DocumentType = "invoice"
	// DocumentTypeCorrection is its own document, referencing an
	// original invoice via Document.CorrectsDocumentID, never a
	// modification of it: see Document.CorrectsDocumentID and
	// DocumentRepository for how immutability is actually enforced.
	DocumentTypeCorrection DocumentType = "correction"
)

// DocumentState is a Document's lifecycle state. "A state transition is a
// new row" (docs/concepts/solobill.md) is implemented at the document
// level, not by a transitions table of its own: DocumentStateDraft to
// DocumentStateIssued is the one in-place transition this package ever
// allows (see DocumentRepository.Issue); the only transition an issued
// document can go through after that is answered by creating a wholly
// new Document (a correction) that references it, never by writing a
// third state onto the same row. See db/migrations' documents migration
// for the fuller reasoning behind not adding a separate transitions
// table for this.
type DocumentState string

const (
	// DocumentStateDraft is mutable: its header and its lines can still
	// be changed or the whole document discarded.
	DocumentStateDraft DocumentState = "draft"
	// DocumentStateIssued is frozen: see Document.FrozenPayload. Nothing
	// in this package can change a row once it carries this state; see
	// db/migrations' documents_block_issued_mutation trigger, the actual
	// enforcement mechanism, not a convention this package merely
	// follows.
	DocumentStateIssued DocumentState = "issued"
)

// VATRegime is the tax treatment a Document is issued under; both are
// explicit goals of docs/concepts/solobill.md.
type VATRegime string

const (
	// VATRegimeStandard charges VAT normally, per line, at that line's
	// own VATRatePermyriad.
	VATRegimeStandard VATRegime = "standard"
	// VATRegimeKleinunternehmer is section 19 UStG: no VAT is charged,
	// which in practice means every line's VATRatePermyriad is 0, but
	// the regime itself is still recorded, since the wording a rendered
	// document carries differs by regime even when the tax figure does
	// not.
	VATRegimeKleinunternehmer VATRegime = "kleinunternehmer"
)

// Document is one entry in the append-only ledger
// docs/concepts/solobill.md's Approach section describes: an offer,
// invoice or correction a tenant has drafted or issued, scoped to one
// tenant. Lines is populated by DocumentRepository.FindByID and
// DocumentRepository.Create/Update (its own input and output), ordered
// by each DocumentLine's Position; DocumentRepository.List leaves it
// nil, since a list view never needs every line of every document.
//
// A draft (State == DocumentStateDraft) has every field below Lines
// unset except Type, CustomerID, VATRegime and whichever of
// ServiceDate/DueDate/the discount fields the caller has filled in so
// far. An issued document (State == DocumentStateIssued) has Number,
// NumberYear, IssuedAt, FrozenPayload and FrozenPayloadSHA256 all set
// together, and never changes again; see
// db/migrations' documents_issued_fields_consistent and
// documents_block_issued_mutation.
type Document struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	Type     DocumentType
	State    DocumentState
	// CustomerID is this document's customer. Composite foreign key to
	// customers (tenant_id, id); see db/migrations' documents migration
	// for why a plain, single-column reference is never used across a
	// row-level-security boundary in this schema.
	CustomerID uuid.UUID
	// CorrectsDocumentID names the original invoice this document
	// corrects. nil for Type == DocumentTypeInvoice; set, and only set,
	// for Type == DocumentTypeCorrection: see db/migrations'
	// documents_correction_requires_original.
	CorrectsDocumentID *uuid.UUID
	VATRegime          VATRegime
	// ServiceDate is the Leistungsdatum/Lieferdatum German VAT law
	// (§14 UStG) requires, separate from the document's own date
	// (IssuedAt once issued). nil until known, which is routinely the
	// case while a draft is still being edited.
	ServiceDate *time.Time
	// DueDate is nil until decided, for the same reason ServiceDate is.
	DueDate *time.Time
	// DiscountPercentPermyriad and DiscountAmountCents are this
	// document's own discount, mutually exclusive, both nil for no
	// document-level discount at all. Permyriad (parts per 10,000:
	// 1500 = 15.00%): see db/migrations' documents migration for why
	// this is a scaled integer, never a Postgres numeric and never a
	// float. internal/money's Calculate allocates whichever is set
	// across this document's lines per docs/concepts/solobill.md's
	// Rounding section; this package only stores and returns the
	// configured discount itself, never that allocation.
	DiscountPercentPermyriad *int32
	DiscountAmountCents      *int64
	// Number is this document's own number within Type and NumberYear:
	// nil for a draft, set exactly once, together with NumberYear,
	// IssuedAt, FrozenPayload and FrozenPayloadSHA256, by
	// DocumentRepository.Issue (or, for an imported historical document,
	// a later import step setting Imported true and Number/NumberYear
	// directly, outside DocumentNumberRepository's counter entirely; see
	// Imported).
	Number     *int64
	NumberYear *int32
	// Imported marks a document brought in from before this tool
	// existed, carrying the Number it was actually issued under rather
	// than one DocumentNumberRepository allocated.
	// docs/concepts/solobill.md requires this to be possible so that an
	// import can never shift or reuse a number a customer already
	// holds; the import itself (reading a historical record and setting
	// this) is a later milestone, out of this step's scope. False for
	// every document this step's own code ever creates.
	Imported bool
	// FrozenPayload is the complete, self-contained snapshot an issued
	// document is rendered from forever: nil until State ==
	// DocumentStateIssued. See DocumentPayload for its shape, and
	// FrozenPayloadSHA256 for how it is verified.
	FrozenPayload []byte
	// FrozenPayloadSHA256 is the lowercase hex-encoded SHA-256 of
	// FrozenPayload's own bytes, exactly as persisted: nil until State
	// == DocumentStateIssued. Computed once, by whichever caller freezes
	// the payload (the issuing step; see
	// db/migrations' documents migration for why this package never
	// re-derives it from a separate canonical re-encoding).
	FrozenPayloadSHA256 *string
	// IssuedAt is nil until State == DocumentStateIssued, set exactly
	// once, together with Number, NumberYear, FrozenPayload and
	// FrozenPayloadSHA256.
	IssuedAt  *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
	// Lines is this document's lines, ordered by DocumentLine.Position;
	// see the type's own doc comment for when it is populated.
	Lines []DocumentLine
}

// DocumentLine is one billable line on a Document, scoped to the same
// tenant as its document. Nothing here is a computed total:
// internal/money's Calculate derives a line's net amount, its discount
// in cents, its share of the document's own discount and its VAT
// entirely from the fields below, at render time for a draft and once,
// frozen into Document.FrozenPayload, at issue time; see db/migrations'
// document_lines migration for why no computed column is stored here
// instead.
type DocumentLine struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	DocumentID uuid.UUID
	// Position is this line's place within its document, used both for
	// display and as the tie-break order internal/money's
	// largest-remainder discount allocation needs; see db/migrations'
	// document_lines migration.
	Position int32
	// CatalogueItemID is nil for an ad hoc line with no catalogue item
	// behind it. Kept for reporting even though Description, UnitPriceCents
	// and Unit below are copied onto this line and do not need it for
	// referential integrity; see db/migrations' document_lines migration.
	CatalogueItemID *uuid.UUID
	// Description, UnitPriceCents and Unit are copied from the catalogue
	// item (when CatalogueItemID is set) at the moment this line is
	// added to a draft, or typed directly for an ad hoc line; never
	// re-read from catalogue_items afterward, so a later price change or
	// rename on the catalogue item never moves an already-drafted line.
	Description string
	// QuantityThousandths is this line's quantity scaled by 1000 (three
	// decimal places: 2500 = 2.5). Scaled integer, never a Postgres
	// numeric and never a float; see db/migrations' document_lines
	// migration for the full reasoning, the same one
	// catalogue_items.unit_price_cents already established for money.
	QuantityThousandths int64
	UnitPriceCents      int64
	// Unit is one of "piece", "hour" or "day"; see catalogue_items'
	// migration for why this is the complete set today.
	Unit string
	// VATRatePermyriad is this line's own VAT rate, in parts per 10,000
	// (1900 = 19.00%). 0 covers both a genuinely zero-rated line and
	// every line under VATRegimeKleinunternehmer.
	VATRatePermyriad int32
	// LineDiscountPercentPermyriad and LineDiscountAmountCents are this
	// line's own discount, mutually exclusive, both nil for no
	// line-level discount, entirely independent of the document's own
	// discount fields above.
	LineDiscountPercentPermyriad *int32
	LineDiscountAmountCents      *int64
	CreatedAt                    time.Time
	UpdatedAt                    time.Time
}
