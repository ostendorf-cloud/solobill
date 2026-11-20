package pdf

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/rand/v2"
	"testing"
)

// realisticDescriptions are descriptions a freelancer writes. The pre
// check must accept them (one may stay refused), and Typst must lay out
// every accepted one.
var realisticDescriptions = []string{
	"Lieferung „Netzwerkkomponenten“, Bestellnummer 4711",
	"Lieferung „Netzwerkkomponenten“ (Bestellnummer 4711)",
	"Einrichtung Firewall, VPN, Monitoring, Backup und Support",
	"Firewall, VPN, Monitoring, Backup, Support",
	"Datenmigration, Tests, Dokumentation, Schulung",
	"Beratung (remote), Konzeption, Umsetzung",
	"Projektleitung - Phase 2 - Abnahme",
	"Workshop: Anforderungen, Architektur, Roadmap",
	"Setup of firewall, VPN, monitoring, backup, support",
	"Consulting (remote) - phase 2: architecture, review",
	"Wartung lt. Vertrag Nr. 2026-17, Zeitraum 01.09.-30.09.2026",
	"Reisekosten: Bahn, Hotel, Verpflegung (pauschal)",
	"Softwareentwicklung „Kundenportal“, Sprint 14, Ticket ABC-1234",
}

func TestRealisticDescriptions(t *testing.T) {
	r := New(Options{Binary: typstOrSkip(t), TempDir: t.TempDir()})
	for _, fixture := range []string{"invoice-standard-de", "invoice-kleinunternehmer-de"} {
		accepted := 0
		for _, d := range realisticDescriptions {
			p := loadFixture(t, fixture)
			p.Lines[0].Description = d
			if err := CheckPrintable(p); err != nil {
				t.Logf("%s: refused by the pre check: %q: %v", fixture, d, err)

				continue
			}
			accepted++
			if _, err := r.RenderPayload(context.Background(), p, false); err != nil {
				t.Errorf("%s: accepted %q does not render: %v", fixture, d, err)
			}
		}
		if accepted < 12 {
			t.Errorf("%s: %d of %d realistic descriptions accepted, want at least 12", fixture, accepted, len(realisticDescriptions))
		}
	}
}

