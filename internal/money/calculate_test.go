package money_test

import (
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/money"
)

func percentPtr(v int32) *money.Permyriad {
	p := money.Permyriad(v)

	return &p
}

func amountPtr(v int64) *money.Cents {
	c := money.Cents(v)

	return &c
}

// TestCalculate_WorkedExample reproduces docs/concepts/solobill.md's
// Rounding section worked example exactly, figure by figure: two lines
// (net after their own line-level discount EUR 120.04 and EUR 70.04,
// no line-level discount configured here since the doc's own example
// starts from the already-discounted net, see the line's own
// UnitPriceCents below), a 15% document-level discount, VAT at 19%. If
// this test's expectations ever disagree with the doc's own worked
// example, the doc is right and this package has a bug.
func TestCalculate_WorkedExample(t *testing.T) {
	t.Parallel()

	lines := []money.Line{
		{QuantityThousandths: 1000, UnitPriceCents: 12004, VATRatePermyriad: 1900},
		{QuantityThousandths: 1000, UnitPriceCents: 7004, VATRatePermyriad: 1900},
	}

	got, err := money.Calculate(lines, money.Discount{PercentPermyriad: percentPtr(1500)})
	if err != nil {
		t.Fatalf("Calculate() error = %v, want nil", err)
	}

	if got.NetTotalCents != 19008 {
		t.Errorf("NetTotalCents = %d, want 19008 (EUR 190.08)", got.NetTotalCents)
	}

	if got.DocumentDiscountCents != 2851 {
		t.Errorf("DocumentDiscountCents = %d, want 2851 (EUR 28.51)", got.DocumentDiscountCents)
	}

	if len(got.Lines) != 2 {
		t.Fatalf("len(Lines) = %d, want 2", len(got.Lines))
	}

	if got.Lines[0].DocumentDiscountShareCents != 1801 {
		t.Errorf("Lines[0].DocumentDiscountShareCents = %d, want 1801 (EUR 18.01, the tied remainder's winner)", got.Lines[0].DocumentDiscountShareCents)
	}

	if got.Lines[1].DocumentDiscountShareCents != 1050 {
		t.Errorf("Lines[1].DocumentDiscountShareCents = %d, want 1050 (EUR 10.50)", got.Lines[1].DocumentDiscountShareCents)
	}

	if got.NetAfterDocumentDiscountCents != 16157 {
		t.Errorf("NetAfterDocumentDiscountCents = %d, want 16157 (EUR 161.57)", got.NetAfterDocumentDiscountCents)
	}

	if len(got.VAT) != 1 {
		t.Fatalf("len(VAT) = %d, want 1 (single rate, 19%%)", len(got.VAT))
	}

	if got.VAT[0].VATCents != 3070 {
		t.Errorf("VAT[0].VATCents = %d, want 3070 (EUR 30.70, 161.57 x 0.19 = 30.6983 rounded)", got.VAT[0].VATCents)
	}

	if got.GrandTotalCents != 19227 {
		t.Errorf("GrandTotalCents = %d, want 19227 (EUR 192.27)", got.GrandTotalCents)
	}
}

