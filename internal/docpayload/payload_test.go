package docpayload_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/money"
)

// standardInvoice builds a complete invoice under the standard regime,
// its totals computed by internal/money exactly as the issue step will.
// It reuses the worked example of docs/concepts/solobill.md's Rounding
// section, so the totals are figures a reviewer already checked by hand.
func standardInvoice(t *testing.T) docpayload.FrozenPayloadV1 {
	t.Helper()

	percent := int32(1500)
	p := docpayload.FrozenPayloadV1{
		Version:         docpayload.Version1,
		Currency:        docpayload.CurrencyEUR,
		Type:            docpayload.TypeInvoice,
		Language:        docpayload.LanguageGerman,
		VATRegime:       docpayload.RegimeStandard,
		TemplateVersion: "invoice-v1",
		Number:          "RE-20260018",
		NumberValue:     18,
		NumberYear:      2026,
		IssueDate:       "2026-12-01",
		ServicePeriod:   &docpayload.ServicePeriod{From: "2026-11-01", To: "2026-11-30"},
		DueDate:         "2026-12-15",
		Seller: docpayload.SellerSnapshot{
			LegalName: "Example Trading",
			Address:   docpayload.Address{Street: "Main Street 1", PostalCode: "12345", City: "Example City", CountryCode: "DE"},
			TaxNumber: "12/345/67890",
			IBAN:      "DE00000000000000000000",
		},
		Customer: docpayload.CustomerSnapshot{
			ID:      "6f1c2a4e-0000-4000-8000-000000000001",
			Name:    "Customer Ltd",
			Address: docpayload.Address{Street: "Side Street 2", PostalCode: "54321", City: "Other City", CountryCode: "DE"},
		},
		Lines: []docpayload.Line{
			{Position: 1, Description: "Consulting", QuantityThousandths: 1000, Unit: "piece", UnitPriceCents: 12004, VATRatePermyriad: 1900},
			{Position: 2, Description: "Support", QuantityThousandths: 1000, Unit: "piece", UnitPriceCents: 7004, VATRatePermyriad: 1900},
		},
		DocumentDiscount: docpayload.Discount{PercentPermyriad: &percent},
	}

	pp := money.Permyriad(percent)
	result, err := money.Calculate([]money.Line{
		{QuantityThousandths: 1000, UnitPriceCents: 12004, VATRatePermyriad: 1900},
		{QuantityThousandths: 1000, UnitPriceCents: 7004, VATRatePermyriad: 1900},
	}, money.Discount{PercentPermyriad: &pp}, money.VATRegimeStandard)
	if err != nil {
		t.Fatalf("money.Calculate() error = %v", err)
	}
	if err := docpayload.ApplyLineResults(p.Lines, result.Lines); err != nil {
		t.Fatalf("ApplyLineResults() error = %v", err)
	}
	p.Totals = docpayload.TotalsFromMoney(result)

	return p
}

// correctionOf is the first correction of standardInvoice under the
// replace model: the full corrected lines, the original's number plus
// "-1".
func correctionOf(t *testing.T) docpayload.FrozenPayloadV1 {
	t.Helper()

	p := standardInvoice(t)
	p.Type = docpayload.TypeCorrection
	p.Corrects = &docpayload.DocumentReference{
		ID:        "6f1c2a4e-0000-4000-8000-000000000003",
		Number:    "RE-20260018",
		IssueDate: "2026-12-01",
	}
	p.CorrectionSeq = 1
	p.Number = "RE-20260018-1"
	p.IssueDate = "2026-12-05"

	return p
}

