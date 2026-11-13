package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPtrToText(t *testing.T) {
	t.Parallel()

	value := "hunter2"

	tests := []struct {
		name  string
		input *string
		want  pgtype.Text
	}{
		{name: "nil pointer", input: nil, want: pgtype.Text{}},
		{name: "empty string", input: ptr(""), want: pgtype.Text{String: "", Valid: true}},
		{name: "set value", input: &value, want: pgtype.Text{String: value, Valid: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ptrToText(tt.input)
			if got != tt.want {
				t.Errorf("ptrToText(%v) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestTextToPtr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input pgtype.Text
		want  *string
	}{
		{name: "not valid", input: pgtype.Text{String: "ignored", Valid: false}, want: nil},
		{name: "empty string", input: pgtype.Text{String: "", Valid: true}, want: ptr("")},
		{name: "set value", input: pgtype.Text{String: "hunter2", Valid: true}, want: ptr("hunter2")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := textToPtr(tt.input)
			assertPtrStringEqual(t, got, tt.want)
		})
	}
}

func TestPtrToUUID(t *testing.T) {
	t.Parallel()

	id := uuid.New()

	tests := []struct {
		name  string
		input *uuid.UUID
		want  pgtype.UUID
	}{
		{name: "nil pointer", input: nil, want: pgtype.UUID{}},
		{name: "set value", input: &id, want: pgtype.UUID{Bytes: id, Valid: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ptrToUUID(tt.input)
			if got != tt.want {
				t.Errorf("ptrToUUID(%v) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestUUIDToPtr(t *testing.T) {
	t.Parallel()

	id := uuid.New()

	tests := []struct {
		name  string
		input pgtype.UUID
		want  *uuid.UUID
	}{
		{name: "not valid", input: pgtype.UUID{Bytes: id, Valid: false}, want: nil},
		{name: "set value", input: pgtype.UUID{Bytes: id, Valid: true}, want: &id},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := uuidToPtr(tt.input)
			assertPtrUUIDEqual(t, got, tt.want)
		})
	}
}

func TestPtrToTimestamptz(t *testing.T) {
	t.Parallel()

	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	tests := []struct {
		name  string
		input *time.Time
		want  pgtype.Timestamptz
	}{
		{name: "nil pointer", input: nil, want: pgtype.Timestamptz{}},
		{name: "set value", input: &when, want: pgtype.Timestamptz{Time: when, Valid: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ptrToTimestamptz(tt.input)
			if got != tt.want {
				t.Errorf("ptrToTimestamptz(%v) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestTimeToPtr(t *testing.T) {
	t.Parallel()

	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	tests := []struct {
		name  string
		input pgtype.Timestamptz
		want  *time.Time
	}{
		{name: "not valid", input: pgtype.Timestamptz{Time: when, Valid: false}, want: nil},
		{name: "set value", input: pgtype.Timestamptz{Time: when, Valid: true}, want: &when},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := timeToPtr(tt.input)
			assertPtrTimeEqual(t, got, tt.want)
		})
	}
}

func TestTimeFromTimestamptzAndToTimestamptz(t *testing.T) {
	t.Parallel()

	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	if got := timeFromTimestamptz(toTimestamptz(when)); !got.Equal(when) {
		t.Errorf("timeFromTimestamptz(toTimestamptz(%v)) = %v, want %v", when, got, when)
	}
}

func ptr(s string) *string {
	return &s
}

func assertPtrStringEqual(t *testing.T, got, want *string) {
	t.Helper()

	switch {
	case got == nil && want == nil:
		return
	case got == nil || want == nil:
		t.Errorf("got %v, want %v", got, want)
	case *got != *want:
		t.Errorf("got %q, want %q", *got, *want)
	}
}

func assertPtrUUIDEqual(t *testing.T, got, want *uuid.UUID) {
	t.Helper()

	switch {
	case got == nil && want == nil:
		return
	case got == nil || want == nil:
		t.Errorf("got %v, want %v", got, want)
	case *got != *want:
		t.Errorf("got %v, want %v", *got, *want)
	}
}

func assertPtrTimeEqual(t *testing.T, got, want *time.Time) {
	t.Helper()

	switch {
	case got == nil && want == nil:
		return
	case got == nil || want == nil:
		t.Errorf("got %v, want %v", got, want)
	case !got.Equal(*want):
		t.Errorf("got %v, want %v", *got, *want)
	}
}
