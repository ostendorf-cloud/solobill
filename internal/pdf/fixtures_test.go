package pdf

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/money"
)

// The fixtures are built here, their figures computed by internal/money
// as the issuing code computes them, and stored as payload JSON under
// testdata/fixtures. Every name and number in them is fictional.

func pct(p int32) *int32 { return &p }

func amount(c int64) *int64 { return &c }

func exampleSeller() docpayload.SellerSnapshot {
	return docpayload.SellerSnapshot{
		LegalName: "Example GmbH",
		Address: docpayload.Address{
			Street:      "Musterstraße 1",
			PostalCode:  "12345",
			City:        "Musterstadt",
			CountryCode: "DE",
		},
		TaxNumber:  "12/345/67890",
		VATID:      "DE000000000",
		BankName:   "Musterbank",
		IBAN:       "DE00 0000 0000 0000 0000 00",
		BIC:        "MUSTDEXXXXX",
		Email:      "rechnung@example.com",
		Phone:      "+49 000 0000000",
		Website:    "www.example.com",
		FooterText: "Geschäftsführer: Max Mustermann · Amtsgericht Musterstadt HRB 00000",
	}
}

func exampleCustomer() docpayload.CustomerSnapshot {
	return docpayload.CustomerSnapshot{
		ID:     "00000000-0000-4000-8000-000000000001",
		Number: "K-1001",
		Name:   "Mustermann Consulting GmbH",
		Address: docpayload.Address{
			Street:      "Beispielweg 7",
			Line2:       "z. Hd. Max Mustermann",
			PostalCode:  "54321",
			City:        "Beispielstadt",
			CountryCode: "DE",
		},
	}
}

// withFigures runs internal/money over the payload's lines and copies
// every figure in, as the issue endpoint does.
func withFigures(t *testing.T, p docpayload.FrozenPayloadV1) docpayload.FrozenPayloadV1 {
	t.Helper()
	in := make([]money.Line, len(p.Lines))
	for i, l := range p.Lines {
		in[i] = money.Line{
			QuantityThousandths: money.Thousandths(l.QuantityThousandths),
			UnitPriceCents:      money.Cents(l.UnitPriceCents),
			VATRatePermyriad:    money.Permyriad(l.VATRatePermyriad),
			Discount:            toMoneyDiscount(l.LineDiscount),
		}
	}
	res, err := money.Calculate(in, toMoneyDiscount(p.DocumentDiscount), money.VATRegime(p.VATRegime))
	if err != nil {
		t.Fatalf("calculate: %v", err)
	}
	if err := docpayload.ApplyLineResults(p.Lines, res.Lines); err != nil {
		t.Fatalf("apply: %v", err)
	}
	p.Totals = docpayload.TotalsFromMoney(res)

	return p
}