// TestCellBoundaries puts one word just below and one just above a
// cell's width straight into the template data, past the pre check, for
// every kind of cell. Below must render, above must be refused with the
// cell's field path. This proves the overflow check on its own, without
// the template hash pin. Skipped with -short.
func TestCellBoundaries(t *testing.T) {
	if testing.Short() {
		t.Skip("renders about 30 documents")
	}
	bin := typstOrSkip(t)
	r := New(Options{Binary: bin, TempDir: t.TempDir()})
	m, err := loadMetrics()
	if err != nil {
		t.Fatal(err)
	}
	inner := func(colMM float64) float64 { return (colMM - 2*cellInsetMM) * ptPerMM }
	type target struct {
		name, fixture, path string
		widthPt, sizePt     float64
		f                   face
		set                 func(in *Input, s string)
	}
	targets := []target{
		{
			"amount", "invoice-standard-de", "lines[0].net_after_line_discount_cents", inner(colAmountMM), 8, regular,
			func(in *Input, s string) { in.Lines[0].Amount = s },
		},
		{
			"amount without VAT column", "invoice-kleinunternehmer-de", "lines[0].net_after_line_discount_cents", inner(colAmountMM), 8, regular,
			func(in *Input, s string) { in.Lines[0].Amount = s },
		},
		{
			"unit price", "invoice-standard-de", "lines[0].unit_price_cents", inner(colUnitPriceMM), 8, regular,
			func(in *Input, s string) { in.Lines[0].UnitPrice = s },
		},
		{
			"quantity", "invoice-standard-de", "lines[0].quantity_thousandths", inner(colQuantityMM), 8, regular,
			func(in *Input, s string) { in.Lines[0].Quantity = s },
		},
		{
			"vat", "invoice-standard-de", "lines[0].vat_rate_permyriad", inner(colVATMM), 8, regular,
			func(in *Input, s string) { in.Lines[0].VATRate = s },
		},
		{
			"position", "invoice-standard-de", "lines[0].position", inner(colPositionMM), 8, regular,
			func(in *Input, s string) { in.Lines[0].Position = s },
		},
		{
			"unit", "invoice-standard-de", "lines[0].unit", inner(colUnitMM), 8, regular,
			func(in *Input, s string) { in.Lines[0].Unit = s },
		},
		{
			"meta value", "invoice-standard-de", "number", cellMetaValue.widthMM * ptPerMM, 9, regular,
			func(in *Input, s string) { in.Meta[0].Value = s },
		},
		{
			"total bold", "invoice-standard-de", "totals.grand_total_cents", totalsValueMM * ptPerMM, 10, bold,
			func(in *Input, s string) { in.Totals[len(in.Totals)-1].Value = s },
		},
		{
			"total", "invoice-standard-de", "totals.net_total_cents", totalsValueMM * ptPerMM, 9, regular,
			func(in *Input, s string) { in.Totals[0].Value = s },
		},
		{
			"total label", "invoice-standard-de", "totals.net_total_cents", cellTotalLabel.widthMM * ptPerMM, 9, regular,
			func(in *Input, s string) { in.Totals[0].Label = s },
		},
		{
			"letterhead", "invoice-standard-de", "seller.legal_name", textWidthMM * ptPerMM, 14, bold,
			func(in *Input, s string) { in.SellerName = s },
		},
		{
			"address", "invoice-standard-de", "customer.name", cellAddress.widthMM * ptPerMM, 10, regular,
			func(in *Input, s string) { in.RecipientLines[0] = s },
		},
		{
			"column header", "invoice-standard-de", "template.columns", inner(colAmountMM), 8, bold,
			func(in *Input, s string) { in.Columns.Amount = s },
		},
	}
	widthPt := func(s string, size float64, f face) float64 {
		var em float64
		for _, r := range s {
			em += m.em(f, r)
		}

		return em * size
	}
	charset := []rune("0123456789.,ilWM")
	rng := rand.New(rand.NewPCG(5, 91))
	// word returns a word whose width minus w lies in [lo, hi] points.
	word := func(w, size float64, f face, lo, hi float64) string {
		for range 200000 {
			var b []rune
			for widthPt(string(b), size, f) < w+lo-12 {
				b = append(b, charset[rng.IntN(len(charset))])
			}
			for range 8 {
				c := charset[rng.IntN(len(charset))]
				d := widthPt(string(append(b, c)), size, f) - w
				if d >= lo && d <= hi {
					return string(append(b, c))
				}
				if d < lo {
					b = append(b, c)
				}
			}
		}

		return ""
	}
	for _, tg := range targets {
		src, err := BuildSource(loadFixture(t, tg.fixture), false)
		if err != nil {
			t.Fatal(err)
		}
		// The cell width as Typst laid it out.
		cw := 0.0
		for _, c := range fitCells(t, bin, src) {
			if math.Abs(c.w-tg.widthPt) < 0.6 {
				cw = c.w
			}
		}
		if cw == 0 {
			t.Errorf("%s: no cell of %.2f pt", tg.name, tg.widthPt)

			continue
		}
		for _, c := range []struct {
			lo, hi float64
			fits   bool
		}{{-0.15, -0.1, true}, {0.2, 0.3, false}} {
			s := word(cw, tg.sizePt, tg.f, c.lo, c.hi)
			if s == "" {
				t.Fatalf("%s: no word %.2f..%.2f pt over %.2f pt", tg.name, c.lo, c.hi, cw)
			}
			var in Input
			if err := json.Unmarshal(src.Data, &in); err != nil {
				t.Fatal(err)
			}
			tg.set(&in, s)
			data, err := MarshalInput(in)
			if err != nil {
				t.Fatal(err)
			}
			edited := src
			edited.Data = data
			_, err = r.compile(context.Background(), edited)
			over := widthPt(s, tg.sizePt, tg.f) - cw
			var invalid *InvalidPayloadError
			refusedHere := errors.Is(err, ErrLayoutOverflow) && errors.As(err, &invalid) && invalid.Details[tg.path] != ""
			switch {
			case c.fits && err != nil:
				t.Errorf("%s: a word %+.3f pt over the cell is refused: %v", tg.name, over, err)
			case !c.fits && !refusedHere:
				t.Errorf("%s: a word %+.3f pt over the cell: err = %v, want an overflow at %s", tg.name, over, err, tg.path)
			}
		}
	}
}
