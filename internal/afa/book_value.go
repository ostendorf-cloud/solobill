package afa

// BookValue is the current book value of an asset, as docs/api.md,
// "Assets", defines it.
type BookValue struct {
	Cents int64
	// Year is the schedule year whose closing book value Cents is. It is
	// 0 when no schedule year has ended yet: Cents is then the book value
	// before any depreciation, the cost basis or, for an imported asset,
	// the opening book value.
	Year int
}

// CurrentBookValue is the book value of a at the end of the last schedule
// year before currentYear: the closing book value of that year. While no
// schedule year has ended, it is the value the schedule starts from. A
// disposal does not set it to 0: it follows the schedule, which keeps the
// rest of a linear asset as the closing book value of the disposal year.
//
// afa has no clock. The caller passes currentYear, the calendar year in
// Germany at its own clock, so the year an EÜR is filed for decides. It
// returns an *InvalidError for an asset Validate refuses.
func CurrentBookValue(a Asset, currentYear int) (BookValue, error) {
	years, err := Schedule(a)
	if err != nil {
		return BookValue{}, err
	}

	v := BookValue{Cents: a.CostBasisCents}
	if a.Opening != nil {
		v.Cents = a.Opening.BookValueCents
	}
	// Schedule returns the years in ascending order.
	for _, y := range years {
		if y.Year >= currentYear {
			break
		}
		v = BookValue{Cents: y.ClosingBookValueCents, Year: y.Year}
	}

	return v, nil
}
