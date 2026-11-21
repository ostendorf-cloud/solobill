// Package afa computes the yearly depreciation (Absetzung für Abnutzung)
// of one asset of the register, per docs/concepts/solobill.md,
// "Depreciation scope": linear by month, GWG, Sammelposten and the
// immediate expense of an item up to 250 EUR. There is no degressive
// method, on purpose: Method has exactly four values and nothing else
// parses.
//
// Pure calculation, no database, no clock, no I/O. Every amount is an
// integer cent and every share is computed in exact rational arithmetic
// (math/big), never a float. Rounding is cumulative, half away from
// zero: the total handed out up to the end of each year is rounded once,
// and a year gets the difference to the year before. Every year is then
// within half a cent of its exact share, and the years always add up to
// the depreciation base: nothing is lost to rounding and nothing is
// invented.
//
// Like internal/money, this package imports nothing from internal/storage:
// its Method, Group and Regime mirror the values of storage.Asset and
// storage.VATRegime, and the caller converts.
//
// Every figure is a computation over the owner's own entries, not tax
// advice. The points marked "not verified" below are listed in
// docs/concepts/solobill.md, "Unverified tax points".
package afa

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/ostendorf-cloud/solobill/internal/category"
)

// Method is how an asset's cost becomes an expense.
type Method string

const (
	// Linear spreads the depreciation base evenly over the useful life in
	// months. The acquisition month counts in full (section 7 Abs. 1
	// Satz 4 EStG).
	Linear Method = "linear"
	// LowValue is a GWG under section 6 Abs. 2 EStG: the full cost in the
	// acquisition year, for a cost basis up to LowValueLimitCents.
	LowValue Method = "gwg"
	// Pool is the Sammelposten under section 6 Abs. 2a EStG: one fifth in
	// the acquisition year and in each of the four following years, for a
	// cost basis above PoolLowerBoundCents up to PoolUpperLimitCents. A
	// disposal does not reduce the pool.
	Pool Method = "sammelposten"
	// Immediate is an item up to ImmediateLimitCents expensed at once.
	Immediate Method = "immediate"
)

// Methods is every method, in the order of docs/api.md. There is no
// degressive method (docs/concepts/solobill.md, "Decisions of the
// owner").
func Methods() []Method {
	return []Method{Linear, LowValue, Pool, Immediate}
}

// ParseMethod returns the Method named s, and false for anything else,
// "degressive" included.
func ParseMethod(s string) (Method, bool) {
	for _, m := range Methods() {
		if string(m) == s {
			return m, true
		}
	}

	return "", false
}

// Group is the Anlage AVEÜR group an asset is reported in.
type Group string

const (
	// Intangible is an immaterielles Wirtschaftsgut. Its linear
	// depreciation goes to category.DepreciationIntangible.
	Intangible Group = "intangible"
	// OfficeEquipment is Büroausstattung.
	OfficeEquipment Group = "office_equipment"
	// OtherMovable is an anderes bewegliches Wirtschaftsgut.
	OtherMovable Group = "other_movable"
)

// ParseGroup returns the Group named s, and false for anything else.
func ParseGroup(s string) (Group, bool) {
	switch g := Group(s); g {
	case Intangible, OfficeEquipment, OtherMovable:
		return g, true
	default:
		return "", false
	}
}

// Regime is the VAT regime that decides the cost basis, mirroring
// storage.VATRegime.
type Regime string

const (
	// Standard is the Regelbesteuerung: the deductible Vorsteuer is not
	// part of the cost (section 9b Abs. 1 EStG).
	Standard Regime = "standard"
	// Kleinunternehmer is section 19 UStG: no Vorsteuer deduction.
	Kleinunternehmer Regime = "kleinunternehmer"
)

