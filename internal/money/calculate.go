package money

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
)

// Calculate computes a whole document's figures from its lines and the
// document's own discount, per docs/concepts/solobill.md's Rounding
// section. It returns an error for an illegal input (a negative price,
// a discount that is both a percentage and an amount, a VAT rate
// outside [0, 10000]); it never substitutes a default for one.
func Calculate(lines []Line, documentDiscount Discount) (Result, error) {
	if len(lines) == 0 {
		return Result{}, errors.New("at least one line is required")
	}

	if err := documentDiscount.validate(); err != nil {
		return Result{}, fmt.Errorf("document discount: %w", err)
	}

	lineNets := make([]Cents, len(lines))

	for i, line := range lines {
		if err := line.Discount.validate(); err != nil {
			return Result{}, fmt.Errorf("line %d discount: %w", i, err)
		}

		if line.QuantityThousandths <= 0 {
			return Result{}, fmt.Errorf("line %d: quantity must be positive", i)
		}

		if line.UnitPriceCents < 0 {
			return Result{}, fmt.Errorf("line %d: unit price must not be negative", i)
		}

		if line.VATRatePermyriad < 0 || line.VATRatePermyriad > 10000 {
			return Result{}, fmt.Errorf("line %d: vat rate %d permyriad out of range [0, 10000]", i, line.VATRatePermyriad)
		}

		lineNets[i] = roundLineNet(line)
	}

	netTotal := sumCents(lineNets)

	documentDiscountCents, lineShares := resolveLineShares(documentDiscount, lineNets)

	netAfterDocumentDiscount := netTotal - documentDiscountCents

	lineResults := make([]LineResult, len(lines))
	for i := range lines {
		lineResults[i] = LineResult{
			NetAfterLineDiscountCents:  lineNets[i],
			DocumentDiscountShareCents: lineShares[i],
			NetAfterAllDiscountsCents:  lineNets[i] - lineShares[i],
		}
	}

	vat, vatTotal := calculateVAT(lines, lineNets, lineShares, documentDiscount, documentDiscountCents, netTotal)

	return Result{
		Lines:                         lineResults,
		NetTotalCents:                 netTotal,
		DocumentDiscountCents:         documentDiscountCents,
		NetAfterDocumentDiscountCents: netAfterDocumentDiscount,
		VAT:                           vat,
		VATTotalCents:                 vatTotal,
		GrandTotalCents:               netAfterDocumentDiscount + vatTotal,
	}, nil
}

// roundLineNet computes one line's net amount after its own line-level
// discount: quantity times unit price, at full (sub-cent) precision,
// minus that same line's exact, unrounded discount, rounded to the cent
// exactly once at this point. docs/concepts/solobill.md's Rounding
// section fixes the rawNet-minus-discount computation and the single
// rounding point; it does not separately specify whether the raw net
// and the line discount are each individually rounded before being
// subtracted, which would risk rounding twice (the doc's own, explicit
// "never rounded twice" rule) for no stated benefit, so this package
// subtracts first, at full precision, and rounds only the result.
func roundLineNet(line Line) Cents {
	rawNet := new(big.Rat).Mul(
		new(big.Rat).SetFrac64(int64(line.QuantityThousandths), 1000),
		new(big.Rat).SetInt64(int64(line.UnitPriceCents)),
	)

	lineDiscountExact := new(big.Rat)

	switch {
	case line.Discount.PercentPermyriad != nil:
		lineDiscountExact.Mul(rawNet, permyriadRat(*line.Discount.PercentPermyriad))
	case line.Discount.AmountCents != nil:
		lineDiscountExact.SetInt64(int64(*line.Discount.AmountCents))
	}

	netAfterLineDiscountExact := new(big.Rat).Sub(rawNet, lineDiscountExact)

	return Cents(roundHalfAwayFromZero(netAfterLineDiscountExact))
}

// resolveLineShares resolves the document-level discount to a whole
// number of cents, and, for a percentage discount only, allocates it
// across lineNets by largest remainder: docs/concepts/solobill.md's own
// worked example computes each line's exact share directly from that
// line's own (already rounded) NetAfterLineDiscountCents times the
// configured percent, not from a share of the already-rounded document
// total, so that is what this does too; see the package's own worked
// example test for the figures this reproduces exactly.
//
// An amount discount is never allocated across lines at all ("needs no
// allocation ... shown as such, never divided across lines"): every
// returned share is 0 in that case, and the configured amount is
// returned unrounded, since it already is a whole number of cents.
func resolveLineShares(documentDiscount Discount, lineNets []Cents) (Cents, []Cents) {
	shares := make([]Cents, len(lineNets))

	switch {
	case documentDiscount.PercentPermyriad != nil:
		percent := permyriadRat(*documentDiscount.PercentPermyriad)
		exactShares := make([]*big.Rat, len(lineNets))
		exactTotal := new(big.Rat)

		for i, net := range lineNets {
			exactShares[i] = new(big.Rat).Mul(new(big.Rat).SetInt64(int64(net)), percent)
			exactTotal.Add(exactTotal, exactShares[i])
		}

		total := Cents(roundHalfAwayFromZero(exactTotal))

		return total, allocateLargestRemainder(exactShares, total)

	case documentDiscount.AmountCents != nil:
		return *documentDiscount.AmountCents, shares

	default:
		return 0, shares
	}
}

