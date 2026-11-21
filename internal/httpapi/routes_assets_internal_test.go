package httpapi

import (
	"testing"
	"time"
)

// TestAssetDateRulesUseTheGermanCalendar pins the clock of the asset
// rules at fixed instants: the year of the PUT rule, "not in the future"
// and the disposal rule all follow the date in Berlin, not in UTC.
func TestAssetDateRulesUseTheGermanCalendar(t *testing.T) {
	t.Parallel()

	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	day := func(s string) time.Time {
		d, err := time.Parse(time.DateOnly, s)
		if err != nil {
			t.Fatal(err)
		}

		return d
	}

	tests := []struct {
		name        string
		now         time.Time
		disposedOn  string
		wantToday   string
		wantProblem bool
		wantEnded   bool
	}{
		{"2024 is refused in 2026", time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), "2024-05-01", "2026-06-01", false, true},
		{"last day of last year is refused", time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), "2025-12-31", "2026-06-01", false, true},
		{"the current year is accepted", time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), "2026-03-01", "2026-06-01", false, false},
		{"today is accepted", time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), "2026-06-01", "2026-06-01", false, false},
		{"tomorrow is in the future", time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), "2026-06-02", "2026-06-01", true, false},
		// 23:30 UTC on 31 December is 00:30 on 1 January in Berlin.
		{"31 Dec 23:30 UTC: 2026 has ended", time.Date(2026, 12, 31, 23, 30, 0, 0, time.UTC), "2026-12-31", "2027-01-01", false, true},
		{"31 Dec 23:30 UTC: 1 Jan 2027 is today", time.Date(2026, 12, 31, 23, 30, 0, 0, time.UTC), "2027-01-01", "2027-01-01", false, false},
		{"1 Jan 00:30 Berlin: 2026 has ended", time.Date(2027, 1, 1, 0, 30, 0, 0, berlin), "2026-12-31", "2027-01-01", false, true},
		{"1 Jan 00:30 Berlin: today is accepted", time.Date(2027, 1, 1, 0, 30, 0, 0, berlin), "2027-01-01", "2027-01-01", false, false},
		// 22:30 UTC on 31 December is 23:30 in Berlin: still 2026.
		{"31 Dec 22:30 UTC: 2026 is open", time.Date(2026, 12, 31, 22, 30, 0, 0, time.UTC), "2026-12-31", "2026-12-31", false, false},
		{"31 Dec 22:30 UTC: 1 Jan 2027 is in the future", time.Date(2026, 12, 31, 22, 30, 0, 0, time.UTC), "2027-01-01", "2026-12-31", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := assetToday(tt.now).Format(time.DateOnly); got != tt.wantToday {
				t.Errorf("assetToday() = %s, want %s", got, tt.wantToday)
			}
			problem, ended := disposalDateRule(day(tt.disposedOn), tt.now)
			if (problem != "") != tt.wantProblem || ended != tt.wantEnded {
				t.Errorf("disposalDateRule(%s) = %q, %v, want problem %v, ended %v", tt.disposedOn, problem, ended, tt.wantProblem, tt.wantEnded)
			}
		})
	}
}

// TestAssetRequestRefusesAFutureAcquisitionInBerlinTime: at 23:30 UTC on
// 31 December an acquisition on 1 January is today in Germany, so it is
// accepted; an hour earlier it is still tomorrow.
func TestAssetRequestRefusesAFutureAcquisitionInBerlinTime(t *testing.T) {
	t.Parallel()

	life := int32(36)
	req := assetRequest{
		Name: "Laptop", AcquiredOn: "2027-01-01", CostBasisCents: 149900, Method: "linear",
		Group: "office_equipment", UsefulLifeMonths: &life,
	}
	if _, err := req.asset(assetToday(time.Date(2026, 12, 31, 23, 30, 0, 0, time.UTC))); err != nil {
		t.Errorf("at 00:30 Berlin on 1 January: %v", err)
	}
	if _, err := req.asset(assetToday(time.Date(2026, 12, 31, 22, 30, 0, 0, time.UTC))); err == nil {
		t.Error("at 23:30 Berlin on 31 December: accepted an acquisition tomorrow")
	}
}