// Thresholds and limits, all in cents or months.
const (
	// ImmediateLimitCents is the highest cost basis expensed at once.
	ImmediateLimitCents int64 = 25000
	// LowValueLimitCents is the highest cost basis of a GWG (section 6
	// Abs. 2 Satz 1 EStG: 800 EUR).
	LowValueLimitCents int64 = 80000
	// PoolLowerBoundCents is exclusive: a Sammelposten item costs more
	// than 250 EUR (section 6 Abs. 2a Satz 1 EStG).
	PoolLowerBoundCents int64 = 25000
	// PoolUpperLimitCents is inclusive: a Sammelposten item costs at most
	// 1,000 EUR.
	PoolUpperLimitCents int64 = 100000
	// PoolYears is the life of a Sammelposten: the year of forming and
	// the four following years.
	PoolYears = 5
	// MaxUsefulLifeMonths bounds the owner's entry: 50 years.
	MaxUsefulLifeMonths = 600
	// ComputerHardwareUsefulLifeMonths is the one year useful life of the
	// BMF letter of 22 February 2022 for computer hardware and software,
	// modelled as 12 months linear. The acquisition year is then pro rata
	// like any other linear asset. Not verified: whether the letter
	// allows the full amount in the acquisition year instead. The owner
	// overrides it by entering another useful life on the asset.
	ComputerHardwareUsefulLifeMonths = 12

	minYear = 1900
	maxYear = 9999
)

// CostBasis is the amount the thresholds and the depreciation apply to:
// the net price under the Regelbesteuerung, the gross price for a
// Kleinunternehmer, who has no Vorsteuer deduction.
//
// Not verified: the Kleinunternehmer reading of section 6 Abs. 2 and
// section 9b EStG is an interpretation the owner confirms with a
// Steuerberater (docs/concepts/solobill.md, "Depreciation scope").
func CostBasis(regime Regime, grossCents, netCents int64) (int64, error) {
	if netCents < 0 || grossCents < netCents {
		return 0, fmt.Errorf("afa: gross %d and net %d cents: need 0 <= net <= gross", grossCents, netCents)
	}

	switch regime {
	case Kleinunternehmer:
		return grossCents, nil
	case Standard:
		return netCents, nil
	default:
		return 0, fmt.Errorf("afa: unknown VAT regime %q", regime)
	}
}

// Opening is the book value of an imported asset at 1 January of Year.
// The schedule continues from it instead of recomputing earlier years.
type Opening struct {
	Year           int
	BookValueCents int64
}

// Asset is what a schedule is computed from. Dates are calendar dates:
// only their year and month are read.
type Asset struct {
	AcquiredOn     time.Time
	CostBasisCents int64
	Method         Method
	Group          Group
	// UsefulLifeMonths is set for Linear and 0 for every other method.
	UsefulLifeMonths int
	// Opening is nil unless the asset was brought in with a book value.
	Opening *Opening
	// DisposedOn ends a linear schedule: the disposal month counts in
	// full. It does not change a GWG, an immediate expense or a
	// Sammelposten.
	DisposedOn *time.Time
}

// Year is one calendar year of a schedule.
type Year struct {
	Year                  int
	OpeningBookValueCents int64
	DepreciationCents     int64
	ClosingBookValueCents int64
	// Key is the EÜR line the depreciation is reported on.
	Key category.Key
}

// InvalidError is an asset Validate refuses. Problems maps the field of
// docs/api.md's asset JSON to the reason.
type InvalidError struct {
	Problems map[string]string
}

func (e *InvalidError) Error() string {
	fields := make([]string, 0, len(e.Problems))
	for f := range e.Problems {
		fields = append(fields, f)
	}
	sort.Strings(fields)

	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = f + ": " + e.Problems[f]
	}

	return "afa: invalid asset: " + strings.Join(parts, ", ")
}

// FirstScheduleYear is the first year the schedule holds a figure for:
// the opening year of an imported asset, the acquisition year
// otherwise. Once it has ended, a figure may already be in a filed EÜR,
// so the asset is no longer edited (docs/concepts/solobill.md,
// "Depreciation scope").
func FirstScheduleYear(a Asset) int {
	if a.Opening != nil {
		return a.Opening.Year
	}

	return a.AcquiredOn.Year()
}

// Key is the EÜR line the asset's depreciation is reported on.
//
// Not verified: an immediate expense up to 250 EUR goes to the GWG line
// (section 6 Abs. 2 EStG, whose Satz 4 waives only the register for
// such an item), and a GWG or Sammelposten of the intangible group goes
// to the same lines as a movable one.
func Key(a Asset) category.Key {
	switch a.Method {
	case LowValue, Immediate:
		return category.LowValueAssets
	case Pool:
		return category.PoolDissolution
	default:
		if a.Group == Intangible {
			return category.DepreciationIntangible
		}

		return category.DepreciationMovable
	}
}

