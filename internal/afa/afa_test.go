package afa_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ostendorf-cloud/solobill/internal/afa"
	"github.com/ostendorf-cloud/solobill/internal/category"
)

func day(t *testing.T, s string) time.Time {
	t.Helper()

	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}

	return d
}

func dayPtr(t *testing.T, s string) *time.Time {
	t.Helper()

	d := day(t, s)

	return &d
}

// y builds an expected year: opening, depreciation and closing book value.
func y(year int, opening, dep, closing int64, key category.Key) afa.Year {
	return afa.Year{Year: year, OpeningBookValueCents: opening, DepreciationCents: dep, ClosingBookValueCents: closing, Key: key}
}

const (
	z32 = category.DepreciationIntangible
	z33 = category.DepreciationMovable
	z36 = category.LowValueAssets
	z37 = category.PoolDissolution
)

// TestSchedule checks hand computed schedules. Every expected figure is
// worked out in the comment next to it, not by the code under test.
func TestSchedule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		asset func(t *testing.T) afa.Asset
		want  []afa.Year
	}{
		{
			// 1,200.00 over 36 months from January: 12/36 of 120000 is
			// 40000 in each of three years, nothing pro rata.
			name: "linear, purchase in January",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{AcquiredOn: day(t, "2026-01-15"), CostBasisCents: 120000, Method: afa.Linear, Group: afa.OfficeEquipment, UsefulLifeMonths: 36}
			},
			want: []afa.Year{
				y(2026, 120000, 40000, 80000, z33),
				y(2027, 80000, 40000, 40000, z33),
				y(2028, 40000, 40000, 0, z33),
			},
		},
		{
			// December counts in full: 1/36 of 120000 = 3333.33 -> 3333.
			// Then 40000 a year, and 2029 (January to November, the last
			// 11 months) takes the rest: 120000 - 3333 - 80000 = 36667.
			name: "linear, purchase in December",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{AcquiredOn: day(t, "2026-12-31"), CostBasisCents: 120000, Method: afa.Linear, Group: afa.OtherMovable, UsefulLifeMonths: 36}
			},
			want: []afa.Year{
				y(2026, 120000, 3333, 116667, z33),
				y(2027, 116667, 40000, 76667, z33),
				y(2028, 76667, 40000, 36667, z33),
				y(2029, 36667, 36667, 0, z33),
			},
		},
		{
			// The docs/api.md example, rounded cumulatively. Handed out
			// by the end of each year, of 149900 over 36 months:
			// 2026, 10 months: 41638.89 -> 41639.
			// 2027, 22 months: 91605.56 -> 91606, so 91606 - 41639 = 49967.
			// 2028, 34 months: 141572.22 -> 141572, so 49966.
			// 2029, 36 months: 149900, so 8328.
			// The first year, the one docs/api.md shows, is 41639.
			name: "linear, pro rata from March",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{AcquiredOn: day(t, "2026-03-10"), CostBasisCents: 149900, Method: afa.Linear, Group: afa.OfficeEquipment, UsefulLifeMonths: 36}
			},
			want: []afa.Year{
				y(2026, 149900, 41639, 108261, z33),
				y(2027, 108261, 49967, 58294, z33),
				y(2028, 58294, 49966, 8328, z33),
				y(2029, 8328, 8328, 0, z33),
			},
		},
		{
			// 1.00 over three full years, cumulative: 33.33 -> 33, then
			// 66.67 -> 67 (34 in 2027), then 100 (33 in 2028). Rounding
			// each year on its own would lose a cent (33 + 33 + 33 = 99).
			name: "linear, nothing lost to rounding",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{AcquiredOn: day(t, "2026-01-01"), CostBasisCents: 100, Method: afa.Linear, Group: afa.OfficeEquipment, UsefulLifeMonths: 36}
			},
			want: []afa.Year{
				y(2026, 100, 33, 67, z33),
				y(2027, 67, 34, 33, z33),
				y(2028, 33, 33, 0, z33),
			},
		},
		{
			// 0.30 over 50 years from January: 0.6 cents a year. Checked
			// by the invariants below and TestScheduleTinyBaseEndsAtZero.
			name: "linear, a tiny base never depreciates below 0",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{AcquiredOn: day(t, "2026-01-01"), CostBasisCents: 30, Method: afa.Linear, Group: afa.OfficeEquipment, UsefulLifeMonths: 600}
			},
			want: nil,
		},
		{
			// Computer hardware, 12 months linear (BMF letter of 22
			// February 2022): March to December is 10/12 of 99900 = 83250,
			// January and February 2027 take the rest, 16650.
			name: "computer hardware, 12 months from March",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{AcquiredOn: day(t, "2026-03-05"), CostBasisCents: 99900, Method: afa.Linear, Group: afa.OfficeEquipment, UsefulLifeMonths: afa.ComputerHardwareUsefulLifeMonths}
			},
			want: []afa.Year{
				y(2026, 99900, 83250, 16650, z33),
				y(2027, 16650, 16650, 0, z33),
			},
		},
		{
			// The same laptop with the owner's override of 36 months:
			// 10/36 of 99900 = 27750, 12/36 = 33300, and 2029 (January and
			// February) takes 99900 - 27750 - 2*33300 = 5550.
			name: "computer hardware, owner override to 36 months",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{AcquiredOn: day(t, "2026-03-05"), CostBasisCents: 99900, Method: afa.Linear, Group: afa.OfficeEquipment, UsefulLifeMonths: 36}
			},
			want: []afa.Year{
				y(2026, 99900, 27750, 72150, z33),
				y(2027, 72150, 33300, 38850, z33),
				y(2028, 38850, 33300, 5550, z33),
				y(2029, 5550, 5550, 0, z33),
			},
		},
		{
			// Intangible software over 36 months from January: 9000 a year
			// on Zeile 32.
			name: "linear intangible goes to Zeile 32",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{AcquiredOn: day(t, "2026-01-01"), CostBasisCents: 27000, Method: afa.Linear, Group: afa.Intangible, UsefulLifeMonths: 36}
			},
			want: []afa.Year{
				y(2026, 27000, 9000, 18000, z32),
				y(2027, 18000, 9000, 9000, z32),
				y(2028, 9000, 9000, 0, z32),
			},
		},
		{
			// Disposal in June 2027 ends the schedule. June counts in full:
			// 6/36 of 120000 = 20000. The rest, 60000, stays as the closing
			// book value.
			name: "linear, disposal date ends the schedule",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{
					AcquiredOn: day(t, "2026-01-15"), CostBasisCents: 120000, Method: afa.Linear, Group: afa.OfficeEquipment,
					UsefulLifeMonths: 36, DisposedOn: dayPtr(t, "2027-06-30"),
				}
			},
			want: []afa.Year{
				y(2026, 120000, 40000, 80000, z33),
				y(2027, 80000, 20000, 60000, z33),
			},
		},
		{
			// Bought and disposed in December: one month, 3333.
			name: "linear, disposal in the acquisition month",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{
					AcquiredOn: day(t, "2026-12-01"), CostBasisCents: 120000, Method: afa.Linear, Group: afa.OfficeEquipment,
					UsefulLifeMonths: 36, DisposedOn: dayPtr(t, "2026-12-20"),
				}
			},
			want: []afa.Year{
				y(2026, 120000, 3333, 116667, z33),
			},
		},
		{
			// A disposal after the useful life ended changes nothing.
			name: "linear, disposal after the end",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{
					AcquiredOn: day(t, "2026-01-15"), CostBasisCents: 120000, Method: afa.Linear, Group: afa.OfficeEquipment,
					UsefulLifeMonths: 12, DisposedOn: dayPtr(t, "2030-06-30"),
				}
			},
			want: []afa.Year{
				y(2026, 120000, 120000, 0, z33),
			},
		},
		{
			// Imported: bought July 2024, 36 months, so July 2024 to
			// December 2025 (18 months) are used before the opening year.
			// The opening book value 42000 spreads over the 18 months
			// left: 12/18 of 42000 = 28000 in 2026, the rest, 14000, in
			// 2027 (January to June).
			name: "linear, opening book value of an imported asset",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{
					AcquiredOn: day(t, "2024-07-01"), CostBasisCents: 72000, Method: afa.Linear, Group: afa.OfficeEquipment,
					UsefulLifeMonths: 36, Opening: &afa.Opening{Year: 2026, BookValueCents: 42000},
				}
			},
			want: []afa.Year{
				y(2026, 42000, 28000, 14000, z33),
				y(2027, 14000, 14000, 0, z33),
			},
		},
		{
			// An imported asset that is fully written off has no schedule.
			name: "linear, opening book value 0",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{
					AcquiredOn: day(t, "2020-07-01"), CostBasisCents: 72000, Method: afa.Linear, Group: afa.OfficeEquipment,
					UsefulLifeMonths: 36, Opening: &afa.Opening{Year: 2026, BookValueCents: 0},
				}
			},
			want: []afa.Year{},
		},
		{
			// GWG at exactly 800.00: all of it in the acquisition year,
			// also when bought on 31 December.
			name: "gwg at 800.00 in December",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{AcquiredOn: day(t, "2026-12-31"), CostBasisCents: 80000, Method: afa.LowValue, Group: afa.OfficeEquipment}
			},
			want: []afa.Year{y(2026, 80000, 80000, 0, z36)},
		},
		{
			name: "gwg is not changed by a disposal",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{AcquiredOn: day(t, "2026-01-02"), CostBasisCents: 50000, Method: afa.LowValue, Group: afa.OtherMovable, DisposedOn: dayPtr(t, "2026-02-01")}
			},
			want: []afa.Year{y(2026, 50000, 50000, 0, z36)},
		},
		{
			name: "immediate expense at 250.00",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{AcquiredOn: day(t, "2026-06-01"), CostBasisCents: 25000, Method: afa.Immediate, Group: afa.OfficeEquipment}
			},
			want: []afa.Year{y(2026, 25000, 25000, 0, z36)},
		},
		{
			// Sammelposten at 250.01, cumulative fifths of 25001:
			// 5000.2 -> 5000, 10000.4 -> 10000, 15000.6 -> 15001,
			// 20000.8 -> 20001, 25001. So 5000, 5000, 5001, 5000, 5000.
			// Bought in December, still a full fifth in the first year.
			name: "sammelposten at 250.01 in December",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{AcquiredOn: day(t, "2026-12-31"), CostBasisCents: 25001, Method: afa.Pool, Group: afa.OfficeEquipment}
			},
			want: []afa.Year{
				y(2026, 25001, 5000, 20001, z37),
				y(2027, 20001, 5000, 15001, z37),
				y(2028, 15001, 5001, 10000, z37),
				y(2029, 10000, 5000, 5000, z37),
				y(2030, 5000, 5000, 0, z37),
			},
		},
		{
			// Sammelposten at 1,000.00: 20000 a year for five years. The
			// disposal in 2027 does not reduce the pool.
			name: "sammelposten at 1000.00 with a disposal",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{AcquiredOn: day(t, "2026-01-01"), CostBasisCents: 100000, Method: afa.Pool, Group: afa.OtherMovable, DisposedOn: dayPtr(t, "2027-03-01")}
			},
			want: []afa.Year{
				y(2026, 100000, 20000, 80000, z37),
				y(2027, 80000, 20000, 60000, z37),
				y(2028, 60000, 20000, 40000, z37),
				y(2029, 40000, 20000, 20000, z37),
				y(2030, 20000, 20000, 0, z37),
			},
		},
		{
			// Imported pool item formed in 2024: 2024 and 2025 are used,
			// three years are left. 30000/3 = 10000 each.
			name: "sammelposten, opening book value",
			asset: func(t *testing.T) afa.Asset {
				return afa.Asset{
					AcquiredOn: day(t, "2024-05-01"), CostBasisCents: 50000, Method: afa.Pool, Group: afa.OtherMovable,
					Opening: &afa.Opening{Year: 2026, BookValueCents: 30000},
				}
			},
			want: []afa.Year{
				y(2026, 30000, 10000, 20000, z37),
				y(2027, 20000, 10000, 10000, z37),
				y(2028, 10000, 10000, 0, z37),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := tt.asset(t)
			got, err := afa.Schedule(a)
			if err != nil {
				t.Fatalf("Schedule() error = %v", err)
			}
			if tt.want != nil && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Schedule() =\n%+v\nwant\n%+v", got, tt.want)
			}

			assertScheduleInvariants(t, a, got)
		})
	}
}

