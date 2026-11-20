package docpayload_test

import (
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
)

type build func(*testing.T) docpayload.FrozenPayloadV1

func TestCompletePayloadsValidate(t *testing.T) {
	t.Parallel()

	for name, b := range map[string]build{
		"standard":            standardInvoice,
		"kleinunternehmer":    kleinunternehmerInvoice,
		"two rates, amounts":  mixedInvoice,
		"correction":          correctionOf,
		"imported":            importedInvoice,
		"imported correction": importedCorrection,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if problems := docpayload.Validate(b(t)); len(problems) != 0 {
				t.Errorf("Validate() = %v, want no problems", docpayload.SortedPaths(problems))
			}
		})
	}

	offer := standardInvoice(t)
	offer.Type = docpayload.TypeOffer
	offer.ServicePeriod = nil
	offer.Seller.TaxNumber = ""
	offer.ValidUntil = "2026-12-31"
	if problems := docpayload.Validate(offer); len(problems) != 0 {
		t.Errorf("Validate(offer) = %v, want none", docpayload.SortedPaths(problems))
	}
}

// TestValidateReports breaks one thing at a time and expects that field
// reported. Each case names the rule it guards.
func TestValidateReports(t *testing.T) {
	t.Parallel()

	p := func(f func(*docpayload.FrozenPayloadV1)) func(*docpayload.FrozenPayloadV1) { return f }

	tests := []struct {
		name   string
		build  build
		mutate func(*docpayload.FrozenPayloadV1)
		want   string
	}{
		// Section 14 Abs. 4 Nr. 1 to 8 UStG.
		{"Nr. 1 seller name", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Seller.LegalName = "" }), "seller.legal_name"},
		{"Nr. 1 seller address", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Seller.Address.City = "" }), "seller.address.city"},
		{"Nr. 1 customer name", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Customer.Name = "" }), "customer.name"},
		{"Nr. 1 customer address", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Customer.Address.Street = "" }), "customer.address.street"},
		{"Nr. 2 tax number", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Seller.TaxNumber = "" }), "seller.tax_number"},
		{"Nr. 2 Kleinunternehmer id is not enough under the standard regime", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) {
			p.Seller.TaxNumber = ""
			p.Seller.KleinunternehmerID = "DE-KU-0000000"
		}), "seller.tax_number"},
		{"Nr. 3 issue date", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.IssueDate = "01.12.2026" }), "issue_date"},
		{"Nr. 4 number", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Number = "" }), "number"},
		{"Nr. 5 line description", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Lines[1].Description = "" }), "lines[1].description"},
		{"Nr. 5 line quantity", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Lines[0].QuantityThousandths = 0 }), "lines[0].quantity_thousandths"},
		{"Nr. 5 no lines", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Lines = nil }), "lines"},
		{"Nr. 6 service period", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.ServicePeriod = nil }), "service_period"},
		{"Nr. 6 service period from after to", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) {
			p.ServicePeriod = &docpayload.ServicePeriod{From: "2026-12-31", To: "2026-11-01"}
		}), "service_period"},
		{"Nr. 7 and 8 VAT buckets", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Totals.VAT = nil }), "totals.vat"},
		{"Nr. 8 exemption note for a 0 percent line", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) {
			p.Totals.VAT = append(p.Totals.VAT, docpayload.VATBucket{RatePermyriad: 0})
		}), "tax_note"},
		// Section 34a UStDV.
		{"34a note", kleinunternehmerInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.TaxNote = "" }), "tax_note"},
		{"34a tax identity", kleinunternehmerInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Seller.KleinunternehmerID = "" }), "seller.tax_number"},
		{"34a no VAT on a line", kleinunternehmerInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Lines[0].VATRatePermyriad = 1900 }), "lines[0].vat_rate_permyriad"},
		// Shape.
		{"currency", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Currency = "USD" }), "currency"},
		{"unknown language", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Language = "fr" }), "language"},
		{"template version", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.TemplateVersion = "" }), "template_version"},
		{"valid until on an invoice", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.ValidUntil = "2026-12-31" }), "valid_until"},
		// Corrections.
		{"invoice must not reference an original", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) {
			p.Corrects = &docpayload.DocumentReference{ID: "x", Number: "RE-1", IssueDate: "2026-01-01"}
		}), "corrects"},
		{"invoice has no sequence", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.CorrectionSeq = 1 }), "correction_seq"},
		{"correction needs its original", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Type = docpayload.TypeCorrection }), "corrects"},
		{"correction original id", correctionOf, p(func(p *docpayload.FrozenPayloadV1) { p.Corrects.ID = "" }), "corrects.id"},
		{"correction original number", correctionOf, p(func(p *docpayload.FrozenPayloadV1) { p.Corrects.Number = "" }), "corrects.number"},
		{"correction original issue date", correctionOf, p(func(p *docpayload.FrozenPayloadV1) { p.Corrects.IssueDate = "05.12.2026" }), "corrects.issue_date"},
		{"correction needs a sequence", correctionOf, p(func(p *docpayload.FrozenPayloadV1) { p.CorrectionSeq = 0 }), "correction_seq"},
		{"correction number carries the suffix", correctionOf, p(func(p *docpayload.FrozenPayloadV1) { p.Number = "RE-20260018" }), "number"},
		{"imported correction needs a sequence too", importedCorrection, p(func(p *docpayload.FrozenPayloadV1) { p.CorrectionSeq = 0 }), "correction_seq"},
		// Recomputed through internal/money.
		{"line net after line discount", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Lines[0].NetAfterLineDiscountCents++ }), "lines[0].net_after_line_discount_cents"},
		{"line document discount share", mixedInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Lines[1].DocumentDiscountShareCents++ }), "lines[1].document_discount_share_cents"},
		{"line net after all discounts", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Lines[1].NetAfterAllDiscountsCents++ }), "lines[1].net_after_all_discounts_cents"},
		{"net total", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Totals.NetTotalCents++ }), "totals.net_total_cents"},
		{"document discount", mixedInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Totals.DocumentDiscountCents++ }), "totals.document_discount_cents"},
		{"net after document discount", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Totals.NetAfterDocumentDiscountCents++ }), "totals.net_after_document_discount_cents"},
		{"bucket rate", mixedInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Totals.VAT[0].RatePermyriad = 1000 }), "totals.vat[0].rate_permyriad"},
		{"bucket net", mixedInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Totals.VAT[1].NetCents++ }), "totals.vat[1].net_cents"},
		{"bucket VAT", mixedInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Totals.VAT[0].VATCents++ }), "totals.vat[0].vat_cents"},
		{"VAT total", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Totals.VATTotalCents++ }), "totals.vat_total_cents"},
		{"grand total", standardInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Totals.GrandTotalCents++ }), "totals.grand_total_cents"},
		{"34a VAT total is 0", kleinunternehmerInvoice, p(func(p *docpayload.FrozenPayloadV1) {
			p.Totals.VATTotalCents = 3612
			p.Totals.GrandTotalCents += 3612
		}), "totals.vat_total_cents"},
		// Arithmetic of an imported document, which is not recomputed.
		{"imported needs its original", importedInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Original = nil }), "original"},
		{"imported net after discount", importedInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Totals.NetAfterDocumentDiscountCents++ }), "totals.net_after_document_discount_cents"},
		{"imported bucket net sum", importedInvoice, p(func(p *docpayload.FrozenPayloadV1) {
			p.Totals.VAT = append(p.Totals.VAT, docpayload.VATBucket{RatePermyriad: 700, NetCents: 100, VATCents: 7})
			p.Totals.VATTotalCents += 7
			p.Totals.GrandTotalCents += 7
		}), "totals.vat"},
		{"imported bucket VAT rounded once", importedInvoice, p(func(p *docpayload.FrozenPayloadV1) {
			p.Totals.VAT[0].VATCents++
			p.Totals.VATTotalCents++
			p.Totals.GrandTotalCents++
		}), "totals.vat[0].vat_cents"},
		{"imported VAT total", importedInvoice, p(func(p *docpayload.FrozenPayloadV1) {
			p.Totals.VATTotalCents++
			p.Totals.GrandTotalCents++
		}), "totals.vat_total_cents"},
		{"imported grand total", importedInvoice, p(func(p *docpayload.FrozenPayloadV1) { p.Totals.GrandTotalCents++ }), "totals.grand_total_cents"},
		{"imported 34a VAT total is 0", importedInvoice, p(func(p *docpayload.FrozenPayloadV1) {
			p.VATRegime = docpayload.RegimeKleinunternehmer
			p.Seller.KleinunternehmerID = "DE-KU-0000000"
		}), "totals.vat_total_cents"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			payload := tt.build(t)
			tt.mutate(&payload)
			problems := docpayload.Validate(payload)
			if _, ok := problems[tt.want]; !ok {
				t.Errorf("Validate() = %v, want %q among them", docpayload.SortedPaths(problems), tt.want)
			}
		})
	}
}