// Validate checks every rule of the asset, and returns nil when it holds.
func Validate(a Asset) *InvalidError {
	p := map[string]string{}

	acqYear := a.AcquiredOn.Year()
	if acqYear < minYear || acqYear > maxYear {
		p["acquired_on"] = "must be a date between 1900 and 9999"
	}

	_, methodOK := ParseMethod(string(a.Method))
	if !methodOK {
		p["method"] = "must be linear, gwg, sammelposten or immediate"
	}
	if _, ok := ParseGroup(string(a.Group)); !ok {
		p["group"] = "must be intangible, office_equipment or other_movable"
	}

	if a.CostBasisCents <= 0 {
		p["cost_basis_cents"] = "must be positive"
	} else if methodOK {
		if reason := fitsCost(a.Method, a.CostBasisCents); reason != "" {
			p["cost_basis_cents"] = reason
		}
	}

	switch {
	case a.Method == Linear && (a.UsefulLifeMonths < 1 || a.UsefulLifeMonths > MaxUsefulLifeMonths):
		p["useful_life_months"] = fmt.Sprintf("required for linear, from 1 to %d", MaxUsefulLifeMonths)
	case a.Method != Linear && a.UsefulLifeMonths != 0:
		p["useful_life_months"] = "only allowed for linear"
	}

	if len(p) == 0 && a.Opening != nil {
		validateOpening(a, p)
	}

	if a.DisposedOn != nil {
		switch {
		case dateBefore(*a.DisposedOn, a.AcquiredOn):
			p["disposed_on"] = "must not be before acquired_on"
		case a.Opening != nil && a.DisposedOn.Year() < a.Opening.Year:
			p["disposed_on"] = "must not be before opening_book_value_year"
		case a.DisposedOn.Year() > maxYear:
			p["disposed_on"] = "must be a date before 10000"
		}
	}

	if len(p) == 0 {
		return nil
	}

	return &InvalidError{Problems: p}
}

// fitsCost is the reason the cost basis does not fit the method, or "".
func fitsCost(m Method, cost int64) string {
	switch m {
	case Immediate:
		if cost > ImmediateLimitCents {
			return "immediate is only allowed up to 250.00 EUR"
		}
	case LowValue:
		if cost > LowValueLimitCents {
			return "gwg is only allowed up to 800.00 EUR"
		}
	case Pool:
		if cost <= PoolLowerBoundCents || cost > PoolUpperLimitCents {
			return "sammelposten is only allowed above 250.00 EUR up to 1,000.00 EUR"
		}
	case Linear:
	}

	return ""
}

// validateOpening checks an imported asset's opening book value. a has
// passed every other check.
func validateOpening(a Asset, p map[string]string) {
	o := a.Opening
	acqYear := a.AcquiredOn.Year()

	if o.Year <= acqYear || o.Year > maxYear {
		p["opening_book_value_year"] = "must be after the year of acquired_on"

		return
	}
	if o.BookValueCents < 0 || o.BookValueCents > a.CostBasisCents {
		p["opening_book_value_cents"] = "must be from 0 to cost_basis_cents"

		return
	}
	if o.BookValueCents == 0 {
		return
	}

	switch a.Method {
	case LowValue, Immediate:
		p["opening_book_value_cents"] = "must be 0: the cost was deducted in the acquisition year"
	case Pool:
		if poolYearsLeft(a) <= 0 {
			p["opening_book_value_cents"] = "must be 0: the pool was dissolved before this year"
		}
	case Linear:
		if linearMonthsLeft(a) <= 0 {
			p["opening_book_value_cents"] = "must be 0: the useful life ended before this year"
		}
	}
}

// Schedule computes the yearly depreciation of a. It returns an
// *InvalidError for an asset Validate refuses. An imported asset with an
// opening book value of 0 has an empty schedule.
func Schedule(a Asset) ([]Year, error) {
	if err := Validate(a); err != nil {
		return nil, err
	}

	key := Key(a)

	switch a.Method {
	case LowValue, Immediate:
		if a.Opening != nil {
			// Validate allows only a book value of 0 here.
			return []Year{}, nil
		}

		return []Year{{
			Year:                  a.AcquiredOn.Year(),
			OpeningBookValueCents: a.CostBasisCents,
			DepreciationCents:     a.CostBasisCents,
			ClosingBookValueCents: 0,
			Key:                   key,
		}}, nil
	case Pool:
		return poolSchedule(a, key), nil
	case Linear:
		return linearSchedule(a, key), nil
	}

	return nil, errors.New("afa: unreachable method")
}