// assertScheduleInvariants checks what every schedule must hold: the
// years follow each other, each closes where the next opens, nothing is
// negative, and without a linear disposal the years add up to the base.
func assertScheduleInvariants(t *testing.T, a afa.Asset, years []afa.Year) {
	t.Helper()

	base := a.CostBasisCents
	if a.Opening != nil {
		base = a.Opening.BookValueCents
	}

	var sum int64
	for i, yr := range years {
		if yr.DepreciationCents < 0 || yr.ClosingBookValueCents < 0 {
			t.Errorf("year %d is negative: %+v", yr.Year, yr)
		}
		if yr.OpeningBookValueCents-yr.DepreciationCents != yr.ClosingBookValueCents {
			t.Errorf("year %d does not add up: %+v", yr.Year, yr)
		}
		if i > 0 && (yr.Year != years[i-1].Year+1 || yr.OpeningBookValueCents != years[i-1].ClosingBookValueCents) {
			t.Errorf("year %d does not follow %+v", yr.Year, years[i-1])
		}
		sum += yr.DepreciationCents
	}

	disposedLinear := a.Method == afa.Linear && a.DisposedOn != nil
	if len(years) > 0 && !disposedLinear && sum != base {
		t.Errorf("years add up to %d, want the base %d", sum, base)
	}
	if len(years) > 0 && years[0].OpeningBookValueCents != base {
		t.Errorf("first opening = %d, want the base %d", years[0].OpeningBookValueCents, base)
	}
}

