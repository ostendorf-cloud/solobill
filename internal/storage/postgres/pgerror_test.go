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
