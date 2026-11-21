package docpayload_test

import (
	"strings"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
)

// TestDocumentV1LimitsArePinned pins the limits of document-v1. They must
// equal internal/pdf's documentV1Limits (issue #74): a change here
// without the same change in the renderer lets issuing freeze a payload
// the renderer refuses, or refuses one it would print.
func TestDocumentV1LimitsArePinned(t *testing.T) {
	t.Parallel()

	got := map[string]int{
		"MaxLines":           docpayload.MaxLines,
		"MaxLineChars":       docpayload.MaxLineChars,
		"MaxUnitChars":       docpayload.MaxUnitChars,
		"MaxSellerChars":     docpayload.MaxSellerChars,
		"MaxFooterTextChars": docpayload.MaxFooterTextChars,
		"MaxFooterTextLines": docpayload.MaxFooterTextLines,
		"MaxTextChars":       docpayload.MaxTextChars,
		"MaxTextLines":       docpayload.MaxTextLines,
		"MaxDataBytes":       docpayload.MaxDataBytes,
	}
	want := map[string]int{
		"MaxLines":           1000,
		"MaxLineChars":       200,
		"MaxUnitChars":       16,
		"MaxSellerChars":     80,
		"MaxFooterTextChars": 300,
		"MaxFooterTextLines": 4,
		"MaxTextChars":       1000,
		"MaxTextLines":       20,
		"MaxDataBytes":       2 << 20,
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s = %d, want %d (internal/pdf document-v1)", name, got[name], w)
		}
	}
	if docpayload.TemplateDocumentV1 != "document-v1" {
		t.Errorf("TemplateDocumentV1 = %q, want document-v1", docpayload.TemplateDocumentV1)
	}
}

func TestRuneProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		r         rune
		multiline bool
		ok        bool
	}{
		{"ascii letter", 'A', false, true},
		{"umlaut", 'ä', false, true},
		{"sharp s", 'ß', false, true},
		{"section sign", '§', false, true},
		{"euro sign", '€', false, true},
		{"en dash", '–', false, true},
		{"cyrillic", 'Ж', false, true},
		{"greek", 'Ω', false, true},
		{"line feed in a line field", '\n', false, false},
		{"line feed in a text field", '\n', true, true},
		{"tab in a text field", '\t', true, true},
		{"nul", 0, true, false},
		{"escape", 0x1B, true, false},
		{"c1 control", 0x85, true, false},
		{"right to left override", 0x202E, false, false},
		{"first strong isolate", 0x2068, false, false},
		{"replacement character", 0xFFFD, false, false},
		{"emoji", 0x1F600, false, false},
		{"cjk", '漢', false, false},
		{"arabic", 'ب', false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reason := docpayload.RuneProblem(tt.r, tt.multiline)
			if (reason == "") != tt.ok {
				t.Errorf("RuneProblem(%U, %v) = %q, want ok %v", tt.r, tt.multiline, reason, tt.ok)
			}
		})
	}
}