func TestScheduleTinyBaseEndsAtZero(t *testing.T) {
	t.Parallel()

	got, err := afa.Schedule(afa.Asset{AcquiredOn: day(t, "2026-01-01"), CostBasisCents: 30, Method: afa.Linear, Group: afa.OfficeEquipment, UsefulLifeMonths: 600})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 50 {
		t.Fatalf("years = %d, want 50", len(got))
	}
	// Exactly 0.6 cents a year. Cumulative rounding hands out
	// round(0.6 * n) by the end of year n: 1, 1, 2, 2, 3, ... so the
	// years alternate 1, 0, 1, 0, 1 and reach 30 in year 50, not in year
	// 30 as rounding 0.6 up every year would. No year is more than half a
	// cent from its exact share, cumulated.
	var handedOut int64
	for n, yr := range got {
		handedOut += yr.DepreciationCents
		// |handedOut - 0.6 (n+1)| <= 0.5, times 10 to stay in integers.
		if diff := handedOut*10 - 6*int64(n+1); diff > 5 || diff < -5 {
			t.Errorf("year %d: handed out %d, exact %d/10", yr.Year, handedOut, 6*(n+1))
		}
	}
	if got[0].DepreciationCents != 1 || got[1].DepreciationCents != 0 || got[49].ClosingBookValueCents != 0 || handedOut != 30 {
		t.Errorf("first = %+v, second = %+v, last = %+v, sum %d", got[0], got[1], got[49], handedOut)
	}
}

