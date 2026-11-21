package postgres

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

func TestMapReadError(t *testing.T) {
	t.Parallel()

	genericErr := errors.New("connection reset")

	tests := []struct {
		name    string
		input   error
		want    error
		wantNil bool
	}{
		{name: "nil error", input: nil, wantNil: true},
		{name: "no rows", input: pgx.ErrNoRows, want: storage.ErrNotFound},
		{name: "wrapped no rows", input: fmt.Errorf("query: %w", pgx.ErrNoRows), want: storage.ErrNotFound},
		{name: "other error", input: genericErr, want: genericErr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := mapReadError(tt.input)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("mapReadError(nil) = %v, want nil", got)
				}

				return
			}

			if !errors.Is(got, tt.want) {
				t.Errorf("mapReadError(%v) = %v, want it to match %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestMapWriteError(t *testing.T) {
	t.Parallel()

	genericErr := errors.New("connection reset")
	uniqueViolation := &pgconn.PgError{Code: pgUniqueViolation}
	otherPgError := &pgconn.PgError{Code: "23503"} // foreign_key_violation

	tests := []struct {
		name    string
		input   error
		want    error
		wantNil bool
	}{
		{name: "nil error", input: nil, wantNil: true},
		{name: "unique violation", input: uniqueViolation, want: storage.ErrConflict},
		{name: "wrapped unique violation", input: fmt.Errorf("insert: %w", uniqueViolation), want: storage.ErrConflict},
		{name: "other postgres error", input: otherPgError, want: otherPgError},
		{name: "other error", input: genericErr, want: genericErr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := mapWriteError(tt.input)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("mapWriteError(nil) = %v, want nil", got)
				}

				return
			}

			if !errors.Is(got, tt.want) {
				t.Errorf("mapWriteError(%v) = %v, want it to match %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestMapLedgerAndDocumentWriteError pins the one difference between the two
// write mappings: a foreign key violation is storage.ErrConflict for a
// document and passes through with its SQLSTATE for an expense or other
// income row. It runs without a database, because the integration tests that
// read the SQLSTATE are the only other guard and they skip without one.
func TestMapLedgerAndDocumentWriteError(t *testing.T) {
	t.Parallel()

	wrap := func(code string) error {
		return fmt.Errorf("insert: %w", &pgconn.PgError{Code: code})
	}

	tests := []struct {
		code             string
		ledgerConflict   bool
		documentConflict bool
	}{
		{code: pgUniqueViolation, ledgerConflict: true, documentConflict: true},
		{code: pgIntegrityConstraintViolationClass, ledgerConflict: true, documentConflict: true},
		{code: pgForeignKeyViolation, ledgerConflict: false, documentConflict: true},
		{code: "23514", ledgerConflict: false, documentConflict: false},
		{code: "42501", ledgerConflict: false, documentConflict: false},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			t.Parallel()

			cases := map[string]struct {
				mapFn    func(error) error
				conflict bool
			}{
				"ledger":   {mapFn: mapLedgerWriteError, conflict: tt.ledgerConflict},
				"document": {mapFn: mapDocumentWriteError, conflict: tt.documentConflict},
			}

			for name, tc := range cases {
				got := tc.mapFn(wrap(tt.code))

				if errors.Is(got, storage.ErrConflict) != tc.conflict {
					t.Errorf("%s mapping of %s = %v, want conflict %v", name, tt.code, got, tc.conflict)
				}

				var pgErr *pgconn.PgError
				if !tc.conflict && (!errors.As(got, &pgErr) || pgErr.Code != tt.code) {
					t.Errorf("%s mapping of %s = %v, want the raw SQLSTATE", name, tt.code, got)
				}
			}
		})
	}

	if mapLedgerWriteError(nil) != nil || mapDocumentWriteError(nil) != nil {
		t.Error("a nil error mapped to a non-nil error")
	}
}