// calculateVAT groups lines by VATRatePermyriad and computes each
// bucket's VAT once, on that rate's own net total after every discount
// attributable to it, rounded half away from zero exactly once per
// bucket.
//
// For a percentage document discount, a bucket's attributable share is
// simply the sum of its own lines' DocumentDiscountShareCents, which
// resolveLineShares already allocated exactly (the per-line shares sum
// to documentDiscountCents exactly, so the per-bucket sums do too).
//
// For an amount discount, docs/concepts/solobill.md states it is never
// divided across lines, but says nothing about a document with more
// than one distinct VAT rate, which its own worked example never
// exercises either. With a single rate, that rate trivially absorbs the
// whole amount. With more than one, this package allocates the amount
// across rate buckets (never across individual lines: every line's own
// DocumentDiscountShareCents still stays 0) using the same
// largest-remainder mechanism the doc specifies for lines, weighted by
// each bucket's own net share of the document total, so that per-rate
// VAT comes out correct and the bucket shares still sum to the
// configured amount exactly. This is this package's own reasoned
// extension of the doc's rule, not a case the doc itself resolves.
func calculateVAT(lines []Line, lineNets, lineShares []Cents, documentDiscount Discount, documentDiscountCents, netTotal Cents) ([]VATBucket, Cents) {
	rates := distinctSortedRates(lines)

	bucketNetBefore := make(map[Permyriad]Cents, len(rates))
	for i, line := range lines {
		bucketNetBefore[line.VATRatePermyriad] += lineNets[i]
	}

	bucketShare := make(map[Permyriad]Cents, len(rates))

	switch {
	case documentDiscount.PercentPermyriad != nil:
		for i, line := range lines {
			bucketShare[line.VATRatePermyriad] += lineShares[i]
		}

	case documentDiscount.AmountCents != nil:
		switch len(rates) {
		case 0:
			// No lines reach here at all (Calculate already requires
			// at least one), so this is unreachable in practice; left
			// out of the switch below only to avoid a spurious
			// division.
		case 1:
			bucketShare[rates[0]] = documentDiscountCents
		default:
			allocateAmountAcrossBuckets(rates, bucketNetBefore, netTotal, documentDiscountCents, bucketShare)
		}
	}

	buckets := make([]VATBucket, len(rates))

	var vatTotal Cents

	for i, rate := range rates {
		netAfter := bucketNetBefore[rate] - bucketShare[rate]
		vatExact := new(big.Rat).Mul(new(big.Rat).SetInt64(int64(netAfter)), permyriadRat(rate))
		vat := Cents(roundHalfAwayFromZero(vatExact))

		buckets[i] = VATBucket{RatePermyriad: rate, NetCents: netAfter, VATCents: vat}
		vatTotal += vat
	}

	return buckets, vatTotal
}

// allocateAmountAcrossBuckets is calculateVAT's multi-rate, amount-discount
// case: see that function's own doc comment for why and when this runs.
func allocateAmountAcrossBuckets(rates []Permyriad, bucketNetBefore map[Permyriad]Cents, netTotal, documentDiscountCents Cents, bucketShare map[Permyriad]Cents) {
	if netTotal == 0 {
		// Every bucket's own net is 0 too; there is no meaningful
		// proportion to allocate by, so there is nothing to allocate at
		// all (bucketShare is left at its zero value for every rate).
		return
	}

	netTotalRat := new(big.Rat).SetInt64(int64(netTotal))
	amountRat := new(big.Rat).SetInt64(int64(documentDiscountCents))

	exactShares := make([]*big.Rat, len(rates))
	for i, rate := range rates {
		share := new(big.Rat).SetInt64(int64(bucketNetBefore[rate]))
		share.Mul(share, amountRat)
		share.Quo(share, netTotalRat)
		exactShares[i] = share
	}

	allocated := allocateLargestRemainder(exactShares, documentDiscountCents)
	for i, rate := range rates {
		bucketShare[rate] = allocated[i]
	}
}

func distinctSortedRates(lines []Line) []Permyriad {
	seen := make(map[Permyriad]bool, len(lines))

	rates := make([]Permyriad, 0, len(lines))

	for _, line := range lines {
		if !seen[line.VATRatePermyriad] {
			seen[line.VATRatePermyriad] = true

			rates = append(rates, line.VATRatePermyriad)
		}
	}

	sort.Slice(rates, func(i, j int) bool { return rates[i] < rates[j] })

	return rates
}

func permyriadRat(p Permyriad) *big.Rat {
	return new(big.Rat).SetFrac64(int64(p), 10000)
}

func sumCents(cs []Cents) Cents {
	var total Cents
	for _, c := range cs {
		total += c
	}

	return total
}
