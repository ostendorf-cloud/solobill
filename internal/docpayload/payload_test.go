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

// withMoney fills the line figures and the totals of p the way the issue
// step will: through internal/money, from p's own lines and discount.
func withMoney(t *testing.T, p docpayload.FrozenPayloadV1) docpayload.FrozenPayloadV1 {
	t.Helper()

	toMoney := func(d docpayload.Discount) money.Discount {
		var out money.Discount
		if d.PercentPermyriad != nil {
			v := money.Permyriad(*d.PercentPermyriad)
			out.PercentPermyriad = &v
		}
		if d.AmountCents != nil {
			v := money.Cents(*d.AmountCents)
			out.AmountCents = &v
		}

		return out
	}

	in := make([]money.Line, len(p.Lines))
	for i, l := range p.Lines {
		in[i] = money.Line{
			QuantityThousandths: money.Thousandths(l.QuantityThousandths),
			UnitPriceCents:      money.Cents(l.UnitPriceCents),
			VATRatePermyriad:    money.Permyriad(l.VATRatePermyriad),
			Discount:            toMoney(l.LineDiscount),
		}
	}
	result, err := money.Calculate(in, toMoney(p.DocumentDiscount), money.VATRegime(p.VATRegime))
	if err != nil {
		t.Fatalf("money.Calculate() error = %v", err)
	}
	if err := docpayload.ApplyLineResults(p.Lines, result.Lines); err != nil {
		t.Fatalf("ApplyLineResults() error = %v", err)
	}
	p.Totals = docpayload.TotalsFromMoney(result)

	return p
}

// standardInvoice builds a complete invoice under the standard regime.
// It reuses the worked example of docs/concepts/solobill.md's Rounding
// section, so the totals are figures a reviewer already checked by hand.
func standardInvoice(t *testing.T) docpayload.FrozenPayloadV1 {
	t.Helper()

	percent := int32(1500)

	return withMoney(t, docpayload.FrozenPayloadV1{
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
	})
}

// mixedInvoice has two VAT rates, a line amount discount and a document
// amount discount, the paths the worked example does not cover.
func mixedInvoice(t *testing.T) docpayload.FrozenPayloadV1 {
	t.Helper()

	lineAmount := int64(500)
	docAmount := int64(1000)
	p := standardInvoice(t)
	p.Lines = []docpayload.Line{
		{
			Position: 1, Description: "Consulting", QuantityThousandths: 2500, Unit: "hour", UnitPriceCents: 9500, VATRatePermyriad: 1900,
			LineDiscount: docpayload.Discount{AmountCents: &lineAmount},
		},
		{Position: 2, Description: "Book", QuantityThousandths: 3000, Unit: "piece", UnitPriceCents: 1999, VATRatePermyriad: 700},
	}
	p.DocumentDiscount = docpayload.Discount{AmountCents: &docAmount}

	return withMoney(t, p)
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

	return withMoney(t, p)
}

// importedInvoice is a historic invoice: no lines, totals from the
// import row, the original PDF referenced.
func importedInvoice(t *testing.T) docpayload.FrozenPayloadV1 {
	t.Helper()

	p := standardInvoice(t)
	p.Imported = true
	p.Lines = nil
	p.TemplateVersion = ""
	p.Number = "R1234"
	p.Original = &docpayload.OriginalDocument{FileID: "6f1c2a4e-0000-4000-8000-000000000002", SHA256: strings.Repeat("a", 64)}

	return p
}

// importedCorrection is a historic correction: it keeps the number it
// was issued under, which follows no suffix rule, and its original was
// never imported, so it has no id.
func importedCorrection(t *testing.T) docpayload.FrozenPayloadV1 {
	t.Helper()

	p := importedInvoice(t)
	p.Type = docpayload.TypeCorrection
	p.Number = "GS-2024-3"
	p.CorrectionSeq = 1
	p.Corrects = &docpayload.DocumentReference{Number: "RE-2024-017", IssueDate: "2024-05-02"}

	return p
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	for name, build := range map[string]func(*testing.T) docpayload.FrozenPayloadV1{
		"standard":         standardInvoice,
		"kleinunternehmer": kleinunternehmerInvoice,
		"mixed":            mixedInvoice,
		"correction":       correctionOf,
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
