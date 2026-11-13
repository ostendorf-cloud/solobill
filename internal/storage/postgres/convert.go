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

// int8ToPtr and ptrToInt8 convert between a nullable Postgres bigint
// column (for example accounts.totp_last_counter) and an *int64.

func int8ToPtr(i pgtype.Int8) *int64 {
	if !i.Valid {
		return nil
	}

	v := i.Int64

	return &v
}

func ptrToInt8(i *int64) pgtype.Int8 {
	if i == nil {
		return pgtype.Int8{}
	}

	return pgtype.Int8{Int64: *i, Valid: true}
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

// dateToPtr and ptrToDate convert between a nullable Postgres date
// column (for example documents.service_date and documents.due_date)
// and a *time.Time. Nothing in this package reads the time-of-day
// component of either side: a date column carries none, and pgx itself
// always decodes one with a zero time-of-day in UTC.

func dateToPtr(d pgtype.Date) *time.Time {
	if !d.Valid {
		return nil
	}

	v := d.Time

	return &v
}

func ptrToDate(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{}
	}

	return pgtype.Date{Time: *t, Valid: true}
}

// int4ToPtr and ptrToInt4 convert between a nullable Postgres integer
// column (for example documents.discount_percent_permyriad and
// documents.number_year) and an *int32.

func int4ToPtr(i pgtype.Int4) *int32 {
	if !i.Valid {
		return nil
	}

	v := i.Int32

	return &v
}

func ptrToInt4(i *int32) pgtype.Int4 {
	if i == nil {
		return pgtype.Int4{}
	}

	return pgtype.Int4{Int32: *i, Valid: true}
}
