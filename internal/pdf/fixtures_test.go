package pdf

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
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

// fixture is one golden case. logo renders it with testdata/logo.png,
// whose SHA-256 the payload names.
type fixture struct {
	name  string
	build func(t *testing.T) docpayload.FrozenPayloadV1
	logo  bool
}

func fixturePath(name string) string {
	return filepath.Join("testdata", "fixtures", name+".json")
}

func goldenPath(name string) string {
	return filepath.Join("testdata", "golden", name+".data.json")
}

// logoPath is the test logo, a 120 by 40 pixel PNG.
var logoPath = filepath.Join("testdata", "logo.png")

func readLogo(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(logoPath)
	if err != nil {
		t.Fatal(err)
	}

	return b
}

func logoSHA256(t *testing.T) string {
	t.Helper()
	sum := sha256.Sum256(readLogo(t))

	return hex.EncodeToString(sum[:])
}

var fixtures = []fixture{
	{name: "invoice-standard-de", build: invoiceStandardDE},
	{name: "invoice-kleinunternehmer-de", build: invoiceKleinunternehmerDE},
	{name: "invoice-line-discount-en", build: invoiceLineDiscountEN},
	{name: "offer-de", build: offerDE},
	{name: "correction-de", build: correctionDE},
	{name: "offer-en", build: offerEN},
	{name: "invoice-kleinunternehmer-en", build: invoiceKleinunternehmerEN},
	{name: "invoice-exempt-de", build: invoiceExemptDE},
	{name: "correction-en", build: correctionEN},
	{name: "offer-kleinunternehmer-de", build: offerKleinunternehmerDE},
	{name: "offer-kleinunternehmer-en", build: offerKleinunternehmerEN},
	{name: "correction-kleinunternehmer-de", build: correctionKleinunternehmerDE},
	{name: "correction-kleinunternehmer-en", build: correctionKleinunternehmerEN},
	{name: "invoice-logo-de", build: invoiceLogoDE, logo: true},
	{name: "offer-de-v1", build: offerDEV1},
}

// correctionEN is the English correction of invoice RE-20260043.
func correctionEN(t *testing.T) docpayload.FrozenPayloadV1 {
	p := correctionDE(t)
	p.Language = docpayload.LanguageEnglish
	p.Number = "RE-20260043-1"
	p.NumberValue = 43
	p.Corrects = &docpayload.DocumentReference{
		ID:        "00000000-0000-4000-8000-000000000043",
		Number:    "RE-20260043",
		IssueDate: "2026-12-07",
	}
	p.Lines[0].Description = "Software development as specified"
	p.Lines[0].Unit = "hours"
	p.PaymentTerms = "Payable within 14 days without deduction."

	return p
}

// offerKleinunternehmerDE is a section 19 UStG offer: no VAT column, no
// VAT row, the section 19 note.
func offerKleinunternehmerDE(t *testing.T) docpayload.FrozenPayloadV1 {
	p := invoiceKleinunternehmerDE(t)
	p.Type = docpayload.TypeOffer
	p.Number = "AN-20260003"
	p.NumberValue = 3
	p.ServicePeriod = nil
	p.DueDate = ""
	p.ValidUntil = "2026-12-20"
	p.Notes = "Wir freuen uns auf Ihre Rückmeldung."

	return p
}

// offerKleinunternehmerEN is the section 19 offer in English.
func offerKleinunternehmerEN(t *testing.T) docpayload.FrozenPayloadV1 {
	p := invoiceKleinunternehmerEN(t)
	p.Type = docpayload.TypeOffer
	p.Number = "AN-20260004"
	p.NumberValue = 4
	p.ServicePeriod = nil
	p.DueDate = ""
	p.ValidUntil = "2026-12-20"
	p.Notes = "We look forward to hearing from you."

	return p
}

// correctionKleinunternehmerDE corrects the section 19 invoice
// RE-20260007: the travel expenses are dropped.
func correctionKleinunternehmerDE(t *testing.T) docpayload.FrozenPayloadV1 {
	p := invoiceKleinunternehmerDE(t)
	p.Type = docpayload.TypeCorrection
	p.Number = "RE-20260007-1"
	p.CorrectionSeq = 1
	p.IssueDate = "2026-11-27"
	p.DueDate = "2026-12-11"
	p.Corrects = &docpayload.DocumentReference{
		ID:        "00000000-0000-4000-8000-000000000007",
		Number:    "RE-20260007",
		IssueDate: "2026-11-20",
	}
	p.Lines = p.Lines[:1]

	return withFigures(t, p)
}

// correctionKleinunternehmerEN is the section 19 correction in English.
func correctionKleinunternehmerEN(t *testing.T) docpayload.FrozenPayloadV1 {
	p := invoiceKleinunternehmerEN(t)
	p.Type = docpayload.TypeCorrection
	p.Number = "RE-20260007-1"
	p.CorrectionSeq = 1
	p.IssueDate = "2026-11-27"
	p.DueDate = "2026-12-11"
	p.Corrects = &docpayload.DocumentReference{
		ID:        "00000000-0000-4000-8000-000000000007",
		Number:    "RE-20260007",
		IssueDate: "2026-11-20",
	}
	p.Lines = p.Lines[:1]

	return withFigures(t, p)
}

// invoiceLogoDE is the section 19 invoice with the seller logo.
func invoiceLogoDE(t *testing.T) docpayload.FrozenPayloadV1 {
	p := invoiceKleinunternehmerDE(t)
	p.Seller.LogoSHA256 = logoSHA256(t)

	return p
}

