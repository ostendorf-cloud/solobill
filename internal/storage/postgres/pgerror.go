package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// pgUniqueViolation is the Postgres error code for a unique constraint or
// unique index violation. See
// https://www.postgresql.org/docs/current/errcodes-appendix.html.
const pgUniqueViolation = "23505"

// mapReadError translates a pgx error from a lookup into a storage
// sentinel, so a caller never has to import pgx to recognize "not found".
func mapReadError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ErrNotFound
	}

	return fmt.Errorf("postgres: %w", err)
}

// mapWriteError translates a pgx error from an insert or update into a
// storage sentinel, so a caller never has to import pgx or know a
// Postgres error code to recognize a uniqueness conflict.
func mapWriteError(err error) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return storage.ErrConflict
	}

	return fmt.Errorf("postgres: %w", err)
}