// kleinunternehmerInvoice is a complete section 34a UStDV invoice.
func kleinunternehmerInvoice(t *testing.T) docpayload.FrozenPayloadV1 {
	t.Helper()

	p := standardInvoice(t)
	p.VATRegime = docpayload.RegimeKleinunternehmer
	p.Seller.TaxNumber = ""
	p.Seller.KleinunternehmerID = "DE-KU-0000000"
	p.ServicePeriod = nil
	p.DocumentDiscount = docpayload.Discount{}
	p.TaxNote = "Gemäß § 19 UStG wird keine Umsatzsteuer berechnet."
	for i := range p.Lines {
		p.Lines[i].VATRatePermyriad = 0
	}

	result, err := money.Calculate([]money.Line{
		{QuantityThousandths: 1000, UnitPriceCents: 12004},
		{QuantityThousandths: 1000, UnitPriceCents: 7004},
	}, money.Discount{}, money.VATRegimeKleinunternehmer)
	if err != nil {
		t.Fatalf("money.Calculate() error = %v", err)
	}
	if err := docpayload.ApplyLineResults(p.Lines, result.Lines); err != nil {
		t.Fatalf("ApplyLineResults() error = %v", err)
	}
	p.Totals = docpayload.TotalsFromMoney(result)

	return p
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	for name, build := range map[string]func(*testing.T) docpayload.FrozenPayloadV1{
		"standard":         standardInvoice,
		"kleinunternehmer": kleinunternehmerInvoice,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			want := build(t)
			data, err := docpayload.Marshal(want)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			got, err := docpayload.Unmarshal(data)
			if err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("round trip changed the payload\n got = %+v\nwant = %+v", got, want)
			}
		})
	}
}

func TestVersionField(t *testing.T) {
	t.Parallel()

	p := standardInvoice(t)
	data, err := docpayload.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if raw["version"] != float64(1) {
		t.Errorf(`"version" = %v, want 1`, raw["version"])
	}

	p.Version = 2
	if _, err := docpayload.Marshal(p); err == nil {
		t.Error("Marshal() of version 2 succeeded, want an error")
	}

	_, err = docpayload.Unmarshal([]byte(`{"version":2}`))
	if !errors.Is(err, docpayload.ErrUnsupportedVersion) {
		t.Errorf("Unmarshal(version 2) error = %v, want ErrUnsupportedVersion", err)
	}
}

func TestUnmarshalRefusesUnknownFields(t *testing.T) {
	t.Parallel()

	data, err := docpayload.Marshal(standardInvoice(t))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	tampered := strings.Replace(string(data), `{"version":1,`, `{"version":1,"surprise":true,`, 1)

	if _, err := docpayload.Unmarshal([]byte(tampered)); err == nil {
		t.Error("Unmarshal() accepted an unknown field")
	}
}

// TestTotalsComeFromMoney checks the worked example of the concept's
// Rounding section end to end through the payload.
func TestTotalsComeFromMoney(t *testing.T) {
	t.Parallel()

	got := standardInvoice(t).Totals
	want := docpayload.Totals{
		NetTotalCents:                 19008,
		DocumentDiscountCents:         2851,
		NetAfterDocumentDiscountCents: 16157,
		VAT:                           []docpayload.VATBucket{{RatePermyriad: 1900, NetCents: 16157, VATCents: 3070}},
		VATTotalCents:                 3070,
		GrandTotalCents:               19227,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Totals = %+v, want %+v", got, want)
	}
}

func TestApplyLineResultsRefusesACountMismatch(t *testing.T) {
	t.Parallel()

	if err := docpayload.ApplyLineResults(make([]docpayload.Line, 2), make([]money.LineResult, 1)); err == nil {
		t.Error("ApplyLineResults() error = nil, want a mismatch error")
	}
}

func TestCompletePayloadsValidate(t *testing.T) {
	t.Parallel()

	for name, build := range map[string]func(*testing.T) docpayload.FrozenPayloadV1{
		"standard":         standardInvoice,
		"kleinunternehmer": kleinunternehmerInvoice,
		"correction":       correctionOf,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if problems := docpayload.Validate(build(t)); len(problems) != 0 {
				t.Errorf("Validate() = %v, want no problems", problems)
			}
		})
	}
}