// offerDEV1 is offerDE frozen with document-v1, which a stored payload
// may name forever.
func offerDEV1(t *testing.T) docpayload.FrozenPayloadV1 {
	p := offerDE(t)
	p.TemplateVersion = "document-v1"

	return p
}

// correctionDE corrects invoice RE-20260042 with a single replaced line.
func correctionDE(t *testing.T) docpayload.FrozenPayloadV1 {
	return withFigures(t, docpayload.FrozenPayloadV1{
		Version:         docpayload.Version1,
		Type:            docpayload.TypeCorrection,
		Language:        docpayload.LanguageGerman,
		VATRegime:       docpayload.RegimeStandard,
		TemplateVersion: CurrentTemplateVersion,
		Currency:        docpayload.CurrencyEUR,
		Number:          "RE-20260042-1",
		NumberValue:     42,
		NumberYear:      2026,
		CorrectionSeq:   1,
		IssueDate:       "2026-12-10",
		ServicePeriod:   &docpayload.ServicePeriod{From: "2026-11-01", To: "2026-11-30"},
		DueDate:         "2026-12-24",
		Seller:          exampleSeller(),
		Customer:        exampleCustomer(),
		Corrects: &docpayload.DocumentReference{
			ID:        "00000000-0000-4000-8000-000000000042",
			Number:    "RE-20260042",
			IssueDate: "2026-12-05",
		},
		Lines: []docpayload.Line{
			{Position: 1, Description: "Softwareentwicklung laut Leistungsbeschreibung", QuantityThousandths: 120000, Unit: "Std.", UnitPriceCents: 9500, VATRatePermyriad: 1900},
		},
		PaymentTerms: "Zahlbar innerhalb von 14 Tagen ohne Abzug.",
	})
}

// offerEN is an English offer with a document discount amount.
func offerEN(t *testing.T) docpayload.FrozenPayloadV1 {
	return withFigures(t, docpayload.FrozenPayloadV1{
		Version:         docpayload.Version1,
		Type:            docpayload.TypeOffer,
		Language:        docpayload.LanguageEnglish,
		VATRegime:       docpayload.RegimeStandard,
		TemplateVersion: CurrentTemplateVersion,
		Currency:        docpayload.CurrencyEUR,
		Number:          "AN-20260013",
		NumberValue:     13,
		NumberYear:      2026,
		IssueDate:       "2026-11-20",
		ValidUntil:      "2027-01-15",
		Seller:          exampleSeller(),
		Customer:        exampleCustomer(),
		Lines: []docpayload.Line{
			{Position: 1, Description: "Kubernetes cluster setup", QuantityThousandths: 1000, Unit: "flat", UnitPriceCents: 380000, VATRatePermyriad: 1900},
			{Position: 2, Description: "Training, two days", QuantityThousandths: 2000, Unit: "days", UnitPriceCents: 120000, VATRatePermyriad: 1900},
		},
		DocumentDiscount: docpayload.Discount{AmountCents: amount(20000)},
		Notes:            "We look forward to hearing from you.",
	})
}

// invoiceKleinunternehmerEN is the section 19 invoice in English.
func invoiceKleinunternehmerEN(t *testing.T) docpayload.FrozenPayloadV1 {
	p := invoiceKleinunternehmerDE(t)
	p.Language = docpayload.LanguageEnglish
	p.Lines[0].Description = "Kubernetes basics workshop"
	p.Lines[0].Unit = "day"
	p.Lines[1].Description = "Travel expenses, flat rate"
	p.Lines[1].Unit = "flat"
	p.TaxNote = "No VAT is charged under section 19 of the German VAT Act (Kleinunternehmerregelung)."
	p.PaymentTerms = "Payable within 14 days without deduction."

	return p
}

// invoiceExemptDE is a standard regime invoice with a tax exempt line
// and the exemption note.
func invoiceExemptDE(t *testing.T) docpayload.FrozenPayloadV1 {
	return withFigures(t, docpayload.FrozenPayloadV1{
		Version:         docpayload.Version1,
		Type:            docpayload.TypeInvoice,
		Language:        docpayload.LanguageGerman,
		VATRegime:       docpayload.RegimeStandard,
		TemplateVersion: CurrentTemplateVersion,
		Currency:        docpayload.CurrencyEUR,
		Number:          "RE-20260044",
		NumberValue:     44,
		NumberYear:      2026,
		IssueDate:       "2026-12-08",
		ServicePeriod:   &docpayload.ServicePeriod{From: "2026-12-01", To: "2026-12-01"},
		DueDate:         "2026-12-22",
		Seller:          exampleSeller(),
		Customer:        exampleCustomer(),
		Lines: []docpayload.Line{
			{Position: 1, Description: "Prüfungsvorbereitung IHK, Unterricht", QuantityThousandths: 6000, Unit: "Std.", UnitPriceCents: 6000},
			{Position: 2, Description: "Lernmaterial", QuantityThousandths: 1000, Unit: "Stück", UnitPriceCents: 4900, VATRatePermyriad: 1900},
		},
		TaxNote:      "Die Unterrichtsleistung ist nach § 4 Nr. 21 UStG steuerfrei.",
		PaymentTerms: "Zahlbar innerhalb von 14 Tagen ohne Abzug.",
	})
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
