package httpapi

import "testing"

// TestVATFromGross pins the rounding of the VAT extracted from a gross
// amount: half away from zero, never a truncation.
func TestVATFromGross(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		gross int64
		rate  int32
		want  int64
	}{
		// 47 * 1900 / 11900 = 7.50: rounds up. A truncation gives 7.
		{"exact half rounds up", 47, 1900, 8},
		{"below half rounds down", 103, 1900, 16},
		{"119 gross", 11900, 1900, 1900},
		{"zero rate", 5000, 0, 0},
		{"zero gross", 0, 1900, 0},
		{"7 percent", 107, 700, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := vatFromGross(tc.gross, tc.rate); got != tc.want {
				t.Errorf("vatFromGross(%d, %d) = %d, want %d", tc.gross, tc.rate, got, tc.want)
			}
		})
	}
}