func TestCheckPrintable(t *testing.T) {
	t.Parallel()

	long := func(n int) string { return strings.Repeat("a", n) }
	lines := func(n int) string { return strings.TrimSuffix(strings.Repeat("a\n", n), "\n") }

	tests := []struct {
		name   string
		mutate func(p *docpayload.FrozenPayloadV1)
		want   string
	}{
		{"complete", func(*docpayload.FrozenPayloadV1) {}, ""},
		{"customer name at the limit", func(p *docpayload.FrozenPayloadV1) { p.Customer.Name = long(200) }, ""},
		{"customer name over the limit", func(p *docpayload.FrozenPayloadV1) { p.Customer.Name = long(201) }, "customer.name"},
		{"customer name with an emoji", func(p *docpayload.FrozenPayloadV1) { p.Customer.Name = "Shop \U0001F600" }, "customer.name"},
		{"customer street with a bidi override", func(p *docpayload.FrozenPayloadV1) { p.Customer.Address.Street = "Main " + string(rune(0x202E)) + "1" }, "customer.address.street"},
		{"customer reference with a line feed", func(p *docpayload.FrozenPayloadV1) { p.CustomerReference = "PO\n7" }, "customer_reference"},
		{"seller field at the limit", func(p *docpayload.FrozenPayloadV1) { p.Seller.Website = long(80) }, ""},
		{"seller field over the limit", func(p *docpayload.FrozenPayloadV1) { p.Seller.Website = long(81) }, "seller.website"},
		{"seller name over the limit", func(p *docpayload.FrozenPayloadV1) { p.Seller.LegalName = long(81) }, "seller.legal_name"},
		{"footer text over its lines", func(p *docpayload.FrozenPayloadV1) { p.Seller.FooterText = lines(5) }, "seller.footer_text"},
		{"footer text at its lines", func(p *docpayload.FrozenPayloadV1) { p.Seller.FooterText = lines(4) }, ""},
		{"description at the limit", func(p *docpayload.FrozenPayloadV1) { p.Lines[0].Description = long(1000) }, ""},
		{"description over the limit", func(p *docpayload.FrozenPayloadV1) { p.Lines[1].Description = long(1001) }, "lines[1].description"},
		{"description with 20 lines", func(p *docpayload.FrozenPayloadV1) { p.Lines[0].Description = lines(20) }, ""},
		{"description with 21 lines", func(p *docpayload.FrozenPayloadV1) { p.Lines[0].Description = lines(21) }, "lines[0].description"},
		{"trailing line feeds are not lines", func(p *docpayload.FrozenPayloadV1) { p.Lines[0].Description = lines(20) + "\n\n\n" }, ""},
		{"unit over the limit", func(p *docpayload.FrozenPayloadV1) { p.Lines[0].Unit = long(17) }, "lines[0].unit"},
		{"notes with a control character", func(p *docpayload.FrozenPayloadV1) { p.Notes = "a\x07b" }, "notes"},
		{"payment terms over the limit", func(p *docpayload.FrozenPayloadV1) { p.PaymentTerms = long(1001) }, "payment_terms"},
		{"tax note over its lines", func(p *docpayload.FrozenPayloadV1) { p.TaxNote = lines(21) }, "tax_note"},
		{"invalid utf-8", func(p *docpayload.FrozenPayloadV1) { p.Customer.Address.City = "Berlin\xff" }, "customer.address.city"},
		{"too much data although every field is in its limit", func(p *docpayload.FrozenPayloadV1) {
			l := p.Lines[0]
			l.Description = strings.Repeat("Ж", docpayload.MaxTextChars)
			p.Lines = make([]docpayload.Line, docpayload.MaxLines)
			for i := range p.Lines {
				p.Lines[i] = l
			}
		}, "lines"},
		{"too many lines", func(p *docpayload.FrozenPayloadV1) {
			l := p.Lines[0]
			p.Lines = make([]docpayload.Line, docpayload.MaxLines+1)
			for i := range p.Lines {
				p.Lines[i] = l
			}
		}, "lines"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := standardInvoice(t)
			tt.mutate(&p)
			got := docpayload.CheckPrintable(p)
			if tt.want == "" {
				if len(got) != 0 {
					t.Fatalf("CheckPrintable() = %v, want no problem", got)
				}

				return
			}
			if _, ok := got[tt.want]; !ok {
				t.Fatalf("CheckPrintable() = %v, want a problem at %q", got, tt.want)
			}
		})
	}
}

func TestCheckPrintableSkipsAnImportedPayload(t *testing.T) {
	t.Parallel()

	p := standardInvoice(t)
	p.Imported = true
	p.Customer.Name = "Shop \U0001F600"
	if got := docpayload.CheckPrintable(p); len(got) != 0 {
		t.Fatalf("CheckPrintable(imported) = %v, want nothing: an imported document is never rendered", got)
	}
}
