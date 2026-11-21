package docpayload

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The limits of template version TemplateDocumentV1. They must equal
// internal/pdf's documentV1Limits (issue #74): the renderer refuses a
// payload outside them, and a frozen payload is never changed again, so
// issuing refuses it first. A version's limits are never tightened; a
// tighter limit is a new template version. TestDocumentV1LimitsArePinned
// pins the values.
const (
	// MaxLines is the most item lines.
	MaxLines = 1000
	// MaxLineChars bounds a single line field: the number, a customer
	// field, the customer reference, the corrected number.
	MaxLineChars = 200
	// MaxUnitChars bounds a line's unit.
	MaxUnitChars = 16
	// MaxSellerChars bounds a seller field printed in the footer.
	MaxSellerChars = 80
	// MaxFooterTextChars and MaxFooterTextLines bound the seller's footer
	// text.
	MaxFooterTextChars = 300
	MaxFooterTextLines = 4
	// MaxTextChars and MaxTextLines bound a free text field: a line's
	// description, the tax note, the payment terms and the notes.
	MaxTextChars = 1000
	MaxTextLines = 20
	// MaxDataBytes bounds the template data the renderer writes for
	// Typst (internal/pdf's DataBytes, 2 MiB). CheckPrintable applies it
	// to the payload's own JSON, a close lower bound of that data; the
	// renderer's checker (docissue.PayloadChecker) enforces the exact
	// limit on the data it builds.
	MaxDataBytes = 2 << 20
)

// printableRanges are the runes both bundled fonts of TemplateDocumentV1
// (Go Regular and Go Bold, internal/pdf/fonts) have a glyph for, from
// U+0020 on. Computed from the two font files of PR #91 with
// golang.org/x/image/font/sfnt (GlyphIndex not 0 in both). Control
// characters and U+FFFD are refused before this table is consulted. The
// table must stay equal to the renderer's coverage: a rune outside it
// cannot be printed, a rune the fonts cover but this table lacks only
// refuses more than the renderer would.
var printableRanges = [...][2]rune{
	{0x0020, 0x007E},
	{0x00A0, 0x017F},
	{0x0192, 0x0192},
	{0x01CD, 0x01DC},
	{0x01FA, 0x01FF},
	{0x0218, 0x021B},
	{0x02C6, 0x02C7},
	{0x02C9, 0x02C9},
	{0x02D8, 0x02DD},
	{0x037E, 0x037E},
	{0x0384, 0x038A},
	{0x038C, 0x038C},
	{0x038E, 0x03A1},
	{0x03A3, 0x03CE},
	{0x0400, 0x045F},
	{0x0490, 0x0491},
	{0x1E80, 0x1E85},
	{0x1EF2, 0x1EF3},
	{0x2013, 0x2015},
	{0x2017, 0x201E},
	{0x2020, 0x2022},
	{0x2026, 0x2026},
	{0x2030, 0x2030},
	{0x2032, 0x2033},
	{0x2039, 0x203A},
	{0x203C, 0x203C},
	{0x203E, 0x203E},
	{0x2044, 0x2044},
	{0x2070, 0x2070},
	{0x2074, 0x208E},
	{0x2099, 0x2099},
	{0x20A3, 0x20A4},
	{0x20A7, 0x20A7},
	{0x20AC, 0x20AC},
	{0x2105, 0x2105},
	{0x2113, 0x2113},
	{0x2116, 0x2116},
	{0x2122, 0x2122},
	{0x2126, 0x2126},
	{0x212E, 0x212E},
	{0x215B, 0x215E},
	{0x2190, 0x2195},
	{0x21A8, 0x21A8},
	{0x2202, 0x2202},
	{0x2206, 0x2206},
	{0x220F, 0x220F},
	{0x2211, 0x2212},
	{0x2215, 0x2215},
	{0x2219, 0x221A},
	{0x221E, 0x221F},
	{0x2229, 0x222B},
	{0x2248, 0x2248},
	{0x2260, 0x2261},
	{0x2264, 0x2265},
	{0x2302, 0x2302},
	{0x2310, 0x2310},
	{0x2320, 0x2321},
	{0x2500, 0x2500},
	{0x2502, 0x2502},
	{0x250C, 0x250C},
	{0x2510, 0x2510},
	{0x2514, 0x2514},
	{0x2518, 0x2518},
	{0x251C, 0x251C},
	{0x2524, 0x2524},
	{0x252C, 0x252C},
	{0x2534, 0x2534},
	{0x253C, 0x253C},
	{0x2550, 0x256C},
	{0x2580, 0x2580},
	{0x2584, 0x2584},
	{0x2588, 0x2588},
	{0x258C, 0x258C},
	{0x2590, 0x2593},
	{0x25A0, 0x25A1},
	{0x25AA, 0x25AC},
	{0x25B2, 0x25B2},
	{0x25BA, 0x25BA},
	{0x25BC, 0x25BC},
	{0x25C4, 0x25C4},
	{0x25CA, 0x25CB},
	{0x25CF, 0x25CF},
	{0x25D8, 0x25D9},
	{0x25E6, 0x25E6},
	{0x263A, 0x263C},
	{0x2640, 0x2640},
	{0x2642, 0x2642},
	{0x2660, 0x2660},
	{0x2663, 0x2663},
	{0x2665, 0x2666},
	{0x266A, 0x266B},
	{0xF800, 0xF800},
	{0xFB01, 0xFB02},
}

