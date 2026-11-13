package money

import (
	"math/big"
	"sort"
)

// roundHalfAwayFromZero rounds r to the nearest integer, a tie rounding
// away from zero (kaufmännisches Runden, commercial rounding):
// docs/concepts/solobill.md's Rounding section's one rounding rule,
// applied at every point this package rounds a sub-cent figure to a
// whole cent. Implemented with math/big only: r is an exact rational,
// never converted through a float at any point.
func roundHalfAwayFromZero(r *big.Rat) int64 {
	num := r.Num()
	den := r.Denom() // big.Rat always normalizes this to a positive value

	neg := num.Sign() < 0

	absNum := new(big.Int).Abs(num)

	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(absNum, den, remainder)

	doubledRemainder := new(big.Int).Lsh(remainder, 1)
	if doubledRemainder.Cmp(den) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}

	result := quotient.Int64()
	if neg {
		result = -result
	}

	return result
}

// floorRat rounds r down, toward negative infinity.
func floorRat(r *big.Rat) int64 {
	num := r.Num()
	den := r.Denom()

	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(num, den, remainder)

	if remainder.Sign() != 0 && num.Sign() < 0 {
		quotient.Sub(quotient, big.NewInt(1))
	}

	return quotient.Int64()
}

// allocateLargestRemainder allocates target whole cents across
// exactShares' own unrounded, exact weights, per
// docs/concepts/solobill.md's Rounding section: floor every share to
// the cent first, then hand out the leftover cents (target minus the
// sum of the floored shares) one at a time to the shares with the
// largest fractional remainder, breaking a tie by original order (the
// order exactShares itself is given in, which every caller in this
// package already arranges to be line order). The returned shares
// always sum to target exactly: no cent is lost or invented.
//
// Every caller in this package passes a target that is at most
// len(exactShares) cents away from the floored shares' own sum, since
// target is itself always either that same exact sum rounded
// (resolveLineShares) or an already-whole-cent amount proportioned over
// the same shares (allocateAmountAcrossBuckets); leftover is then
// always within [0, len(exactShares)). The distribution loops below
// still wrap safely rather than panic if a future caller ever violates
// that.
func allocateLargestRemainder(exactShares []*big.Rat, target Cents) []Cents {
	n := len(exactShares)
	shares := make([]int64, n)
	remainders := make([]*big.Rat, n)

	var flooredSum int64

	for i, exact := range exactShares {
		floored := floorRat(exact)
		shares[i] = floored
		flooredSum += floored
		remainders[i] = new(big.Rat).Sub(exact, new(big.Rat).SetInt64(floored))
	}

	leftover := int64(target) - flooredSum

	order := make([]int, n)
	for i := range order {
		order[i] = i
	}

	// Largest remainder first; a tie keeps the original (line) order,
	// since sort.SliceStable never reorders two elements its comparator
	// reports as equal.
	sort.SliceStable(order, func(a, b int) bool {
		return remainders[order[a]].Cmp(remainders[order[b]]) > 0
	})

	switch {
	case leftover > 0:
		for k := int64(0); k < leftover; k++ {
			shares[order[int(k)%n]]++
		}
	case leftover < 0:
		// The mirror image of the case above: take back from the
		// smallest remainders first, which sit at the end of order.
		for k := int64(0); k < -leftover; k++ {
			shares[order[n-1-int(k)%n]]--
		}
	}

	result := make([]Cents, n)
	for i, s := range shares {
		result[i] = Cents(s)
	}

	return result
}
