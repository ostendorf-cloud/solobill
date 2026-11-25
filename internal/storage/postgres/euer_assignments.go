package postgres

import (
	"context"
	"errors"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres/sqlcgen"
)

// euerAssignmentRepository satisfies storage.EuerAssignmentRepository.
// Every method requires a context storage.TenantScope.WithTenant
// produced; see txFromContext.
type euerAssignmentRepository struct{}

// Create implements storage.EuerAssignmentRepository. It takes a per
// tenant advisory lock first, so the effective row it compares
// SupersedesID with cannot change before the insert. The unique indexes
// of the table refuse the same as a last guard: a second first row of an
// entry, or a second row superseding one row, is storage.ErrConflict.
//
// The table's own insert trigger refuses an entry that does not exist,
// is no assignable entry, or lies outside the turn of the year. The
// handler checks the same first; such an error passes through wrapped.
func (r *euerAssignmentRepository) Create(ctx context.Context, a storage.EuerAssignment) (storage.EuerAssignment, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.EuerAssignment{}, err
	}

	q := sqlcgen.New(tx)
	if err := q.LockEuerAssignments(ctx); err != nil {
		return storage.EuerAssignment{}, mapWriteError(err)
	}

	effective, err := q.EffectiveEuerAssignmentOfEntry(ctx, sqlcgen.EffectiveEuerAssignmentOfEntryParams{
		EntryKind: string(a.EntryKind),
		EntryID:   a.EntryID,
	})
	switch err = mapReadError(err); {
	case errors.Is(err, storage.ErrNotFound):
		if a.SupersedesID != nil {
			return storage.EuerAssignment{}, storage.ErrConflict
		}
	case err != nil:
		return storage.EuerAssignment{}, err
	default:
		if a.SupersedesID == nil || *a.SupersedesID != effective.ID {
			return storage.EuerAssignment{}, storage.ErrConflict
		}
	}

	row, err := q.CreateEuerAssignment(ctx, sqlcgen.CreateEuerAssignmentParams{
		TenantID:     a.TenantID,
		EntryKind:    string(a.EntryKind),
		EntryID:      a.EntryID,
		TaxYear:      a.TaxYear,
		Note:         ptrToText(a.Note),
		SupersedesID: ptrToUUID(a.SupersedesID),
		CreatedBy:    ptrToUUID(a.CreatedBy),
	})
	if err != nil {
		return storage.EuerAssignment{}, mapWriteError(err)
	}

	return euerAssignmentFromRow(row), nil
}

// ListEffective implements storage.EuerAssignmentRepository.
func (r *euerAssignmentRepository) ListEffective(ctx context.Context) ([]storage.EuerAssignment, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := sqlcgen.New(tx).ListEffectiveEuerAssignments(ctx)
	if err != nil {
		return nil, mapReadError(err)
	}

	out := make([]storage.EuerAssignment, len(rows))
	for i, row := range rows {
		out[i] = euerAssignmentFromRow(row)
	}

	return out, nil
}

func euerAssignmentFromRow(row sqlcgen.EuerAssignment) storage.EuerAssignment {
	return storage.EuerAssignment{
		ID:           row.ID,
		TenantID:     row.TenantID,
		EntryKind:    storage.EuerEntryKind(row.EntryKind),
		EntryID:      row.EntryID,
		TaxYear:      row.TaxYear,
		Note:         textToPtr(row.Note),
		SupersedesID: uuidToPtr(row.SupersedesID),
		CreatedBy:    uuidToPtr(row.CreatedBy),
		CreatedAt:    timeFromTimestamptz(row.CreatedAt),
	}
}