// monthIndex counts months from year 0, so two dates compare and
// subtract as plain integers.
func monthIndex(t time.Time) int {
	return t.Year()*12 + int(t.Month()) - 1
}

func dateBefore(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	if ay != by {
		return ay < by
	}
	if am != bm {
		return am < bm
	}

	return ad < bd
}

// linearMonthsLeft is the useful life left at 1 January of the opening
// year: the life minus the months from the acquisition month (in full)
// to December of the year before.
func linearMonthsLeft(a Asset) int {
	used := a.Opening.Year*12 - monthIndex(a.AcquiredOn)

	return a.UsefulLifeMonths - used
}

// poolYearsLeft is the Sammelposten years left from the opening year on.
func poolYearsLeft(a Asset) int {
	return PoolYears - (a.Opening.Year - a.AcquiredOn.Year())
}

// linearSchedule spreads base over months months from the month with
// index start. Rounding is cumulative: the depreciation handed out up to
// the end of a year is base * months so far / months, rounded once, and
// the year gets that minus what earlier years already got. So every
// year is within half a cent of its exact share, and the year holding
// the last month of the useful life ends at exactly base. A disposal
// ends the schedule in its month, which counts in full, and leaves the
// rest as the closing book value (not verified: R 7.4 Abs. 8 EStR is
// read as counting the disposal month in full).
func linearSchedule(a Asset, key category.Key) []Year {
	base := a.CostBasisCents
	start := monthIndex(a.AcquiredOn)
	months := a.UsefulLifeMonths
	if a.Opening != nil {
		base = a.Opening.BookValueCents
		start = a.Opening.Year * 12
		months = linearMonthsLeft(a)
	}
	if base == 0 {
		return []Year{}
	}

	end := start + months - 1
	stop := end
	if a.DisposedOn != nil {
		stop = min(stop, monthIndex(*a.DisposedOn))
	}

	years := []Year{}
	var handedOut int64
	for y := start / 12; y <= stop/12; y++ {
		last := min(stop, y*12+11)
		cumulative := share(base, int64(last-start+1), int64(months))
		years = append(years, Year{
			Year:                  y,
			OpeningBookValueCents: base - handedOut,
			DepreciationCents:     cumulative - handedOut,
			ClosingBookValueCents: base - cumulative,
			Key:                   key,
		})
		handedOut = cumulative
	}

	return years
}

// poolSchedule spreads base evenly over the pool years left, rounded
// cumulatively like linearSchedule, so the last year ends at exactly
// base. A disposal does not change it (section 6 Abs. 2a Satz 3 EStG).
func poolSchedule(a Asset, key category.Key) []Year {
	base := a.CostBasisCents
	first := a.AcquiredOn.Year()
	count := PoolYears
	if a.Opening != nil {
		base = a.Opening.BookValueCents
		first = a.Opening.Year
		count = poolYearsLeft(a)
	}
	if base == 0 {
		return []Year{}
	}

	years := make([]Year, 0, count)
	var handedOut int64
	for i := range count {
		cumulative := share(base, int64(i+1), int64(count))
		years = append(years, Year{
			Year:                  first + i,
			OpeningBookValueCents: base - handedOut,
			DepreciationCents:     cumulative - handedOut,
			ClosingBookValueCents: base - cumulative,
			Key:                   key,
		})
		handedOut = cumulative
	}

	return years
}

// share is base * num / den rounded half away from zero, in exact
// arithmetic. base, num and den are not negative and den is positive.
func share(base, num, den int64) int64 {
	r := new(big.Rat).SetFrac(
		new(big.Int).Mul(big.NewInt(base), big.NewInt(num)),
		big.NewInt(den),
	)

	q, rem := new(big.Int).QuoRem(r.Num(), r.Denom(), new(big.Int))
	if new(big.Int).Lsh(rem, 1).Cmp(r.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}

	return q.Int64()
}
