// Package category is the closed set of EÜR line keys solobill books
// income, expenses and depreciation against.
//
// A key names a line of the Anlage EÜR, never a form number and never a
// Kennzahl: "euer.z27" is the line "Waren, Rohstoffe und Hilfsstoffe".
// The line numbers in the keys are those of the Anlage EÜR 2025, the
// year the set was first defined against. A key is stable forever, even
// when a later form moves its line. The mapping from a key to the line
// and the Kennzahl of one tax year is data per tax year, owned by the
// EÜR report (issue #80), not this package.
//
// A line with two Kennzahlen for "nicht abziehbar" and "abziehbar" gets
// two keys with the suffixes ".nd" and ".d".
//
// The keys are registered per issue, one file each, so two issues never
// edit the same lines: keys_67.go (the contract, verified 2025 lines),
// keys_76.go (the car cost lines, issue #76) and keys_80.go (Zeile 13 and
// Zeile 65, issue #80). Every key has a row in
// docs/concepts/euer-keys/<issue>.md of the issue that registers it. A
// test fails otherwise.
package category

import (
	"slices"
	"strconv"
	"strings"
)

// Key is one EÜR line key, for example "euer.z27".
type Key string

// Kind is who produces an amount for a key.
type Kind string

const (
	// KindIncome is a Betriebseinnahme the owner records as other income
	// (issue #76), outside an invoice.
	KindIncome Kind = "income"
	// KindExpense is a Betriebsausgabe the owner records as an expense
	// (issue #76).
	KindExpense Kind = "expense"
	// KindAsset is an amount the asset register computes (issue #78).
	// It is never chosen on an expense.
	KindAsset Kind = "asset"
	// KindDerived is an amount the EÜR report computes from invoices,
	// payments and VAT figures (issue #80). It is never chosen by hand.
	KindDerived Kind = "derived"
)

// Category is one key with its kind, its labels and the issue that owns
// it. LabelDE is the wording of the 2025 form line, shortened; LabelEN
// is a translation for the English user interface, never printed on a
// tax form.
type Category struct {
	Key     Key
	Kind    Kind
	LabelDE string
	LabelEN string
	// Issue is the issue whose keys_<issue>.go registers the key and
	// whose docs/concepts/euer-keys/<issue>.md documents it.
	Issue int
}

// all is the registered set. Append only: a key is never renamed or
// removed, because booked rows reference it.
var all []Category

// register adds the keys of one issue. Called from the init of
// keys_<issue>.go only.
func register(issue int, cats ...Category) {
	for _, c := range cats {
		c.Issue = issue
		all = append(all, c)
	}
}

// All returns every category in form order: by Zeile, then the plain
// key, then ".nd", then ".d". The slice is a copy.
func All() []Category {
	out := slices.Clone(all)
	slices.SortStableFunc(out, func(a, b Category) int {
		za, sa := split(a.Key)
		zb, sb := split(b.Key)
		if za != zb {
			return za - zb
		}

		return suffixRank(sa) - suffixRank(sb)
	})

	return out
}

// Zeile returns the 2025 line number a key names: "euer.z62.nd" is 62.
func Zeile(key Key) int {
	z, _ := split(key)

	return z
}

func split(key Key) (int, string) {
	rest := strings.TrimPrefix(string(key), "euer.z")
	number, suffix, _ := strings.Cut(rest, ".")
	z, err := strconv.Atoi(number)
	if err != nil {
		return -1, suffix
	}

	return z, suffix
}

func suffixRank(s string) int {
	switch s {
	case "":
		return 0
	case "nd":
		return 1
	default:
		return 2
	}
}

// Lookup returns the category of key and whether it exists.
func Lookup(key Key) (Category, bool) {
	for _, c := range all {
		if c.Key == key {
			return c, true
		}
	}

	return Category{}, false
}

// Valid reports whether key exists and has the given kind. An expense
// accepts only KindExpense keys, other income only KindIncome keys.
func Valid(key Key, kind Kind) bool {
	c, ok := Lookup(key)

	return ok && c.Kind == kind
}
