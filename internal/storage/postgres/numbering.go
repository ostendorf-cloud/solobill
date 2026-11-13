package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres/sqlcgen"
)

// documentNumberRepository satisfies storage.DocumentNumberRepository.
// Every method requires a context storage.TenantScope.WithTenant
// produced; see txFromContext.
type documentNumberRepository struct{}

// AllocateNext implements storage.DocumentNumberRepository. See
// allocateNextDocumentNumber for the actual locking and allocation; this
// method only resolves the transaction from ctx the same way every other
// tenant-scoped repository in this package does.
func (r *documentNumberRepository) AllocateNext(ctx context.Context, docType storage.DocumentType, year int) (int64, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return 0, err
	}

	return allocateNextDocumentNumber(ctx, tx, docType, year)
}

// Current implements storage.DocumentNumberRepository. A missing row
// (nothing issued yet under this key) answers pgx.ErrNoRows from
// GetDocumentNumber; that is not an error condition for this method, it
// is exactly what "0 issued so far" looks like, so it is translated to
// (0, nil) rather than propagated as storage.ErrNotFound (which would
// wrongly suggest the caller asked for a row that should already exist).
func (r *documentNumberRepository) Current(ctx context.Context, docType storage.DocumentType, year int) (int64, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return 0, err
	}

	last, err := sqlcgen.New(tx).GetDocumentNumber(ctx, sqlcgen.GetDocumentNumberParams{
		Type:       string(docType),
		NumberYear: int32(year), //nolint:gosec // year is a calendar year, far inside int32's range
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}

		return 0, fmt.Errorf("postgres: %w", err)
	}

	return last, nil
}

// allocateNextDocumentNumber is the actual gapless-numbering mechanism
// docs/concepts/solobill.md's Approach section describes, shared by
// documentNumberRepository.AllocateNext and documentRepository.Issue (the
// issuing transaction calls this directly, on the same tx it is already
// running in, rather than going through a second, independent
// transaction): a per-tenant, per-type, per-year counter allocated inside
// the caller's own transaction, so a transaction that allocates a number
// and then rolls back takes that number back with it.
//
// Three statements, in this exact order, all against the same row and
// all inside tx:
//
//  1. EnsureDocumentNumberRow: an idempotent upsert that does nothing if
//     the (tenant, docType, year) counter already exists, and creates it
//     at last_number = 0 if this is the very first allocation ever
//     requested under this key. Without this, LockDocumentNumber below
//     would find no row to lock at all on a brand new key.
//  2. LockDocumentNumber: SELECT ... FOR UPDATE on exactly that row. This
//     is the lock issue #15's gaplessness guarantee rests on: a second,
//     concurrent call for the same tenant, docType and year blocks here
//     until this transaction commits (having consumed the number it is
//     about to allocate) or rolls back (having consumed nothing), and
//     only then proceeds to read the row for itself. Two different
//     tenants, two different types, or two different years never
//     contend for this lock at all, since each is its own row.
//  3. IncrementDocumentNumber: last_number + 1, persisted and returned in
//     one UPDATE. Because this transaction already holds step 2's lock
//     on this row, no other transaction's step 3 can land between this
//     transaction's own step 2 and step 3.
//
// LockDocumentNumber's own result is deliberately unused beyond
// acquiring the lock: IncrementDocumentNumber's RETURNING already gives
// the authoritative post-increment value, and reading last_number twice
// (once from the lock, once from the increment) would just be two
// different names for the same fact, read a moment apart on a row this
// transaction already has exclusively locked.
func allocateNextDocumentNumber(ctx context.Context, tx pgx.Tx, docType storage.DocumentType, year int) (int64, error) {
	yearInt32 := int32(year) //nolint:gosec // year is a calendar year, far inside int32's range

	queries := sqlcgen.New(tx)

	// EnsureDocumentNumberRow reads the tenant to insert under from the
	// same current_setting('app.tenant_id', true) every other query in
	// this package is scoped by; see queries/document_numbers.sql.
	if err := queries.EnsureDocumentNumberRow(ctx, sqlcgen.EnsureDocumentNumberRowParams{
		Type:       string(docType),
		NumberYear: yearInt32,
	}); err != nil {
		return 0, fmt.Errorf("ensure document number row: %w", err)
	}

	if _, err := queries.LockDocumentNumber(ctx, sqlcgen.LockDocumentNumberParams{
		Type:       string(docType),
		NumberYear: yearInt32,
	}); err != nil {
		return 0, fmt.Errorf("lock document number: %w", err)
	}

	next, err := queries.IncrementDocumentNumber(ctx, sqlcgen.IncrementDocumentNumberParams{
		Type:       string(docType),
		NumberYear: yearInt32,
	})
	if err != nil {
		return 0, fmt.Errorf("increment document number: %w", err)
	}

	return next, nil
}