// TestSection14Fields removes one field at a time and expects exactly
// that field reported. Each case names the paragraph it guards.
func TestSection14Fields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		build  func(*testing.T) docpayload.FrozenPayloadV1
		mutate func(*docpayload.FrozenPayloadV1)
		want   string
	}{
		{"Nr. 1 seller name", standardInvoice, func(p *docpayload.FrozenPayloadV1) { p.Seller.LegalName = "" }, "seller.legal_name"},
		{"Nr. 1 seller address", standardInvoice, func(p *docpayload.FrozenPayloadV1) { p.Seller.Address.City = "" }, "seller.address.city"},
		{"Nr. 1 customer name", standardInvoice, func(p *docpayload.FrozenPayloadV1) { p.Customer.Name = "" }, "customer.name"},
		{"Nr. 1 customer address", standardInvoice, func(p *docpayload.FrozenPayloadV1) { p.Customer.Address.Street = "" }, "customer.address.street"},
		{"Nr. 2 tax number", standardInvoice, func(p *docpayload.FrozenPayloadV1) { p.Seller.TaxNumber = "" }, "seller.tax_number"},
		{"Nr. 2 Kleinunternehmer id is not enough under the standard regime", standardInvoice, func(p *docpayload.FrozenPayloadV1) {
			p.Seller.TaxNumber = ""
			p.Seller.KleinunternehmerID = "DE-KU-0000000"
		}, "seller.tax_number"},
		{"Nr. 3 issue date", standardInvoice, func(p *docpayload.FrozenPayloadV1) { p.IssueDate = "01.12.2026" }, "issue_date"},
		{"Nr. 4 number", standardInvoice, func(p *docpayload.FrozenPayloadV1) { p.Number = "" }, "number"},
		{"Nr. 5 line description", standardInvoice, func(p *docpayload.FrozenPayloadV1) { p.Lines[1].Description = "" }, "lines[1].description"},
		{"Nr. 5 line quantity", standardInvoice, func(p *docpayload.FrozenPayloadV1) { p.Lines[0].QuantityThousandths = 0 }, "lines[0].quantity_thousandths"},
		{"Nr. 5 no lines", standardInvoice, func(p *docpayload.FrozenPayloadV1) { p.Lines = nil }, "lines"},
		{"Nr. 6 service period", standardInvoice, func(p *docpayload.FrozenPayloadV1) { p.ServicePeriod = nil }, "service_period"},
		{"Nr. 7 and 8 VAT buckets", standardInvoice, func(p *docpayload.FrozenPayloadV1) {
			p.Totals.VAT = nil
			p.Totals.VATTotalCents = 0
			p.Totals.GrandTotalCents = p.Totals.NetAfterDocumentDiscountCents
		}, "totals.vat"},
		{"currency", standardInvoice, func(p *docpayload.FrozenPayloadV1) { p.Currency = "USD" }, "currency"},
		{"net total is the sum of the lines", standardInvoice, func(p *docpayload.FrozenPayloadV1) {
			p.Lines[0].NetAfterLineDiscountCents++
			p.Lines[0].NetAfterAllDiscountsCents++
		}, "totals.net_total_cents"},
		{"line net after all discounts", standardInvoice, func(p *docpayload.FrozenPayloadV1) {
			p.Lines[1].NetAfterAllDiscountsCents++
		}, "lines[1].net_after_all_discounts_cents"},
		{"net after document discount", standardInvoice, func(p *docpayload.FrozenPayloadV1) {
			p.Totals.DocumentDiscountCents++
		}, "totals.net_after_document_discount_cents"},
		{"bucket VAT rounded once", standardInvoice, func(p *docpayload.FrozenPayloadV1) {
			p.Totals.VAT[0].VATCents++
			p.Totals.VATTotalCents++
			p.Totals.GrandTotalCents++
		}, "totals.vat[0].vat_cents"},
		{"VAT total is the sum of the buckets", standardInvoice, func(p *docpayload.FrozenPayloadV1) {
			p.Totals.VATTotalCents++
			p.Totals.GrandTotalCents++
		}, "totals.vat_total_cents"},
		{"grand total", standardInvoice, func(p *docpayload.FrozenPayloadV1) { p.Totals.GrandTotalCents++ }, "totals.grand_total_cents"},
		{"34a VAT total is 0", kleinunternehmerInvoice, func(p *docpayload.FrozenPayloadV1) {
			p.Totals.VAT[0].RatePermyriad = 1900
			// 19008 cents at 19 % is 3611.52, rounded once to 3612: the
			// bucket itself adds up, only the regime forbids it.
			p.Totals.VAT[0].VATCents = 3612
			p.Totals.VATTotalCents = 3612
			p.Totals.GrandTotalCents += 3612
		}, "totals.vat_total_cents"},
		{"correction number carries the suffix", correctionOf, func(p *docpayload.FrozenPayloadV1) { p.Number = "RE-20260018" }, "number"},
		{"correction needs a sequence", correctionOf, func(p *docpayload.FrozenPayloadV1) {
			p.CorrectionSeq = 0
		}, "correction_seq"},
		{"invoice has no sequence", standardInvoice, func(p *docpayload.FrozenPayloadV1) { p.CorrectionSeq = 1 }, "correction_seq"},
		{"Nr. 8 exemption note for a 0 percent line", standardInvoice, func(p *docpayload.FrozenPayloadV1) {
			p.Totals.VAT = append(p.Totals.VAT, docpayload.VATBucket{RatePermyriad: 0})
		}, "tax_note"},
		{"34a note", kleinunternehmerInvoice, func(p *docpayload.FrozenPayloadV1) { p.TaxNote = "" }, "tax_note"},
		{"34a tax identity", kleinunternehmerInvoice, func(p *docpayload.FrozenPayloadV1) { p.Seller.KleinunternehmerID = "" }, "seller.tax_number"},
		{"34a no VAT on a line", kleinunternehmerInvoice, func(p *docpayload.FrozenPayloadV1) { p.Lines[0].VATRatePermyriad = 1900 }, "lines[0].vat_rate_permyriad"},
		{"correction needs its original", standardInvoice, func(p *docpayload.FrozenPayloadV1) { p.Type = docpayload.TypeCorrection }, "corrects"},
		{"unknown language", standardInvoice, func(p *docpayload.FrozenPayloadV1) { p.Language = "fr" }, "language"},
		{"template version", standardInvoice, func(p *docpayload.FrozenPayloadV1) { p.TemplateVersion = "" }, "template_version"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := tt.build(t)
			tt.mutate(&p)
			problems := docpayload.Validate(p)
			if _, ok := problems[tt.want]; !ok || len(problems) != 1 {
				t.Errorf("Validate() = %v, want exactly %q", docpayload.SortedPaths(problems), tt.want)
			}
		})
	}
}

func TestOfferAndImportedRules(t *testing.T) {
	t.Parallel()

	offer := standardInvoice(t)
	offer.Type = docpayload.TypeOffer
	offer.ServicePeriod = nil
	offer.Seller.TaxNumber = ""
	offer.ValidUntil = "2026-12-31"
	if problems := docpayload.Validate(offer); len(problems) != 0 {
		t.Errorf("Validate(offer) = %v, want none", problems)
	}

	imported := standardInvoice(t)
	imported.Imported = true
	imported.Lines = nil
	imported.TemplateVersion = ""
	imported.Number = "R1234"
	if got := docpayload.SortedPaths(docpayload.Validate(imported)); !reflect.DeepEqual(got, []string{"original"}) {
		t.Errorf("Validate(imported without original) = %v, want [original]", got)
	}
	imported.Original = &docpayload.OriginalDocument{FileID: "6f1c2a4e-0000-4000-8000-000000000002", SHA256: strings.Repeat("a", 64)}
	if problems := docpayload.Validate(imported); len(problems) != 0 {
		t.Errorf("Validate(imported) = %v, want none", problems)
	}
}