func toMoneyDiscount(d docpayload.Discount) money.Discount {
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

// fixture is one golden case.
type fixture struct {
	name  string
	build func(t *testing.T) docpayload.FrozenPayloadV1
}

func fixturePath(name string) string {
	return filepath.Join("testdata", "fixtures", name+".json")
}

func goldenPath(name string) string {
	return filepath.Join("testdata", "golden", name+".data.json")
}

var fixtures = []fixture{
	{name: "invoice-standard-de", build: invoiceStandardDE},
	{name: "invoice-kleinunternehmer-de", build: invoiceKleinunternehmerDE},
	{name: "invoice-line-discount-en", build: invoiceLineDiscountEN},
	{name: "offer-de", build: offerDE},
}

// invoiceStandardDE is a normal invoice under the standard regime with
// two VAT rates and enough lines to continue on a second page.
func invoiceStandardDE(t *testing.T) docpayload.FrozenPayloadV1 {
	var lines []docpayload.Line
	for i := range 28 {
		lines = append(lines, docpayload.Line{
			Position:            int32(i + 1),
			Description:         fmt.Sprintf("Softwareentwicklung Modul %d\nUmsetzung laut Leistungsbeschreibung", i+1),
			QuantityThousandths: int64(4000 + 500*(i%5)),
			Unit:                "Std.",
			UnitPriceCents:      9500,
			VATRatePermyriad:    1900,
		})
	}
	lines = append(lines,
		docpayload.Line{
			Position: 29, Description: "Fachbuch Betriebssysteme", QuantityThousandths: 2000,
			Unit: "Stück", UnitPriceCents: 3990, VATRatePermyriad: 700,
		},
		docpayload.Line{
			Position: 30, Description: "Lizenz Entwicklungsumgebung, 12 Monate", QuantityThousandths: 1000,
			Unit: "Stück", UnitPriceCents: 123456, VATRatePermyriad: 1900,
		},
	)

	return withFigures(t, docpayload.FrozenPayloadV1{
		Version:           docpayload.Version1,
		Type:              docpayload.TypeInvoice,
		Language:          docpayload.LanguageGerman,
		VATRegime:         docpayload.RegimeStandard,
		TemplateVersion:   CurrentTemplateVersion,
		Currency:          docpayload.CurrencyEUR,
		Number:            "RE-20260042",
		NumberValue:       42,
		NumberYear:        2026,
		IssueDate:         "2026-12-05",
		ServicePeriod:     &docpayload.ServicePeriod{From: "2026-11-01", To: "2026-11-30"},
		DueDate:           "2026-12-19",
		Seller:            exampleSeller(),
		Customer:          exampleCustomer(),
		CustomerReference: "Bestellung 4711",
		Lines:             lines,
		PaymentTerms:      "Zahlbar innerhalb von 14 Tagen ohne Abzug.",
		Notes:             "Vielen Dank für Ihren Auftrag.",
	})
}

// invoiceKleinunternehmerDE is a section 19 UStG invoice: no VAT, the
// exemption note, a single service date.
func invoiceKleinunternehmerDE(t *testing.T) docpayload.FrozenPayloadV1 {
	seller := exampleSeller()
	seller.VATID = ""
	seller.FooterText = ""
	seller.LegalName = "Max Mustermann IT-Beratung"
	customer := exampleCustomer()
	customer.Address.Line2 = ""

	return withFigures(t, docpayload.FrozenPayloadV1{
		Version:         docpayload.Version1,
		Type:            docpayload.TypeInvoice,
		Language:        docpayload.LanguageGerman,
		VATRegime:       docpayload.RegimeKleinunternehmer,
		TemplateVersion: CurrentTemplateVersion,
		Currency:        docpayload.CurrencyEUR,
		Number:          "RE-20260007",
		NumberValue:     7,
		NumberYear:      2026,
		IssueDate:       "2026-11-20",
		ServicePeriod:   &docpayload.ServicePeriod{From: "2026-11-18", To: "2026-11-18"},
		DueDate:         "2026-12-04",
		Seller:          seller,
		Customer:        customer,
		Lines: []docpayload.Line{
			{Position: 1, Description: "Workshop Kubernetes Grundlagen", QuantityThousandths: 1000, Unit: "Tag", UnitPriceCents: 120000},
			{Position: 2, Description: "Reisekosten pauschal", QuantityThousandths: 1000, Unit: "pauschal", UnitPriceCents: 8550},
		},
		TaxNote:      "Gemäß § 19 UStG wird keine Umsatzsteuer berechnet.",
		PaymentTerms: "Zahlbar innerhalb von 14 Tagen ohne Abzug.",
	})
}

// invoiceLineDiscountEN is an English reverse charge invoice to an
// Austrian business with line discounts and a document discount.
func invoiceLineDiscountEN(t *testing.T) docpayload.FrozenPayloadV1 {
	customer := docpayload.CustomerSnapshot{
		ID:     "00000000-0000-4000-8000-000000000002",
		Number: "K-2002",
		Name:   "Beispiel Handels GmbH",
		Address: docpayload.Address{
			Street:      "Musterplatz 3",
			PostalCode:  "1010",
			City:        "Wien",
			CountryCode: "AT",
		},
		VATID: "ATU00000000",
	}

	return withFigures(t, docpayload.FrozenPayloadV1{
		Version:           docpayload.Version1,
		Type:              docpayload.TypeInvoice,
		Language:          docpayload.LanguageEnglish,
		VATRegime:         docpayload.RegimeStandard,
		TemplateVersion:   CurrentTemplateVersion,
		Currency:          docpayload.CurrencyEUR,
		Number:            "RE-20260043",
		NumberValue:       43,
		NumberYear:        2026,
		IssueDate:         "2026-12-07",
		ServicePeriod:     &docpayload.ServicePeriod{From: "2026-11-01", To: "2026-11-30"},
		DueDate:           "2026-12-21",
		Seller:            exampleSeller(),
		Customer:          customer,
		CustomerReference: "PO 2026-118",
		Lines: []docpayload.Line{
			{
				Position: 1, Description: "Cloud architecture review", QuantityThousandths: 12500,
				Unit: "hours", UnitPriceCents: 11000, LineDiscount: docpayload.Discount{PercentPermyriad: pct(1000)},
			},
			{
				Position: 2, Description: "Platform migration, fixed price", QuantityThousandths: 1000,
				Unit: "flat", UnitPriceCents: 450000, LineDiscount: docpayload.Discount{AmountCents: amount(25000)},
			},
			{Position: 3, Description: "Operations handbook", QuantityThousandths: 1000, Unit: "flat", UnitPriceCents: 80000},
		},
		DocumentDiscount: docpayload.Discount{PercentPermyriad: pct(300)},
		TaxNote:          "Reverse charge: the recipient is liable for VAT (Article 196 Council Directive 2006/112/EC).",
		PaymentTerms:     "Payable within 14 days without deduction.",
	})
}

// offerDE is an offer: validity date, no payment terms, no service
// period.
func offerDE(t *testing.T) docpayload.FrozenPayloadV1 {
	return withFigures(t, docpayload.FrozenPayloadV1{
		Version:         docpayload.Version1,
		Type:            docpayload.TypeOffer,
		Language:        docpayload.LanguageGerman,
		VATRegime:       docpayload.RegimeStandard,
		TemplateVersion: CurrentTemplateVersion,
		Currency:        docpayload.CurrencyEUR,
		Number:          "AN-20260012",
		NumberValue:     12,
		NumberYear:      2026,
		IssueDate:       "2026-11-20",
		ValidUntil:      "2026-12-31",
		Seller:          exampleSeller(),
		Customer:        exampleCustomer(),
		Lines: []docpayload.Line{
			{Position: 1, Description: "Einrichtung Monitoring\nPrometheus, Alertmanager und Grafana", QuantityThousandths: 1000, Unit: "pauschal", UnitPriceCents: 240000, VATRatePermyriad: 1900},
			{Position: 2, Description: "Betreuung pro Monat", QuantityThousandths: 12000, Unit: "Monat", UnitPriceCents: 35000, VATRatePermyriad: 1900},
		},
		// An offer prints no payment terms even when the payload has them.
		PaymentTerms: "Zahlbar innerhalb von 14 Tagen.",
		Notes:        "Wir freuen uns auf Ihre Rückmeldung.",
	})
}
