package postgres

import (
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// textToPtr and ptrToText convert between a nullable Postgres text column
// and the *string a domain struct such as storage.Account uses for the
// same nullability (nil until internal/auth sets a value).

func textToPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}

	v := t.String

	return &v
}

func ptrToText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}

	return pgtype.Text{String: *s, Valid: true}
}

// uuidToPtr and ptrToUUID convert between a nullable Postgres uuid column
// (for example invites.created_by) and a *uuid.UUID.

func uuidToPtr(u pgtype.UUID) *uuid.UUID {
	if !u.Valid {
		return nil
	}

	id := uuid.UUID(u.Bytes)

	return &id
}

func ptrToUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}

	return pgtype.UUID{Bytes: *id, Valid: true}
}

// timeToPtr and ptrToTimestamptz convert between a nullable Postgres
// timestamptz column (for example invites.used_at) and a *time.Time.

func timeToPtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}

	v := t.Time

	return &v
}

func ptrToTimestamptz(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}

	return pgtype.Timestamptz{Time: *t, Valid: true}
}

// timeFromTimestamptz and toTimestamptz convert a NOT NULL timestamptz
// column (for example created_at, always set by a database default) to
// and from a plain time.Time.

func timeFromTimestamptz(t pgtype.Timestamptz) time.Time {
	return t.Time
}

func toTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
