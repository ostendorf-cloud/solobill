package euer

import (
	"errors"
	"time"
)

// The turn of the year window: entries from 22 December to 10 January are
// listed for the owner to assign to a tax year (docs/concepts/
// solobill.md, "EÜR mapping as data per tax year").
//
// NOT VERIFIED: the 10 day reading of section 11 Abs. 1 Satz 2 and Abs. 2
// Satz 2 EStG ("kurze Zeit") comes from secondary sources only, and so
// does its application to USt-Vorauszahlungen. solobill never decides it:
// it lists the entries of the window and the owner assigns each one, or
// leaves it in the year of its date.
const (
	windowStartDay = 22 // December
	windowEndDay   = 10 // January
)

// InWindow reports whether the calendar date of d lies from 22 December
// to 10 January, both inclusive.
func InWindow(d time.Time) bool {
	switch d.Month() {
	case time.December:
		return d.Day() >= windowStartDay
	case time.January:
		return d.Day() <= windowEndDay
	default:
		return false
	}
}

// neighbourYear is the other year an entry inside the window may be
// assigned to: the next year for a December date, the previous one for a
// January date.
func neighbourYear(d time.Time) int {
	if d.Month() == time.December {
		return d.Year() + 1
	}

	return d.Year() - 1
}

var (
	// ErrOutsideWindow refuses an assignment of an entry dated outside 22
	// December to 10 January.
	ErrOutsideWindow = errors.New("euer: the entry's date lies outside 22 December to 10 January")
	// ErrWrongTaxYear refuses an assignment to a year that is neither the
	// year of the entry's date nor the neighbouring year it may belong
	// to.
	ErrWrongTaxYear = errors.New("euer: the tax year is neither the year of the date nor its neighbouring year")
)

// CheckAssignment checks the owner's assignment of an entry dated d to
// taxYear: d lies in the window, and taxYear is the year of d or the
// neighbouring year.
func CheckAssignment(d time.Time, taxYear int) error {
	if !InWindow(d) {
		return ErrOutsideWindow
	}
	if taxYear != d.Year() && taxYear != neighbourYear(d) {
		return ErrWrongTaxYear
	}

	return nil
}

// windowsOf are the two windows that touch year: the turn into it and the
// turn out of it.
func windowsOf(year int) [2][2]time.Time {
	return [2][2]time.Time{
		{date(year-1, time.December, windowStartDay), date(year, time.January, windowEndDay)},
		{date(year, time.December, windowStartDay), date(year+1, time.January, windowEndDay)},
	}
}

// LoadRange is the range of dates whose entries a report of year needs:
// every entry that may count in year, with or without an assignment, and
// every entry of the two windows. Both ends are inclusive.
func LoadRange(year int) (from, to time.Time) {
	w := windowsOf(year)

	return w[0][0], w[1][1]
}

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// dateOnly drops the clock and the location of t, keeping its calendar
// date as stored.
func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()

	return date(y, m, d)
}