func TestCalculate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		lines            []money.Line
		documentDiscount money.Discount
		want             money.Result
	}{
		{
			name: "single line, no discount at all",
			lines: []money.Line{
				{QuantityThousandths: 2500, UnitPriceCents: 1000, VATRatePermyriad: 1900},
			},
			want: money.Result{
				Lines: []money.LineResult{
					{NetAfterLineDiscountCents: 2500, NetAfterAllDiscountsCents: 2500},
				},
				NetTotalCents:                 2500,
				NetAfterDocumentDiscountCents: 2500,
				VAT:                           []money.VATBucket{{RatePermyriad: 1900, NetCents: 2500, VATCents: 475}},
				VATTotalCents:                 475,
				GrandTotalCents:               2975,
			},
		},
		{
			name: "kleinunternehmer: zero VAT rate still computes a (zero) bucket",
			lines: []money.Line{
				{QuantityThousandths: 1000, UnitPriceCents: 5000, VATRatePermyriad: 0},
			},
			want: money.Result{
				Lines: []money.LineResult{
					{NetAfterLineDiscountCents: 5000, NetAfterAllDiscountsCents: 5000},
				},
				NetTotalCents:                 5000,
				NetAfterDocumentDiscountCents: 5000,
				VAT:                           []money.VATBucket{{RatePermyriad: 0, NetCents: 5000, VATCents: 0}},
				VATTotalCents:                 0,
				GrandTotalCents:               5000,
			},
		},
		{
			name: "line-level percentage discount",
			lines: []money.Line{
				{
					QuantityThousandths: 1000, UnitPriceCents: 10000, VATRatePermyriad: 1900,
					Discount: money.Discount{PercentPermyriad: percentPtr(1000)}, // 10%
				},
			},
			want: money.Result{
				Lines: []money.LineResult{
					{NetAfterLineDiscountCents: 9000, NetAfterAllDiscountsCents: 9000},
				},
				NetTotalCents:                 9000,
				NetAfterDocumentDiscountCents: 9000,
				VAT:                           []money.VATBucket{{RatePermyriad: 1900, NetCents: 9000, VATCents: 1710}},
				VATTotalCents:                 1710,
				GrandTotalCents:               10710,
			},
		},
		{
			name: "line-level amount discount",
			lines: []money.Line{
				{
					QuantityThousandths: 1000, UnitPriceCents: 10000, VATRatePermyriad: 1900,
					Discount: money.Discount{AmountCents: amountPtr(250)},
				},
			},
			want: money.Result{
				Lines: []money.LineResult{
					{NetAfterLineDiscountCents: 9750, NetAfterAllDiscountsCents: 9750},
				},
				NetTotalCents:                 9750,
				NetAfterDocumentDiscountCents: 9750,
				// 9750 x 0.19 = 1852.5 exactly, a tie, rounding away
				// from zero (up, since positive) to 1853.
				VAT:             []money.VATBucket{{RatePermyriad: 1900, NetCents: 9750, VATCents: 1853}},
				VATTotalCents:   1853,
				GrandTotalCents: 11603,
			},
		},
		{
			name: "fractional quantity rounds the raw net at the one rounding point",
			lines: []money.Line{
				// 0.333 units at EUR 1.00: 33.3 cents exactly, rounding
				// down (below half) to 33.
				{QuantityThousandths: 333, UnitPriceCents: 100, VATRatePermyriad: 0},
			},
			want: money.Result{
				Lines: []money.LineResult{
					{NetAfterLineDiscountCents: 33, NetAfterAllDiscountsCents: 33},
				},
				NetTotalCents:                 33,
				NetAfterDocumentDiscountCents: 33,
				VAT:                           []money.VATBucket{{RatePermyriad: 0, NetCents: 33, VATCents: 0}},
				VATTotalCents:                 0,
				GrandTotalCents:               33,
			},
		},
		{
			// The boundary right next to TestCalculate_Errors' "document
			// amount discount exceeds the net total": a discount exactly
			// equal to netTotal is accepted, leaving every figure at 0,
			// not refused.
			name: "document amount discount exactly equal to the net total",
			lines: []money.Line{
				{QuantityThousandths: 1000, UnitPriceCents: 10000, VATRatePermyriad: 1900},
			},
			documentDiscount: money.Discount{AmountCents: amountPtr(10000)},
			want: money.Result{
				Lines: []money.LineResult{
					{NetAfterLineDiscountCents: 10000, DocumentDiscountShareCents: 0, NetAfterAllDiscountsCents: 10000},
				},
				NetTotalCents:                 10000,
				DocumentDiscountCents:         10000,
				NetAfterDocumentDiscountCents: 0,
				VAT:                           []money.VATBucket{{RatePermyriad: 1900, NetCents: 0, VATCents: 0}},
				VATTotalCents:                 0,
				GrandTotalCents:               0,
			},
		},
		{
			name: "document amount discount on a single VAT rate needs no allocation",
			lines: []money.Line{
				{QuantityThousandths: 1000, UnitPriceCents: 5000, VATRatePermyriad: 1900},
				{QuantityThousandths: 1000, UnitPriceCents: 5000, VATRatePermyriad: 1900},
			},
			documentDiscount: money.Discount{AmountCents: amountPtr(1500)},
			want: money.Result{
				Lines: []money.LineResult{
					{NetAfterLineDiscountCents: 5000, NetAfterAllDiscountsCents: 5000},
					{NetAfterLineDiscountCents: 5000, NetAfterAllDiscountsCents: 5000},
				},
				NetTotalCents:                 10000,
				DocumentDiscountCents:         1500,
				NetAfterDocumentDiscountCents: 8500,
				VAT:                           []money.VATBucket{{RatePermyriad: 1900, NetCents: 8500, VATCents: 1615}},
				VATTotalCents:                 1615,
				GrandTotalCents:               10115,
			},
		},
		{
			// per-line and per-rate VAT rounding visibly differ here:
			// rounding each line's own VAT separately
			// (round(333 x 0.5%) = round(1.665) = 2, twice, summing to
			// 4) gives a different total than this package's own
			// per-rate rounding (round(666 x 0.5%) = round(3.33) = 3),
			// which docs/concepts/solobill.md requires.
			name: "VAT rounds once per rate, not once per line, and the two visibly differ",
			lines: []money.Line{
				{QuantityThousandths: 1000, UnitPriceCents: 333, VATRatePermyriad: 50},
				{QuantityThousandths: 1000, UnitPriceCents: 333, VATRatePermyriad: 50},
			},
			want: money.Result{
				Lines: []money.LineResult{
					{NetAfterLineDiscountCents: 333, NetAfterAllDiscountsCents: 333},
					{NetAfterLineDiscountCents: 333, NetAfterAllDiscountsCents: 333},
				},
				NetTotalCents:                 666,
				NetAfterDocumentDiscountCents: 666,
				VAT:                           []money.VATBucket{{RatePermyriad: 50, NetCents: 666, VATCents: 3}},
				VATTotalCents:                 3,
				GrandTotalCents:               669,
			},
		},
		{
			// Mixed VAT rates with an amount-based document discount:
			// this package's own reasoned extension (see calculateVAT's
			// doc comment), allocating the amount across rate buckets by
			// largest remainder rather than across lines. Net 6667 at
			// 19% and 3333 at 7%, discount amount 1000: exact bucket
			// shares 666.7 and 333.3, floor 666/333 (sum 999), the
			// single leftover cent going to the larger remainder (0.7).
			name: "document amount discount allocated across mixed VAT rate buckets",
			lines: []money.Line{
				{QuantityThousandths: 1000, UnitPriceCents: 6667, VATRatePermyriad: 1900},
				{QuantityThousandths: 1000, UnitPriceCents: 3333, VATRatePermyriad: 700},
			},
			documentDiscount: money.Discount{AmountCents: amountPtr(1000)},
			want: money.Result{
				Lines: []money.LineResult{
					{NetAfterLineDiscountCents: 6667, NetAfterAllDiscountsCents: 6667},
					{NetAfterLineDiscountCents: 3333, NetAfterAllDiscountsCents: 3333},
				},
				NetTotalCents:                 10000,
				DocumentDiscountCents:         1000,
				NetAfterDocumentDiscountCents: 9000,
				VAT: []money.VATBucket{
					{RatePermyriad: 700, NetCents: 3000, VATCents: 210},
					{RatePermyriad: 1900, NetCents: 6000, VATCents: 1140},
				},
				VATTotalCents:   1350,
				GrandTotalCents: 10350,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := money.Calculate(tt.lines, tt.documentDiscount)
			if err != nil {
				t.Fatalf("Calculate() error = %v, want nil", err)
			}

			assertResult(t, got, tt.want)
		})
	}
}

