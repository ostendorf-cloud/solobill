package postgres

import (
	"testing"
	"time"
)

// TestYearRange pins the half open range the list queries filter on: 31
// December belongs to its year, 1 January of the next year does not.
func TestYearRange(t *testing.T) {
	t.Parallel()

	from, before := yearRange(2026)
	if !from.Valid || !before.Valid {
		t.Fatalf("yearRange(2026) = %v, %v, want both valid", from, before)
	}
	if got := from.Time.Format(time.DateOnly); got != "2026-01-01" {
		t.Errorf("from = %s, want 2026-01-01", got)
	}
	if got := before.Time.Format(time.DateOnly); got != "2027-01-01" {
		t.Errorf("before = %s, want 2027-01-01 (exclusive)", got)
	}

	inRange := func(day string) bool {
		d, err := time.Parse(time.DateOnly, day)
		if err != nil {
			t.Fatal(err)
		}

		return !d.Before(from.Time) && d.Before(before.Time)
	}
	for day, want := range map[string]bool{
		"2025-12-31": false,
		"2026-01-01": true,
		"2026-12-31": true,
		"2027-01-01": false,
	} {
		if got := inRange(day); got != want {
			t.Errorf("%s in 2026 = %v, want %v", day, got, want)
		}
	}
}
