package afa_test

import (
	"errors"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/afa"
)

func TestCurrentBookValue(t *testing.T) {
	t.Parallel()

	// docs/api.md's laptop: 149900 over 36 months from March 2026, closing
	// 108261, 58294, 8328 and 0 (TestSchedule).
	laptop := afa.Asset{
		AcquiredOn: day(t, "2026-03-10"), CostBasisCents: 149900,
		Method: afa.Linear, Group: afa.OfficeEquipment, UsefulLifeMonths: 36,
	}
	gwg := afa.Asset{AcquiredOn: day(t, "2026-05-01"), CostBasisCents: 80000, Method: afa.LowValue, Group: afa.OfficeEquipment}
	immediate := afa.Asset{AcquiredOn: day(t, "2026-05-01"), CostBasisCents: 25000, Method: afa.Immediate, Group: afa.OtherMovable}
	pool := afa.Asset{AcquiredOn: day(t, "2026-05-01"), CostBasisCents: 100000, Method: afa.Pool, Group: afa.OfficeEquipment}
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
	importedZero := afa.Asset{
		AcquiredOn: day(t, "2024-07-01"), CostBasisCents: 60000, Method: afa.LowValue, Group: afa.OfficeEquipment,
		Opening: &afa.Opening{Year: 2025, BookValueCents: 0},
	}

	tests := []struct {
		name        string
		input       afa.Asset
		currentYear int
		want        afa.BookValue
	}{
		{"linear in the acquisition year", laptop, 2026, afa.BookValue{Cents: 149900}},
		{"linear before the acquisition year", laptop, 2025, afa.BookValue{Cents: 149900}},
		{"linear after the first year", laptop, 2027, afa.BookValue{Cents: 108261, Year: 2026}},
		{"linear in the last year", laptop, 2029, afa.BookValue{Cents: 8328, Year: 2028}},
		{"linear fully depreciated", laptop, 2030, afa.BookValue{Cents: 0, Year: 2029}},
		{"linear long after the useful life", laptop, 2040, afa.BookValue{Cents: 0, Year: 2029}},
		{"gwg in the acquisition year", gwg, 2026, afa.BookValue{Cents: 80000}},
		{"gwg after the acquisition year", gwg, 2027, afa.BookValue{Cents: 0, Year: 2026}},
		{"immediate after the acquisition year", immediate, 2027, afa.BookValue{Cents: 0, Year: 2026}},
		{"sammelposten in the year of forming", pool, 2026, afa.BookValue{Cents: 100000}},
		{"sammelposten in the third year", pool, 2028, afa.BookValue{Cents: 60000, Year: 2027}},
		{"sammelposten dissolved", pool, 2035, afa.BookValue{Cents: 0, Year: 2030}},
		{"sammelposten disposal changes nothing", poolDisposed, 2029, afa.BookValue{Cents: 40000, Year: 2028}},
		{"disposal year still open", disposed, 2026, afa.BookValue{Cents: 80000, Year: 2025}},
		{"disposal year ended keeps the rest", disposed, 2027, afa.BookValue{Cents: 76667, Year: 2026}},
		{"disposed long ago", disposed, 2031, afa.BookValue{Cents: 76667, Year: 2026}},
		{"imported in its opening year", imported, 2026, afa.BookValue{Cents: 42000}},
		{"imported after its opening year", imported, 2027, afa.BookValue{Cents: 14000, Year: 2026}},
		{"imported fully depreciated", imported, 2028, afa.BookValue{Cents: 0, Year: 2027}},
		{"imported with an opening value of 0", importedZero, 2026, afa.BookValue{Cents: 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := afa.CurrentBookValue(tt.input, tt.currentYear)
			if err != nil {
				t.Fatalf("CurrentBookValue() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("CurrentBookValue(%d) = %+v, want %+v", tt.currentYear, got, tt.want)
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
