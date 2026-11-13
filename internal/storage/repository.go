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

// CustomerRepository persists and queries customers. Every method must
// run inside a context TenantScope.WithTenant produced; see
// ErrNoTenantScope.
type CustomerRepository interface {
	Create(ctx context.Context, customer Customer) (Customer, error)

	// FindByID does not filter on a soft-deleted row: a later document
	// still has to resolve a customer it references by id, even one that
	// has since been deleted, so a point lookup by id must keep
	// answering for it. This is the opposite of List, deliberately; see
	// List.
	FindByID(ctx context.Context, id uuid.UUID) (Customer, error)

	// List returns only active (not soft-deleted) customers: it backs a
	// human-facing picker (for example a document's "choose a customer"
	// step), which must never offer an already-deleted customer. This is
	// the opposite of FindByID, deliberately; see FindByID.
	List(ctx context.Context) ([]Customer, error)

	// Update is guarded against a soft-deleted row: it fails with
	// ErrNotFound, not ErrConflict, when the row does not exist or is
	// already deleted. Unlike InviteRepository.MarkUsed, there is no
	// concurrent-redeem race to report as a conflict here, only "no such
	// active row to update", which ErrNotFound already means everywhere
	// else in this package.
	Update(ctx context.Context, customer Customer) (Customer, error)

	// Delete soft deletes: it sets deleted_at rather than removing the
	// row, and fails with ErrNotFound when the row does not exist or is
	// already deleted, for the same reason Update does.
	Delete(ctx context.Context, id uuid.UUID) error
}

// CatalogueItemRepository persists and queries catalogue items. Every
// method must run inside a context TenantScope.WithTenant produced; see
// ErrNoTenantScope. The FindByID-versus-List asymmetry, and the
// ErrNotFound choice on Update and Delete, are the same as
// CustomerRepository's; see its doc comments for the reasoning.
type CatalogueItemRepository interface {
	Create(ctx context.Context, item CatalogueItem) (CatalogueItem, error)
	FindByID(ctx context.Context, id uuid.UUID) (CatalogueItem, error)
	List(ctx context.Context) ([]CatalogueItem, error)
	Update(ctx context.Context, item CatalogueItem) (CatalogueItem, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

// DocumentRepository persists and queries documents, together with their
// lines. Every method must run inside a context TenantScope.WithTenant
// produced; see ErrNoTenantScope.
//
// Immutability once State == DocumentStateIssued is not this interface's
// job to enforce: db/migrations' documents_block_issued_mutation and
// document_lines_block_issued_document_mutation triggers are what
// actually refuse an UPDATE or DELETE against an issued document or its
// lines, at the database level, so that raw SQL run through the same
// connection is refused exactly as a call through this interface is.
// Update, DeleteDraft and Issue below rely on that refusal rather than
// repeating it as an application-level check that a direct SQL
// statement could simply not call.
type DocumentRepository interface {
	// Create inserts a new draft document together with its lines, in
	// one call: a document is never meaningfully created without at
	// least the lines it is drafted with, and inserting both under the
	// same transaction is what keeps every line's Position contiguous
	// and correctly ordered from the first write onward.
	Create(ctx context.Context, document Document, lines []DocumentLine) (Document, error)

	// FindByID returns a document together with its lines, ordered by
	// each line's Position. It does not filter on State: a caller
	// resolving a correction's CorrectsDocumentID, or displaying an
	// issued document, needs exactly this, unconditionally, the same
	// FindByID-does-not-filter pattern CustomerRepository.FindByID and
	// CatalogueItemRepository.FindByID already use for their own reason.
	FindByID(ctx context.Context, id uuid.UUID) (Document, error)

	// List returns every document's own header, with Lines left nil: a
	// human-facing list view has no use for every line of every
	// document it is not currently showing.
	List(ctx context.Context) ([]Document, error)

	// Update replaces a draft's header and its complete set of lines.
	// Fails with ErrNotFound when id does not exist in this tenant at
	// all, or ErrConflict when it exists but is already issued: the
	// underlying query's zero-row result cannot tell those two cases
	// apart by itself the way CustomerRepository.Update's "AND
	// deleted_at IS NULL" guard can, since nothing here filters on
	// State; db/migrations' documents_block_issued_mutation trigger is
	// what actually refuses the write for an issued row, as a database
	// error this package recognizes and maps to ErrConflict, not a
	// query that simply matches zero rows.
	Update(ctx context.Context, document Document, lines []DocumentLine) (Document, error)

	// DeleteDraft removes a draft document and its lines (document_lines'
	// own ON DELETE CASCADE foreign key takes the lines). ErrNotFound or
	// ErrConflict for the same two cases, and the same reason, as Update.
	DeleteDraft(ctx context.Context, id uuid.UUID) error

	// Issue allocates the next number for docType and year (via
	// DocumentNumberRepository.AllocateNext, inside this call's own
	// transaction: see that method's doc comment for what that
	// allocation actually guarantees under concurrency) and writes it,
	// together with issuedAt, payload and payloadHash, moving the
	// document from DocumentStateDraft to DocumentStateIssued in one
	// statement. ErrConflict when id does not resolve to a draft in
	// this tenant (already issued, or raced by a concurrent Issue
	// call): see internal/storage/postgres/queries/documents.sql's
	// IssueDocument for why that case is a conflict, not a not-found.
	Issue(ctx context.Context, id uuid.UUID, docType DocumentType, year int, issuedAt time.Time, payload []byte, payloadHash string) (Document, error)
}

// DocumentNumberRepository allocates and reads the per-tenant,
// per-type, per-year counters docs/concepts/solobill.md's gapless
// numbering rests on. Every method must run inside a context
// TenantScope.WithTenant produced; see ErrNoTenantScope.
type DocumentNumberRepository interface {
	// AllocateNext locks docType's counter for year, for the remaining
	// lifetime of the caller's own transaction, and returns the next
	// number: the counter's last_number + 1, persisted atomically with
	// that same increment. This is the one mechanism issue #15's
	// gaplessness guarantee rests on: a caller that calls this and then
	// returns a non-nil error from its own TenantScope.WithTenant
	// closure rolls the whole transaction back, this allocation
	// included, so the number just returned is never actually issued to
	// anything, and the next successful call still gets it, not the one
	// after it. A concurrent call for the same tenant, docType and year
	// blocks here until this transaction commits or rolls back, which
	// is what makes two concurrent issues under the same key serialize
	// rather than race.
	AllocateNext(ctx context.Context, docType DocumentType, year int) (int64, error)

	// Current reads docType's counter for year without allocating or
	// locking anything, answering 0 when nothing has been issued under
	// that key yet. DocumentRepository.Issue never calls this; it
	// exists for a test asserting what the counter currently holds.
	Current(ctx context.Context, docType DocumentType, year int) (int64, error)
}
