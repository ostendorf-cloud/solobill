package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// assignmentTx is a pgx.Tx that records every statement. The effective
// row lookup finds effective when it is set; the insert answers with no
// row. Any other method panics on the nil embedded interface.
type assignmentTx struct {
	pgx.Tx

	mu         sync.Mutex
	statements []string
	effective  *uuid.UUID
}

func (tx *assignmentTx) record(sql string) {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	tx.statements = append(tx.statements, sql)
}

func (tx *assignmentTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	tx.record(sql)

	return pgconn.CommandTag{}, nil
}

// effectiveRow scans an effective assignment with the given id.
type effectiveRow struct{ id *uuid.UUID }

func (r effectiveRow) Scan(dest ...any) error {
	if r.id == nil {
		return pgx.ErrNoRows
	}
	if p, ok := dest[0].(*uuid.UUID); ok {
		*p = *r.id
	}

	return nil
}

func (tx *assignmentTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	tx.record(sql)
	// sqlc prefixes every statement with its "-- name: X" comment.
	if strings.Contains(sql, "name: EffectiveEuerAssignmentOfEntry ") {
		return effectiveRow{id: tx.effective}
	}

	return noRow{}
}

func (tx *assignmentTx) sent(name string) int {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	for i, s := range tx.statements {
		if strings.Contains(s, "name: "+name+" ") {
			return i
		}
	}

	return -1
}

// TestEuerAssignmentCreateNamesTheEffectiveRow pins the Go side of the
// supersede rule, without a database: a first assignment supersedes
// nothing, a changed one names the effective row, and anything else is
// storage.ErrConflict before an INSERT is sent. The lock comes first.
func TestEuerAssignmentCreateNamesTheEffectiveRow(t *testing.T) {
	t.Parallel()

	effective, other := uuid.New(), uuid.New()
	tests := []struct {
		name       string
		effective  *uuid.UUID
		supersedes *uuid.UUID
		wantInsert bool
	}{
		{"first assignment", nil, nil, true},
		{"supersedes a row although none is effective", nil, &other, false},
		{"names the effective row", &effective, &effective, true},
		{"a second first assignment", &effective, nil, false},
		{"names another row", &effective, &other, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tx := &assignmentTx{effective: tt.effective}
			ctx := context.WithValue(context.Background(), txContextKey{}, pgx.Tx(tx))

			_, err := (&euerAssignmentRepository{}).Create(ctx, storage.EuerAssignment{
				TenantID: uuid.New(), EntryKind: storage.EuerEntryExpense, EntryID: uuid.New(), TaxYear: 2026,
				SupersedesID: tt.supersedes,
			})

			insert := tx.sent("CreateEuerAssignment")
			if got := insert >= 0; got != tt.wantInsert {
				t.Fatalf("insert sent = %v, want %v (err %v)", got, tt.wantInsert, err)
			}
			if !tt.wantInsert && err != storage.ErrConflict { //nolint:errorlint // the contract is the bare sentinel
				t.Errorf("err = %v, want the bare storage.ErrConflict", err)
			}
			if tt.wantInsert && (err == nil || errors.Is(err, storage.ErrConflict)) {
				// The fake answers the INSERT with no row, so only that
				// error may come back.
				t.Errorf("err = %v, want the fake's missing row from the insert", err)
			}
			lock, lookup := tx.sent("LockEuerAssignments"), tx.sent("EffectiveEuerAssignmentOfEntry")
			if lock < 0 || lookup < lock || (insert >= 0 && insert < lookup) {
				t.Errorf("statements = lock %d, lookup %d, insert %d, want that order", lock, lookup, insert)
			}
		})
	}
}

func TestEuerAssignmentRepositoryNeedsATenantScope(t *testing.T) {
	t.Parallel()

	repo := &euerAssignmentRepository{}
	if _, err := repo.Create(context.Background(), storage.EuerAssignment{}); !errors.Is(err, storage.ErrNoTenantScope) {
		t.Errorf("Create err = %v, want ErrNoTenantScope", err)
	}
	if _, err := repo.ListEffective(context.Background()); !errors.Is(err, storage.ErrNoTenantScope) {
		t.Errorf("ListEffective err = %v, want ErrNoTenantScope", err)
	}
}