// TestScheduleRunningTotalStaysWithinHalfACent checks cumulative rounding
// over many bases: by the end of every year of a linear schedule from
// January, the running total handed out is within half a cent of
// base * months / life. A single year is only within one cent: in the
// docs/api.md example 2028 is exactly 49966.67 and gets 49966.
func TestScheduleRunningTotalStaysWithinHalfACent(t *testing.T) {
	t.Parallel()

	for _, base := range []int64{1, 7, 100, 149900, 999999} {
		for _, life := range []int{7, 36, 61} {
			got, err := afa.Schedule(afa.Asset{AcquiredOn: day(t, "2026-01-01"), CostBasisCents: base, Method: afa.Linear, Group: afa.OfficeEquipment, UsefulLifeMonths: life})
			if err != nil {
				t.Fatal(err)
			}
			var handedOut int64
			for n, yr := range got {
				handedOut += yr.DepreciationCents
				months := int64(min(12*(n+1), life))
				// 2 * |handedOut * life - base * months| <= life
				diff := 2 * (handedOut*int64(life) - base*months)
				if diff > int64(life) || diff < -int64(life) {
					t.Errorf("base %d, life %d, year %d: handed out %d, exact %d*%d/%d", base, life, yr.Year, handedOut, base, months, life)
				}
			}
		}
	}
}