func assertResult(t *testing.T, got, want money.Result) {
	t.Helper()

	if len(got.Lines) != len(want.Lines) {
		t.Fatalf("len(Lines) = %d, want %d", len(got.Lines), len(want.Lines))
	}

	for i := range want.Lines {
		if got.Lines[i] != want.Lines[i] {
			t.Errorf("Lines[%d] = %+v, want %+v", i, got.Lines[i], want.Lines[i])
		}
	}

	if got.NetTotalCents != want.NetTotalCents {
		t.Errorf("NetTotalCents = %d, want %d", got.NetTotalCents, want.NetTotalCents)
	}

	if got.DocumentDiscountCents != want.DocumentDiscountCents {
		t.Errorf("DocumentDiscountCents = %d, want %d", got.DocumentDiscountCents, want.DocumentDiscountCents)
	}

	if got.NetAfterDocumentDiscountCents != want.NetAfterDocumentDiscountCents {
		t.Errorf("NetAfterDocumentDiscountCents = %d, want %d", got.NetAfterDocumentDiscountCents, want.NetAfterDocumentDiscountCents)
	}

	if len(got.VAT) != len(want.VAT) {
		t.Fatalf("len(VAT) = %d, want %d", len(got.VAT), len(want.VAT))
	}

	for i := range want.VAT {
		if got.VAT[i] != want.VAT[i] {
			t.Errorf("VAT[%d] = %+v, want %+v", i, got.VAT[i], want.VAT[i])
		}
	}

	if got.VATTotalCents != want.VATTotalCents {
		t.Errorf("VATTotalCents = %d, want %d", got.VATTotalCents, want.VATTotalCents)
	}

	if got.GrandTotalCents != want.GrandTotalCents {
		t.Errorf("GrandTotalCents = %d, want %d", got.GrandTotalCents, want.GrandTotalCents)
	}
}