func inPrintableRanges(r rune) bool {
	for _, rg := range printableRanges {
		if r < rg[0] {
			return false
		}
		if r <= rg[1] {
			return true
		}
	}

	return false
}

// RuneProblem names why r cannot be printed, or "" when it can. multiline
// allows a line feed, a carriage return and a tab, as a free text field
// does. The rules are the renderer's: no control character, no
// bidirectional formatting control (it can reorder what a reader sees,
// for example the digits of an IBAN), no U+FFFD (it is what invalid UTF-8
// decodes to), and a glyph in every bundled font.
func RuneProblem(r rune, multiline bool) string {
	switch {
	case multiline && (r == '\n' || r == '\r' || r == '\t'):
		return ""
	case (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069):
		return fmt.Sprintf("bidirectional control character %U is not allowed", r)
	case unicode.IsControl(r):
		return fmt.Sprintf("control character %U is not allowed", r)
	case r == utf8.RuneError:
		return "invalid UTF-8 or replacement character U+FFFD is not allowed"
	case !inPrintableRanges(r):
		return fmt.Sprintf("character %q (%U) cannot be printed with the bundled font", r, r)
	default:
		return ""
	}
}

// stringProblem returns the problem of the first rune of s that cannot
// be printed, or "".
func stringProblem(s string, multiline bool) string {
	for _, r := range s {
		if reason := RuneProblem(r, multiline); reason != "" {
			return reason
		}
	}

	return ""
}

// textLines splits a free text field the way the renderer does: a
// carriage return is dropped, a tab prints as a space, trailing line
// feeds and spaces are not printed.
func textLines(s string) []string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.TrimRight(s, "\n ")
	if s == "" {
		return nil
	}

	return strings.Split(s, "\n")
}

// LineFieldProblem checks a single line field against maxChars and the
// printable runes, "" when it can be printed.
func LineFieldProblem(s string, maxChars int) string {
	if reason := stringProblem(s, false); reason != "" {
		return reason
	}
	if utf8.RuneCountInString(s) > maxChars {
		return fmt.Sprintf("at most %d characters can be printed", maxChars)
	}

	return ""
}

