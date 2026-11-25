package afa

// BookValue is the current book value of an asset, as docs/api.md,
// "Assets", defines it.
type BookValue struct {
	Cents int64
	// Year is the schedule year whose closing book value Cents is, or the
	// disposal year of a disposed asset. It is 0 when no schedule year has
	// ended yet: Cents is then the book value before any depreciation, the
	// cost basis or, for an imported asset, the opening book value.
	Year int
	// DisposalRestCents is the book value left at the end of the disposal
	// year, nil unless the asset is disposed and not a Sammelposten.
	DisposalRestCents *int64
}

// CurrentBookValue is the book value of a at the end of the last schedule
// year before currentYear: the closing book value of that year. While no
// schedule year has ended, it is the value the schedule starts from.
//
// A disposal of a linear asset, a GWG or an immediate expense takes the
// asset out of the register: from the disposal year on the value is 0,
// Year is the disposal year and DisposalRestCents holds the rest the
// disposal year expenses (section 4 Abs. 3 Satz 4 EStG, not verified with
// a Steuerberater). A Sammelposten is not reduced by a disposal (section 6
// Abs. 2a Satz 3 EStG), so its value follows the schedule and it has no
// rest.
//
// afa has no clock. The caller passes currentYear, the calendar year in
// Germany at its own clock, so the year an EÜR is filed for decides. It
// returns an *InvalidError for an asset Validate refuses.
func CurrentBookValue(a Asset, currentYear int) (BookValue, error) {
	years, err := Schedule(a)
	if err != nil {
		return BookValue{}, err
	}

	start := a.CostBasisCents
	if a.Opening != nil {
		start = a.Opening.BookValueCents
	}

	if a.DisposedOn != nil && a.Method != Pool {
		disposalYear := a.DisposedOn.Year()
		rest := valueAtEndOf(years, disposalYear, start)
		if disposalYear <= currentYear {
			return BookValue{Cents: 0, Year: disposalYear, DisposalRestCents: &rest}, nil
		}
		v := endedValue(years, currentYear, start)
		v.DisposalRestCents = &rest

		return v, nil
	}

	return endedValue(years, currentYear, start), nil
}

// endedValue is the closing book value of the last schedule year before
// currentYear, or start with Year 0 while none has ended.
func endedValue(years []Year, currentYear int, start int64) BookValue {
	v := BookValue{Cents: start}
	// Schedule returns the years in ascending order.
	for _, y := range years {
		if y.Year >= currentYear {
			break
		}
		v = BookValue{Cents: y.ClosingBookValueCents, Year: y.Year}
	}

	return v
}

// valueAtEndOf is the closing book value of the last schedule year up to
// and including year, or start when the schedule has none.
func valueAtEndOf(years []Year, year int, start int64) int64 {
	v := start
	for _, y := range years {
		if y.Year > year {
			break
		}
		v = y.ClosingBookValueCents
	}

	return v
}