func TestCalculate_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		lines            []money.Line
		documentDiscount money.Discount
	}{
		{
			name:  "no lines at all",
			lines: nil,
		},
		{
			name: "zero quantity",
			lines: []money.Line{
				{QuantityThousandths: 0, UnitPriceCents: 100, VATRatePermyriad: 1900},
			},
		},
		{
			name: "negative quantity",
			lines: []money.Line{
				{QuantityThousandths: -1000, UnitPriceCents: 100, VATRatePermyriad: 1900},
			},
		},
		{
			name: "negative unit price",
			lines: []money.Line{
				{QuantityThousandths: 1000, UnitPriceCents: -1, VATRatePermyriad: 1900},
			},
		},
		{
			name: "VAT rate above 100%",
			lines: []money.Line{
				{QuantityThousandths: 1000, UnitPriceCents: 100, VATRatePermyriad: 10001},
			},
		},
		{
			name: "VAT rate negative",
			lines: []money.Line{
				{QuantityThousandths: 1000, UnitPriceCents: 100, VATRatePermyriad: -1},
			},
		},
		{
			name: "line amount discount exceeds the line's own net amount",
			lines: []money.Line{
				{
					QuantityThousandths: 1000, UnitPriceCents: 100, VATRatePermyriad: 1900,
					Discount: money.Discount{AmountCents: amountPtr(500)},
				},
			},
		},
		{
			name: "line discount percent above 100%",
			lines: []money.Line{
				{
					QuantityThousandths: 1000, UnitPriceCents: 100, VATRatePermyriad: 1900,
					Discount: money.Discount{PercentPermyriad: percentPtr(10001)},
				},
			},
		},
		{
			name: "line discount both percent and amount set",
			lines: []money.Line{
				{
					QuantityThousandths: 1000, UnitPriceCents: 100, VATRatePermyriad: 1900,
					Discount: money.Discount{PercentPermyriad: percentPtr(100), AmountCents: amountPtr(1)},
				},
			},
		},
		{
			name: "document discount both percent and amount set",
			lines: []money.Line{
				{QuantityThousandths: 1000, UnitPriceCents: 100, VATRatePermyriad: 1900},
			},
			documentDiscount: money.Discount{PercentPermyriad: percentPtr(100), AmountCents: amountPtr(1)},
		},
		{
			name: "document discount percent above 100%",
			lines: []money.Line{
				{QuantityThousandths: 1000, UnitPriceCents: 100, VATRatePermyriad: 1900},
			},
			documentDiscount: money.Discount{PercentPermyriad: percentPtr(10001)},
		},
		{
			name: "document discount negative amount",
			lines: []money.Line{
				{QuantityThousandths: 1000, UnitPriceCents: 100, VATRatePermyriad: 1900},
			},
			documentDiscount: money.Discount{AmountCents: amountPtr(-1)},
		},
		{
			// netTotal is 200 (100 at 19% plus 100 at 7%); a 500-cent
			// document amount discount exceeds it, the document-level
			// counterpart of "line amount discount exceeds the line's
			// own net amount" above.
			name: "document amount discount exceeds the net total",
			lines: []money.Line{
				{QuantityThousandths: 1000, UnitPriceCents: 100, VATRatePermyriad: 1900},
				{QuantityThousandths: 1000, UnitPriceCents: 100, VATRatePermyriad: 700},
			},
			documentDiscount: money.Discount{AmountCents: amountPtr(500)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := money.Calculate(tt.lines, tt.documentDiscount)
			if err == nil {
				t.Fatal("Calculate() error = nil, want an error")
			}
		})
	}
}