// TextFieldProblem checks a free text field against maxChars, maxLines
// and the printable runes, "" when it can be printed.
func TextFieldProblem(s string, maxChars, maxLines int) string {
	if reason := stringProblem(s, true); reason != "" {
		return reason
	}
	if utf8.RuneCountInString(s) > maxChars {
		return fmt.Sprintf("at most %d characters can be printed", maxChars)
	}
	if len(textLines(s)) > maxLines {
		return fmt.Sprintf("at most %d lines can be printed", maxLines)
	}

	return ""
}

// CheckPrintable reports every field of p that template version
// TemplateDocumentV1 cannot print: a character the bundled fonts lack, a
// control character, or a field over the limits above. The keys use the
// path scheme of Validate. Issuing merges the result into the same
// details and refuses to freeze a payload with problems, so an issued
// document can never be one the renderer refuses.
//
// It does not measure words against the cells of the layout and does not
// estimate pages: that needs the font metrics, which live in the
// renderer (internal/pdf.CheckPrintable, issue #74). An imported payload
// is never rendered and is not checked.
func CheckPrintable(p FrozenPayloadV1) map[string]string {
	problems := map[string]string{}
	if p.Imported {
		return problems
	}
	add := func(path, reason string) {
		if reason == "" {
			return
		}
		if _, seen := problems[path]; !seen {
			problems[path] = reason
		}
	}
	line := func(path, s string) { add(path, LineFieldProblem(s, MaxLineChars)) }
	seller := func(path, s string) { add(path, LineFieldProblem(s, MaxSellerChars)) }
	text := func(path, s string) { add(path, TextFieldProblem(s, MaxTextChars, MaxTextLines)) }

	line("number", p.Number)
	line("issue_date", p.IssueDate)
	line("due_date", p.DueDate)
	line("valid_until", p.ValidUntil)
	if p.ServicePeriod != nil {
		line("service_period.from", p.ServicePeriod.From)
		line("service_period.to", p.ServicePeriod.To)
	}
	line("customer_reference", p.CustomerReference)
	if p.Corrects != nil {
		line("corrects.number", p.Corrects.Number)
		line("corrects.issue_date", p.Corrects.IssueDate)
	}

	s := p.Seller
	seller("seller.legal_name", s.LegalName)
	seller("seller.address.street", s.Address.Street)
	seller("seller.address.line2", s.Address.Line2)
	seller("seller.address.postal_code", s.Address.PostalCode)
	seller("seller.address.city", s.Address.City)
	seller("seller.tax_number", s.TaxNumber)
	seller("seller.vat_id", s.VATID)
	seller("seller.kleinunternehmer_id", s.KleinunternehmerID)
	seller("seller.bank_name", s.BankName)
	seller("seller.iban", s.IBAN)
	seller("seller.bic", s.BIC)
	seller("seller.email", s.Email)
	seller("seller.phone", s.Phone)
	seller("seller.website", s.Website)
	add("seller.footer_text", TextFieldProblem(s.FooterText, MaxFooterTextChars, MaxFooterTextLines))

	c := p.Customer
	line("customer.number", c.Number)
	line("customer.name", c.Name)
	line("customer.vat_id", c.VATID)
	line("customer.address.street", c.Address.Street)
	line("customer.address.line2", c.Address.Line2)
	line("customer.address.postal_code", c.Address.PostalCode)
	line("customer.address.city", c.Address.City)

	if len(p.Lines) > MaxLines {
		add("lines", fmt.Sprintf("at most %d lines can be printed", MaxLines))
	}
	for i, l := range p.Lines {
		path := fmt.Sprintf("lines[%d].", i)
		text(path+"description", l.Description)
		add(path+"unit", LineFieldProblem(l.Unit, MaxUnitChars))
	}

	text("tax_note", p.TaxNote)
	text("payment_terms", p.PaymentTerms)
	text("notes", p.Notes)

	if data, err := json.Marshal(p); err != nil || len(data) > MaxDataBytes {
		add("lines", fmt.Sprintf("the document is too large to print: at most %d bytes of data", MaxDataBytes))
	}

	return problems
}