// TestValidateThresholds checks every bound of docs/concepts/solobill.md,
// "Depreciation scope", one cent on each side.
func TestValidateThresholds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method afa.Method
		cost   int64
		ok     bool
	}{
		{afa.Immediate, 1, true},
		{afa.Immediate, 25000, true},
		{afa.Immediate, 25001, false},
		{afa.LowValue, 25000, true},
		{afa.LowValue, 80000, true},
		{afa.LowValue, 80001, false},
		{afa.Pool, 25000, false},
		{afa.Pool, 25001, true},
		{afa.Pool, 100000, true},
		{afa.Pool, 100001, false},
		{afa.Linear, 1, true},
		{afa.Linear, 100000000, true},
		{afa.Linear, 0, false},
		{afa.LowValue, -1, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.method), func(t *testing.T) {
			t.Parallel()

			a := afa.Asset{AcquiredOn: day(t, "2026-05-01"), CostBasisCents: tt.cost, Method: tt.method, Group: afa.OfficeEquipment}
			if tt.method == afa.Linear {
				a.UsefulLifeMonths = 36
			}

			err := afa.Validate(a)
			if (err == nil) != tt.ok {
				t.Fatalf("Validate(%s, %d) = %v, want ok %v", tt.method, tt.cost, err, tt.ok)
			}
			if err != nil {
				if _, found := err.Problems["cost_basis_cents"]; !found {
					t.Errorf("problems = %v, want cost_basis_cents", err.Problems)
				}
				if _, serr := afa.Schedule(a); serr == nil {
					t.Error("Schedule() accepted an asset Validate refuses")
				}
			}
		})
	}
}

func TestValidateFields(t *testing.T) {
	t.Parallel()

	linear := func(t *testing.T) afa.Asset {
		return afa.Asset{AcquiredOn: day(t, "2026-05-01"), CostBasisCents: 120000, Method: afa.Linear, Group: afa.OfficeEquipment, UsefulLifeMonths: 36}
	}

	tests := []struct {
		name  string
		edit  func(t *testing.T, a *afa.Asset)
		field string
	}{
		{"degressive is no method", func(_ *testing.T, a *afa.Asset) { a.Method = "degressive" }, "method"},
		{"unknown group", func(_ *testing.T, a *afa.Asset) { a.Group = "buildings" }, "group"},
		{"linear without useful life", func(_ *testing.T, a *afa.Asset) { a.UsefulLifeMonths = 0 }, "useful_life_months"},
		{"useful life above 50 years", func(_ *testing.T, a *afa.Asset) { a.UsefulLifeMonths = 601 }, "useful_life_months"},
		{"useful life on a gwg", func(_ *testing.T, a *afa.Asset) { a.Method, a.CostBasisCents = afa.LowValue, 50000 }, "useful_life_months"},
		{"acquired before 1900", func(t *testing.T, a *afa.Asset) { a.AcquiredOn = day(t, "1899-12-31") }, "acquired_on"},
		{"disposal before acquisition", func(t *testing.T, a *afa.Asset) { a.DisposedOn = dayPtr(t, "2026-04-30") }, "disposed_on"},
		{"opening year is the acquisition year", func(_ *testing.T, a *afa.Asset) {
			a.Opening = &afa.Opening{Year: 2026, BookValueCents: 1000}
		}, "opening_book_value_year"},
		{"opening value above cost", func(_ *testing.T, a *afa.Asset) {
			a.Opening = &afa.Opening{Year: 2027, BookValueCents: 120001}
		}, "opening_book_value_cents"},
		{"opening value after the useful life", func(_ *testing.T, a *afa.Asset) {
			a.Opening = &afa.Opening{Year: 2030, BookValueCents: 1}
		}, "opening_book_value_cents"},
		{"opening value of a gwg", func(_ *testing.T, a *afa.Asset) {
			a.Method, a.CostBasisCents, a.UsefulLifeMonths = afa.LowValue, 50000, 0
			a.Opening = &afa.Opening{Year: 2027, BookValueCents: 1}
		}, "opening_book_value_cents"},
		{"opening value of a dissolved pool", func(_ *testing.T, a *afa.Asset) {
			a.Method, a.CostBasisCents, a.UsefulLifeMonths = afa.Pool, 50000, 0
			a.Opening = &afa.Opening{Year: 2031, BookValueCents: 1}
		}, "opening_book_value_cents"},
		{"disposal before the opening year", func(t *testing.T, a *afa.Asset) {
			a.Opening = &afa.Opening{Year: 2027, BookValueCents: 80000}
			a.DisposedOn = dayPtr(t, "2026-12-31")
		}, "disposed_on"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := linear(t)
			tt.edit(t, &a)
			err := afa.Validate(a)
			if err == nil {
				t.Fatalf("Validate() = nil, want a problem on %s", tt.field)
			}
			if _, ok := err.Problems[tt.field]; !ok {
				t.Errorf("problems = %v, want %s", err.Problems, tt.field)
			}

			_, serr := afa.Schedule(a)
			var invalid *afa.InvalidError
			if !errors.As(serr, &invalid) {
				t.Errorf("Schedule() error = %v, want *InvalidError", serr)
			}
		})
	}

	if err := afa.Validate(linear(t)); err != nil {
		t.Errorf("the valid base asset: %v", err)
	}
}

