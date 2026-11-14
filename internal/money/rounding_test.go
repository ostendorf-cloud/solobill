package money

import (
	"math/big"
	"testing"
)

func TestRoundHalfAwayFromZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		num  int64
		den  int64
		want int64
	}{
		{name: "exact integer", num: 5, den: 1, want: 5},
		{name: "below half rounds down", num: 1049, den: 1000, want: 1},     // 1.049
		{name: "exactly half rounds up", num: 15, den: 10, want: 2},         // 1.5
		{name: "above half rounds up", num: 1800600, den: 1000, want: 1801}, // 1800.6
		{name: "negative below half rounds toward zero", num: -1049, den: 1000, want: -1},
		{name: "negative exactly half rounds away from zero", num: -15, den: 10, want: -2},
		{name: "negative above half rounds away from zero", num: -1800600, den: 1000, want: -1801},
		{name: "zero", num: 0, den: 1, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := new(big.Rat).SetFrac64(tt.num, tt.den)

			got := roundHalfAwayFromZero(r)
			if got != tt.want {
				t.Fatalf("roundHalfAwayFromZero(%d/%d) = %d, want %d", tt.num, tt.den, got, tt.want)
			}
		})
	}
}

func TestFloorRat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		num  int64
		den  int64
		want int64
	}{
		{name: "exact integer", num: 5, den: 1, want: 5},
		{name: "positive fraction floors down", num: 18006, den: 1000, want: 18},
		{name: "negative fraction floors further down", num: -18006, den: 1000, want: -19},
		{name: "zero", num: 0, den: 1, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := new(big.Rat).SetFrac64(tt.num, tt.den)

			got := floorRat(r)
			if got != tt.want {
				t.Fatalf("floorRat(%d/%d) = %d, want %d", tt.num, tt.den, got, tt.want)
			}
		})
	}
}

// TestAllocateLargestRemainder_WorkedExample pins
// docs/concepts/solobill.md's own Rounding section worked example at the
// allocation step itself: two lines sharing EUR 18.006 and EUR 10.506 of
// an exact, unrounded 15% discount (in cents: 1800.6 and 1050.6), a
// rounded target of 2851 cents (EUR 28.51). Both lines carry the same
// 0.6 remainder, so the tie breaks by order and line 1 (index 0)
// receives the single leftover cent.
func TestAllocateLargestRemainder_WorkedExample(t *testing.T) {
	t.Parallel()

	// In cents, matching the units allocateLargestRemainder and target
	// both operate in: EUR 18.006 and EUR 10.506 are 1800.6 and 1050.6
	// cents.
	shares := []*big.Rat{
		new(big.Rat).SetFrac64(18006, 10),
		new(big.Rat).SetFrac64(10506, 10),
	}

	got := allocateLargestRemainder(shares, 2851)

	want := []Cents{1801, 1050}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("allocateLargestRemainder(...) = %v, want %v", got, want)
	}
}

func TestAllocateLargestRemainder_SumsExactlyToTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		shares []*big.Rat
		target Cents
		want   []Cents
	}{
		{
			name: "no remainder at all",
			shares: []*big.Rat{
				new(big.Rat).SetFrac64(100, 1),
				new(big.Rat).SetFrac64(200, 1),
			},
			target: 300,
			want:   []Cents{100, 200},
		},
		{
			name: "three way tie keeps original order",
			shares: []*big.Rat{
				new(big.Rat).SetFrac64(1005, 10),
				new(big.Rat).SetFrac64(1005, 10),
				new(big.Rat).SetFrac64(1005, 10),
			},
			// Exact sum is 30.15; floor sum is 30 (10 each); target 31
			// hands out exactly one leftover cent, to index 0 since
			// every remainder ties.
			target: 31,
			want:   []Cents{11, 10, 10},
		},
		{
			name: "single share absorbs the whole target",
			shares: []*big.Rat{
				new(big.Rat).SetFrac64(500, 1),
			},
			target: 500,
			want:   []Cents{500},
		},
		{
			// A zero-weight line (a fully discounted line, say) has an
			// exact share of 0 and therefore a remainder of 0 as well:
			// it must never receive a leftover cent ahead of a line
			// with a genuinely larger, non-zero remainder. Exact shares
			// 0, 5.7 and 5.3; floor sum 0+5+5=10; target 12 hands out
			// two leftover cents, both to the non-zero lines
			// (remainders 0.7 and 0.3), none to the zero line.
			name: "a zero-weight share never receives a leftover cent ahead of a non-zero one",
			shares: []*big.Rat{
				new(big.Rat).SetFrac64(0, 1),
				new(big.Rat).SetFrac64(57, 10),
				new(big.Rat).SetFrac64(53, 10),
			},
			target: 12,
			want:   []Cents{0, 6, 6},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := allocateLargestRemainder(tt.shares, tt.target)

			var sum Cents
			for i, g := range got {
				if g != tt.want[i] {
					t.Fatalf("allocateLargestRemainder(...)[%d] = %d, want %d", i, g, tt.want[i])
				}

				sum += g
			}

			if sum != tt.target {
				t.Fatalf("sum of allocated shares = %d, want exactly target %d", sum, tt.target)
			}
		})
	}
}
