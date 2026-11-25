package afa_test

import (
	"errors"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/afa"
)

// noRest marks a case whose DisposalRestCents must be nil.
const noRest int64 = -1

func TestCurrentBookValue(t *testing.T) {
	t.Parallel()

	// docs/api.md's laptop: 149900 over 36 months from March 2026, closing
	// 108261, 58294, 8328 and 0 (TestSchedule).
	laptop := afa.Asset{
		AcquiredOn: day(t, "2026-03-10"), CostBasisCents: 149900,
		Method: afa.Linear, Group: afa.OfficeEquipment, UsefulLifeMonths: 36,
	}
	gwg := afa.Asset{AcquiredOn: day(t, "2026-05-01"), CostBasisCents: 80000, Method: afa.LowValue, Group: afa.OfficeEquipment}
	gwgDisposed := gwg
	gwgDisposed.DisposedOn = dayPtr(t, "2027-02-01")
	immediate := afa.Asset{AcquiredOn: day(t, "2026-05-01"), CostBasisCents: 25000, Method: afa.Immediate, Group: afa.OtherMovable}
	pool := afa.Asset{AcquiredOn: day(t, "2026-05-01"), CostBasisCents: 100000, Method: afa.Pool, Group: afa.OfficeEquipment}
	// A disposal does not reduce the pool (section 6 Abs. 2a Satz 3 EStG):
	// the value keeps following the schedule and there is no rest.
	poolDisposed := pool
	poolDisposed.DisposedOn = dayPtr(t, "2027-06-01")
	// 120000 over 36 months from January 2025, disposed in January 2026:
	// 40000 in 2025, then 13/36 handed out, 76667 left (TestAssetsDisposal).
	disposed := afa.Asset{
		AcquiredOn: day(t, "2025-01-15"), CostBasisCents: 120000,
		Method: afa.Linear, Group: afa.OfficeEquipment, UsefulLifeMonths: 36,
		DisposedOn: dayPtr(t, "2026-01-31"),
	}
	// 42000 at 1 January 2026 over the 18 months left: 28000 and 14000.
	imported := afa.Asset{
		AcquiredOn: day(t, "2024-07-01"), CostBasisCents: 72000,
		Method: afa.Linear, Group: afa.OfficeEquipment, UsefulLifeMonths: 36,
		Opening: &afa.Opening{Year: 2026, BookValueCents: 42000},
	}
	// Disposed in March 2026: 3 of the 18 months, 7000 handed out.
	importedDisposed := imported
	importedDisposed.DisposedOn = dayPtr(t, "2026-03-15")
	importedZero := afa.Asset{
		AcquiredOn: day(t, "2024-07-01"), CostBasisCents: 60000, Method: afa.LowValue, Group: afa.OfficeEquipment,
		Opening: &afa.Opening{Year: 2025, BookValueCents: 0},
	}

	tests := []struct {
		name        string
		input       afa.Asset
		currentYear int
		wantCents   int64
		wantYear    int
		wantRest    int64
	}{
		{"linear in the acquisition year", laptop, 2026, 149900, 0, noRest},
		{"linear before the acquisition year", laptop, 2025, 149900, 0, noRest},
		{"linear after the first year", laptop, 2027, 108261, 2026, noRest},
		{"linear in the last year", laptop, 2029, 8328, 2028, noRest},
		{"linear fully depreciated", laptop, 2030, 0, 2029, noRest},
		{"linear long after the useful life", laptop, 2040, 0, 2029, noRest},
		{"gwg in the acquisition year", gwg, 2026, 80000, 0, noRest},
		{"gwg after the acquisition year", gwg, 2027, 0, 2026, noRest},
		{"gwg disposed after its year has no rest", gwgDisposed, 2027, 0, 2027, 0},
		{"immediate after the acquisition year", immediate, 2027, 0, 2026, noRest},
		{"sammelposten in the year of forming", pool, 2026, 100000, 0, noRest},
		{"sammelposten in the third year", pool, 2028, 60000, 2027, noRest},
		{"sammelposten dissolved", pool, 2035, 0, 2030, noRest},
		{"sammelposten disposal changes nothing", poolDisposed, 2029, 40000, 2028, noRest},
		{"disposed in the open year", disposed, 2026, 0, 2026, 76667},
		{"disposal year ended", disposed, 2027, 0, 2026, 76667},
		{"disposed long ago", disposed, 2031, 0, 2026, 76667},
		{"disposal after the current year", disposed, 2025, 120000, 0, 76667},
		{"imported disposed in its opening year", importedDisposed, 2026, 0, 2026, 35000},
		{"imported in its opening year", imported, 2026, 42000, 0, noRest},
		{"imported after its opening year", imported, 2027, 14000, 2026, noRest},
		{"imported fully depreciated", imported, 2028, 0, 2027, noRest},
		{"imported with an opening value of 0", importedZero, 2026, 0, 0, noRest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := afa.CurrentBookValue(tt.input, tt.currentYear)
			if err != nil {
				t.Fatalf("CurrentBookValue() error = %v", err)
			}
			if got.Cents != tt.wantCents || got.Year != tt.wantYear {
				t.Errorf("CurrentBookValue(%d) = %d in %d, want %d in %d", tt.currentYear, got.Cents, got.Year, tt.wantCents, tt.wantYear)
			}
			switch {
			case tt.wantRest == noRest && got.DisposalRestCents != nil:
				t.Errorf("DisposalRestCents = %d, want nil", *got.DisposalRestCents)
			case tt.wantRest != noRest && got.DisposalRestCents == nil:
				t.Errorf("DisposalRestCents = nil, want %d", tt.wantRest)
			case tt.wantRest != noRest && *got.DisposalRestCents != tt.wantRest:
				t.Errorf("DisposalRestCents = %d, want %d", *got.DisposalRestCents, tt.wantRest)
			}
		})
	}
}

func TestCurrentBookValueRefusesAnInvalidAsset(t *testing.T) {
	t.Parallel()

	_, err := afa.CurrentBookValue(afa.Asset{AcquiredOn: day(t, "2026-05-01"), CostBasisCents: 80001, Method: afa.LowValue, Group: afa.OfficeEquipment}, 2027)

	var invalid *afa.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v, want *afa.InvalidError", err)
	}
	if _, ok := invalid.Problems["cost_basis_cents"]; !ok {
		t.Errorf("problems = %v, want cost_basis_cents", invalid.Problems)
	}
}