// TestCostBasisAndThresholds: a Kleinunternehmer has no Vorsteuer
// deduction, so the gross price is the cost basis the thresholds apply to;
// under the Regelbesteuerung it is the net price. Not verified with a
// Steuerberater (docs/concepts/solobill.md, "Depreciation scope").
func TestCostBasisAndThresholds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		regime       afa.Regime
		gross, net   int64
		wantBasis    int64
		method       afa.Method
		wantMethodOK bool
	}{
		// 800.00 net plus 19 % VAT is 952.00 gross.
		{"Regelbesteuerung, net 800.00 is a GWG", afa.Standard, 95200, 80000, 80000, afa.LowValue, true},
		{"Kleinunternehmer, gross 952.00 is no GWG", afa.Kleinunternehmer, 95200, 95200, 95200, afa.LowValue, false},
		{"Kleinunternehmer, gross 952.00 is a Sammelposten", afa.Kleinunternehmer, 95200, 95200, 95200, afa.Pool, true},
		// 250.00 net is 297.50 gross.
		{"Regelbesteuerung, net 250.00 is immediate", afa.Standard, 29750, 25000, 25000, afa.Immediate, true},
		{"Kleinunternehmer, gross 297.50 is not immediate", afa.Kleinunternehmer, 29750, 29750, 29750, afa.Immediate, false},
		// The basis is the regime's, even when net and gross differ on a
		// receipt a Kleinunternehmer received.
		{"Kleinunternehmer takes gross over net", afa.Kleinunternehmer, 29750, 25000, 29750, afa.Immediate, false},
		{"Regelbesteuerung takes net over gross", afa.Standard, 29750, 25000, 25000, afa.Pool, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			basis, err := afa.CostBasis(tt.regime, tt.gross, tt.net)
			if err != nil {
				t.Fatal(err)
			}
			if basis != tt.wantBasis {
				t.Errorf("CostBasis() = %d, want %d", basis, tt.wantBasis)
			}

			verr := afa.Validate(afa.Asset{AcquiredOn: day(t, "2026-05-01"), CostBasisCents: basis, Method: tt.method, Group: afa.OfficeEquipment})
			if (verr == nil) != tt.wantMethodOK {
				t.Errorf("Validate(%s, %d) = %v, want ok %v", tt.method, basis, verr, tt.wantMethodOK)
			}
		})
	}

	for _, bad := range []struct {
		regime     afa.Regime
		gross, net int64
	}{
		{afa.Standard, 100, 101},
		{afa.Standard, 100, -1},
		{"flat", 100, 100},
	} {
		if _, err := afa.CostBasis(bad.regime, bad.gross, bad.net); err == nil {
			t.Errorf("CostBasis(%q, %d, %d) = nil error", bad.regime, bad.gross, bad.net)
		}
	}
}

func TestFirstScheduleYear(t *testing.T) {
	t.Parallel()

	a := afa.Asset{AcquiredOn: day(t, "2024-07-01")}
	if got := afa.FirstScheduleYear(a); got != 2024 {
		t.Errorf("acquired = %d, want 2024", got)
	}
	a.Opening = &afa.Opening{Year: 2026}
	if got := afa.FirstScheduleYear(a); got != 2026 {
		t.Errorf("imported = %d, want 2026", got)
	}
}

// TestNoDegressiveMethod pins the owner's decision: exactly four methods.
func TestNoDegressiveMethod(t *testing.T) {
	t.Parallel()

	want := []afa.Method{afa.Linear, afa.LowValue, afa.Pool, afa.Immediate}
	if got := afa.Methods(); !reflect.DeepEqual(got, want) {
		t.Errorf("Methods() = %v, want %v", got, want)
	}
	for _, s := range []string{"degressive", "declining_balance", "LINEAR", ""} {
		if _, ok := afa.ParseMethod(s); ok {
			t.Errorf("ParseMethod(%q) accepted", s)
		}
	}
	if _, ok := afa.ParseGroup("office_equipment"); !ok {
		t.Error("ParseGroup(office_equipment) refused")
	}
}
