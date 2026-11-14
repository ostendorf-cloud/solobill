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
// Two statements, in this exact order, both against the same row and
// both inside tx:
//
//  1. EnsureDocumentNumberRow: an idempotent upsert that does nothing if
//     the (tenant, docType, year) counter already exists, and creates it
//     at last_number = 0 if this is the very first allocation ever
//     requested under this key. Without this, IncrementDocumentNumber
//     below would find no row to update at all on a brand new key.
//  2. IncrementDocumentNumber: last_number + 1, persisted and returned in
//     one UPDATE ... RETURNING. This single statement is the lock
//     issue #15's gaplessness guarantee rests on, not a separate SELECT
//     ... FOR UPDATE: see queries/document_numbers.sql for why the
//     UPDATE's own row lock and Postgres' READ COMMITTED re-evaluation
//     of its SET expression against the latest committed row already
//     serialise two concurrent calls for the same tenant, docType and
//     year without a second round trip. Two different tenants, two
//     different types, or two different years never contend for this
//     lock at all, since each is its own row.
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

	next, err := queries.IncrementDocumentNumber(ctx, sqlcgen.IncrementDocumentNumberParams{
		Type:       string(docType),
		NumberYear: yearInt32,
	})
	if err != nil {
		return 0, fmt.Errorf("increment document number: %w", err)
	}

	return next, nil
}
