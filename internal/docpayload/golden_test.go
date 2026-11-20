package docpayload_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
)

var update = flag.Bool("update", false, "rewrite the golden file from the current type")

// goldenPath pins every JSON field name of version 1. A stored payload
// is read forever, so a renamed, removed or added field fails here
// first. Regenerating it with -update is a deliberate change of the
// contract and needs a new payload version instead.
var goldenPath = filepath.Join("testdata", "frozen_payload_v1.golden.json")

// everyField sets every field of FrozenPayloadV1 to a non zero value, so
// no omitempty field is missing from the golden file. It is a shape
// fixture, not a valid document.
func everyField() docpayload.FrozenPayloadV1 {
	percent := int32(1000)
	amount := int64(500)

	return docpayload.FrozenPayloadV1{
		Version:         docpayload.Version1,
		Type:            docpayload.TypeCorrection,
		Language:        docpayload.LanguageEnglish,
		VATRegime:       docpayload.RegimeStandard,
		TemplateVersion: "invoice-v1",
		Currency:        docpayload.CurrencyEUR,
		Number:          "RE-20260009-1",
		NumberValue:     9,
		NumberYear:      2026,
		CorrectionSeq:   1,
		IssueDate:       "2026-12-05",
		ServicePeriod:   &docpayload.ServicePeriod{From: "2026-11-01", To: "2026-11-30"},
		DueDate:         "2026-12-19",
		ValidUntil:      "2026-12-31",
		Seller: docpayload.SellerSnapshot{
			LegalName:          "Example Trading",
			Address:            docpayload.Address{Street: "Main Street 1", Line2: "Building B", PostalCode: "12345", City: "Example City", CountryCode: "DE"},
			TaxNumber:          "12/345/67890",
			VATID:              "DE000000000",
			KleinunternehmerID: "DE-KU-0000000",
			BankName:           "Example Bank",
			IBAN:               "DE00000000000000000000",
			BIC:                "EXAMPLEXXX",
			Email:              "office@example.test",
			Phone:              "+49 000 000000",
			Website:            "https://example.test",
			FooterText:         "Footer",
			LogoSHA256:         "aa",
		},
		Customer: docpayload.CustomerSnapshot{
			ID:      "6f1c2a4e-0000-4000-8000-000000000001",
			Number:  "K-0042",
			Name:    "Customer Ltd",
			Address: docpayload.Address{Street: "Side Street 2", Line2: "c/o Accounts", PostalCode: "54321", City: "Other City", CountryCode: "DE"},
			VATID:   "DE111111111",
		},
		CustomerReference: "PO-7",
		Corrects:          &docpayload.DocumentReference{ID: "6f1c2a4e-0000-4000-8000-000000000003", Number: "RE-20260009", IssueDate: "2026-12-01"},
		Lines: []docpayload.Line{{
			Position:                   1,
			Description:                "Consulting",
			QuantityThousandths:        1000,
			Unit:                       "hour",
			UnitPriceCents:             10000,
			VATRatePermyriad:           1900,
			LineDiscount:               docpayload.Discount{PercentPermyriad: &percent, AmountCents: &amount},
			NetAfterLineDiscountCents:  9000,
			DocumentDiscountShareCents: 100,
			NetAfterAllDiscountsCents:  8900,
		}},
		DocumentDiscount: docpayload.Discount{PercentPermyriad: &percent, AmountCents: &amount},
		Totals: docpayload.Totals{
			NetTotalCents:                 9000,
			DocumentDiscountCents:         100,
			NetAfterDocumentDiscountCents: 8900,
			VAT:                           []docpayload.VATBucket{{RatePermyriad: 1900, NetCents: 8900, VATCents: 1691}},
			VATTotalCents:                 1691,
			GrandTotalCents:               10591,
		},
		TaxNote:      "Note",
		PaymentTerms: "14 days",
		Notes:        "Thanks",
		Imported:     true,
		Original:     &docpayload.OriginalDocument{FileID: "6f1c2a4e-0000-4000-8000-000000000002", SHA256: "bb"},
	}
}

func TestGoldenFieldNames(t *testing.T) {
	t.Parallel()

	data, err := docpayload.Marshal(everyField())
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var got bytes.Buffer
	if err := json.Indent(&got, data, "", "  "); err != nil {
		t.Fatalf("indent: %v", err)
	}
	got.WriteByte('\n')

	if *update {
		if err := os.WriteFile(goldenPath, got.Bytes(), 0o600); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Errorf("FrozenPayloadV1 JSON differs from %s; a version 1 field must never change\n got: %s\nwant: %s", goldenPath, got.Bytes(), want)
	}

	if _, err := docpayload.Unmarshal(want); err != nil {
		t.Errorf("Unmarshal(golden) error = %v", err)
	}
}
