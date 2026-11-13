// Package money implements the rounding and VAT rules
// docs/concepts/solobill.md's Rounding section specifies: line totals,
// line discounts, a document-level discount allocated by largest
// remainder, and VAT computed once per rate on the post-discount net.
// That section is the specification, checked figure by figure by a
// reviewer; this package implements it, never re-derives it. Where this
// package's own doc comments disagree with docs/concepts/solobill.md,
// the doc is right and this package has a bug.
//
// Pure calculation, no database, no clock, no I/O: every amount in and
// out is an integer minor unit (Cents), and every intermediate step
// before a rounding point uses exact rational arithmetic (math/big.Rat),
// never a float, the same "no column is ever a float" rule
// docs/concepts/solobill.md states for persisted amounts, extended here
// to every calculation step in between two persisted or displayed
// figures.
package money

import (
	"errors"
	"fmt"
)

// Cents is an amount in integer minor currency units. Never a float;
// see the package doc comment.
type Cents int64

// Permyriad is a rate or a percentage in parts per 10,000: 1900 means
// 19.00%, 0 means 0%. The same scaled-integer reasoning as Cents, used
// for a VAT rate and for a percentage discount: see
// db/migrations' documents and document_lines migrations for why the
// persisted columns behind these values are integers, never a Postgres
// numeric and never a float.
type Permyriad int32

// Thousandths is a quantity scaled by 1,000: 2500 means 2.5. The same
// scaled-integer reasoning as Cents and Permyriad, covering a fractional
// quantity (hours, days) without a float; see db/migrations'
// document_lines migration.
type Thousandths int64

// Discount is a percentage or an absolute amount, never both. The zero
// value is "no discount at all".
type Discount struct {
	PercentPermyriad *Permyriad
	AmountCents      *Cents
}

// validate reports whether d is a legal discount: at most one of
// PercentPermyriad or AmountCents set, a percentage in [0, 10000], and
// an amount that is not negative.
func (d Discount) validate() error {
	if d.PercentPermyriad != nil && d.AmountCents != nil {
		return errors.New("percent and amount are mutually exclusive")
	}

	if d.PercentPermyriad != nil && (*d.PercentPermyriad < 0 || *d.PercentPermyriad > 10000) {
		return fmt.Errorf("percent %d permyriad out of range [0, 10000]", *d.PercentPermyriad)
	}

	if d.AmountCents != nil && *d.AmountCents < 0 {
		return fmt.Errorf("amount %d cents must not be negative", *d.AmountCents)
	}

	return nil
}

// Line is one billable document line, the calculation's own input shape
// for internal/storage.DocumentLine: quantity, the unit price already
// copied from the catalogue, a VAT rate, and the line's own discount.
type Line struct {
	QuantityThousandths Thousandths
	UnitPriceCents      Cents
	VATRatePermyriad    Permyriad
	Discount            Discount
}

// LineResult is one line's computed figures.
type LineResult struct {
	// NetAfterLineDiscountCents is this line's own net amount, after its
	// own Discount (never the document-level one), rounded to the cent
	// exactly once: quantity times unit price, minus the line's exact,
	// unrounded discount, rounded half away from zero at that point.
	// This is the figure NetAfterAllDiscountsCents below is itself
	// derived from.
	NetAfterLineDiscountCents Cents

	// DocumentDiscountShareCents is this line's own share of a
	// percentage document-level discount, allocated by largest
	// remainder over every line's NetAfterLineDiscountCents
	// (docs/concepts/solobill.md's Rounding section). Always 0 when the
	// document carries no discount, or an amount discount rather than a
	// percentage: an amount discount is never divided across lines (see
	// the doc's own "needs no allocation" rule), so no line's own
	// figure ever reflects it; it is accounted for only in Result's own
	// document-level and VAT-bucket totals.
	DocumentDiscountShareCents Cents

	// NetAfterAllDiscountsCents is NetAfterLineDiscountCents minus
	// DocumentDiscountShareCents. For a percentage document discount,
	// summing this across every line reproduces
	// Result.NetAfterDocumentDiscountCents exactly, by construction of
	// the largest-remainder allocation. For an amount document
	// discount, DocumentDiscountShareCents is always 0, so this equals
	// NetAfterLineDiscountCents unchanged, and the lines' own sum does
	// NOT equal Result.NetAfterDocumentDiscountCents: the amount
	// discount still reduces the document's own total, it is simply
	// never attributed to any one line's own displayed figure.
	NetAfterAllDiscountsCents Cents
}

// VATBucket is the VAT computed once for every line sharing one
// VATRatePermyriad, on that rate's own net total after every discount:
// docs/concepts/solobill.md's Rounding section, "VAT rounds once per
// rate ... never once per line".
type VATBucket struct {
	RatePermyriad Permyriad
	// NetCents is this rate's combined net total after the document
	// discount's share attributable to this rate is already subtracted;
	// see Result's own doc comment for how that attribution is decided
	// for a percentage versus an amount discount.
	NetCents Cents
	// VATCents is NetCents times RatePermyriad, rounded half away from
	// zero exactly once at that point.
	VATCents Cents
}

// Result is a whole document's computed figures.
type Result struct {
	// Lines is one LineResult per input Line, same order.
	Lines []LineResult

	// NetTotalCents is the document's net total before any
	// document-level discount: the sum of every line's own
	// NetAfterLineDiscountCents.
	NetTotalCents Cents

	// DocumentDiscountCents is the document-level discount actually
	// applied, resolved to a whole number of cents: for a percentage,
	// NetTotalCents times that percent, rounded half away from zero
	// exactly once; for an amount, exactly the configured amount,
	// unrounded because it already is a whole number of cents. 0 when
	// the document carries no discount at all.
	DocumentDiscountCents Cents

	// NetAfterDocumentDiscountCents is NetTotalCents minus
	// DocumentDiscountCents: both already integers, so this subtraction
	// introduces no rounding of its own.
	NetAfterDocumentDiscountCents Cents

	// VAT is one VATBucket per distinct VATRatePermyriad present among
	// the input lines, sorted ascending by rate.
	VAT []VATBucket

	// VATTotalCents is the sum of every VATBucket's own VATCents.
	VATTotalCents Cents

	// GrandTotalCents is NetAfterDocumentDiscountCents plus
	// VATTotalCents.
	GrandTotalCents Cents
}
