package docpayload

import (
	"fmt"
	"sort"
	"strconv"
)

// Validate reports every field the payload lacks or gets wrong for its
// type and regime, as a map from a field path to a reason. An empty map
// means the payload is complete and its totals add up. The issue
// endpoint (issue #70) refuses to freeze a payload with problems and
// answers the map as the error's details, unchanged.
//
// Field paths use the JSON names joined by ".", with "[i]" for an array
// index: "seller.address.city", "lines[1].description". The seller
// validation of issue #68 uses the same scheme under "seller.".
//
// The rules are section 14 Abs. 4 Satz 1 Nr. 1 to 8 UStG for an invoice
// and a correction under the standard regime, and section 34a UStDV
// under the Kleinunternehmer regime. An offer is no invoice: it needs
// the parties, a number, an issue date, lines and totals. An imported
// document needs only what the import can know: parties, number, issue
// date, totals and the original.
func Validate(p FrozenPayloadV1) map[string]string {
	v := validator{problems: map[string]string{}}

	if p.Version != Version1 {
		v.add("version", fmt.Sprintf("must be %d", Version1))
	}
	if p.Currency != CurrencyEUR {
		v.add("currency", "must be "+CurrencyEUR)
	}
	v.oneOf("type", p.Type, TypeInvoice, TypeCorrection, TypeOffer)
	v.oneOf("language", p.Language, LanguageGerman, LanguageEnglish)
	v.oneOf("vat_regime", p.VATRegime, RegimeStandard, RegimeKleinunternehmer)

	// Nr. 1: names and addresses of both parties.
	v.required("seller.legal_name", p.Seller.LegalName)
	v.address("seller.address", p.Seller.Address)
	v.required("customer.name", p.Customer.Name)
	v.address("customer.address", p.Customer.Address)

	// Nr. 3 and Nr. 4.
	v.date("issue_date", p.IssueDate)
	v.required("number", p.Number)
	if p.NumberValue <= 0 {
		v.add("number_value", "must be positive")
	}
	if p.NumberYear <= 0 {
		v.add("number_year", "must be positive")
	}
	if !p.Imported {
		v.required("template_version", p.TemplateVersion)
	}

	isInvoice := p.Type == TypeInvoice || p.Type == TypeCorrection

	if isInvoice {
		v.taxIdentity(p)
	}
	v.correction(p)
	if p.Type == TypeOffer {
		if p.ServicePeriod != nil {
			v.add("service_period", "must be empty for an offer")
		}
		v.optionalDate("valid_until", p.ValidUntil)
	} else if p.ValidUntil != "" {
		v.add("valid_until", "only an offer has a validity date")
	}
	v.optionalDate("due_date", p.DueDate)

	if p.Imported {
		if p.Original == nil || p.Original.SHA256 == "" || p.Original.FileID == "" {
			v.add("original", "required for an imported document")
		}
		v.totals(p)

		return v.problems
	}
	if p.Original != nil {
		v.add("original", "only an imported document has an original")
	}

	// Nr. 6: the Leistungszeitpunkt, required for an invoice under the
	// standard regime. Section 34a UStDV does not list it, so a
	// Kleinunternehmer invoice may omit it.
	if isInvoice && p.VATRegime == RegimeStandard && p.ServicePeriod == nil {
		v.add("service_period", "required for an invoice")
	}
	if p.ServicePeriod != nil {
		v.date("service_period.from", p.ServicePeriod.From)
		v.date("service_period.to", p.ServicePeriod.To)
		if p.ServicePeriod.From > p.ServicePeriod.To {
			v.add("service_period", "from must not be after to")
		}
	}

	// Nr. 5: quantity and kind of every line.
	if len(p.Lines) == 0 {
		v.add("lines", "at least one line is required")
	}
	for i, l := range p.Lines {
		path := fmt.Sprintf("lines[%d]", i)
		v.required(path+".description", l.Description)
		v.required(path+".unit", l.Unit)
		if l.QuantityThousandths <= 0 {
			v.add(path+".quantity_thousandths", "must be positive")
		}
		if p.VATRegime == RegimeKleinunternehmer && l.VATRatePermyriad != 0 {
			v.add(path+".vat_rate_permyriad", "must be 0 under the Kleinunternehmer regime")
		}
	}

	// Nr. 7 and Nr. 8: one VAT bucket per rate, or the exemption note.
	if p.VATRegime == RegimeKleinunternehmer {
		v.required("tax_note", p.TaxNote)
	}
	if p.VATRegime == RegimeStandard && len(p.Lines) > 0 && len(p.Totals.VAT) == 0 {
		v.add("totals.vat", "one bucket per VAT rate is required")
	}
	if p.VATRegime == RegimeStandard {
		for _, b := range p.Totals.VAT {
			if b.RatePermyriad == 0 && p.TaxNote == "" {
				v.add("tax_note", "required when a line is tax exempt")
			}
		}
	}

	v.lineSums(p)
	v.totals(p)

	return v.problems
}

// SortedPaths returns the keys of a Validate result in a stable order,
// for messages and tests.
func SortedPaths(problems map[string]string) []string {
	paths := make([]string, 0, len(problems))
	for k := range problems {
		paths = append(paths, k)
	}
	sort.Strings(paths)

	return paths
}

type validator struct {
	problems map[string]string
}

func (v validator) add(path, reason string) {
	if _, ok := v.problems[path]; !ok {
		v.problems[path] = reason
	}
}

func (v validator) required(path, value string) {
	if value == "" {
		v.add(path, "required")
	}
}

func (v validator) oneOf(path, value string, allowed ...string) {
	for _, a := range allowed {
		if value == a {
			return
		}
	}
	v.add(path, fmt.Sprintf("must be one of %v", allowed))
}

func (v validator) date(path, value string) {
	if !validDate(value) {
		v.add(path, "must be a date in the form YYYY-MM-DD")
	}
}

func (v validator) optionalDate(path, value string) {
	if value != "" {
		v.date(path, value)
	}
}

func (v validator) address(path string, a Address) {
	v.required(path+".street", a.Street)
	v.required(path+".postal_code", a.PostalCode)
	v.required(path+".city", a.City)
	v.required(path+".country_code", a.CountryCode)
}

// taxIdentity checks Nr. 2: the Steuernummer or the USt-IdNr, and for a
// Kleinunternehmer also the Kleinunternehmer-Identifikationsnummer.
func (v validator) taxIdentity(p FrozenPayloadV1) {
	s := p.Seller
	if s.TaxNumber != "" || s.VATID != "" {
		return
	}
	if p.VATRegime == RegimeKleinunternehmer && s.KleinunternehmerID != "" {
		return
	}
	v.add("seller.tax_number", "a Steuernummer or USt-IdNr is required")
}

// correction checks the replace model: a correction names its original
// and carries the original's number plus "-" and its sequence.
func (v validator) correction(p FrozenPayloadV1) {
	if p.Type != TypeCorrection {
		if p.Corrects != nil {
			v.add("corrects", "only a correction references an original")
		}
		if p.CorrectionSeq != 0 {
			v.add("correction_seq", "only a correction has a sequence")
		}

		return
	}

	if p.Corrects == nil {
		v.add("corrects", "required for a correction")

		return
	}
	if p.CorrectionSeq < 1 {
		v.add("correction_seq", "must be 1 or higher for a correction")

		return
	}
	if want := p.Corrects.Number + "-" + strconv.Itoa(p.CorrectionSeq); p.Number != want {
		v.add("number", fmt.Sprintf("must be %q for correction %d of %q", want, p.CorrectionSeq, p.Corrects.Number))
	}
}

// lineSums checks each line's own figures and that the lines add up to
// the net total.
func (v validator) lineSums(p FrozenPayloadV1) {
	var netTotal int64
	for i, l := range p.Lines {
		if l.NetAfterAllDiscountsCents != l.NetAfterLineDiscountCents-l.DocumentDiscountShareCents {
			v.add(fmt.Sprintf("lines[%d].net_after_all_discounts_cents", i),
				"must be net_after_line_discount_cents minus document_discount_share_cents")
		}
		netTotal += l.NetAfterLineDiscountCents
	}
	if len(p.Lines) > 0 && netTotal != p.Totals.NetTotalCents {
		v.add("totals.net_total_cents", fmt.Sprintf("must be the sum of the lines, %d", netTotal))
	}
}

// totals checks the document level arithmetic: net minus discount, the
// VAT buckets, each bucket's VAT rounded half away from zero once, and
// the grand total. Under the Kleinunternehmer regime the VAT total is 0.
func (v validator) totals(p FrozenPayloadV1) {
	t := p.Totals
	if t.NetAfterDocumentDiscountCents != t.NetTotalCents-t.DocumentDiscountCents {
		v.add("totals.net_after_document_discount_cents", "must be net_total_cents minus document_discount_cents")
	}

	var bucketNet, bucketVAT int64
	for i, b := range t.VAT {
		bucketNet += b.NetCents
		bucketVAT += b.VATCents
		if want := roundHalfAwayFromZero(b.NetCents*int64(b.RatePermyriad), 10000); b.VATCents != want {
			v.add(fmt.Sprintf("totals.vat[%d].vat_cents", i), fmt.Sprintf("must be %d", want))
		}
	}
	if len(t.VAT) > 0 && bucketNet != t.NetAfterDocumentDiscountCents {
		v.add("totals.vat", "the bucket nets must add up to net_after_document_discount_cents")
	}
	if bucketVAT != t.VATTotalCents {
		v.add("totals.vat_total_cents", "must be the sum of the bucket VAT")
	}
	if p.VATRegime == RegimeKleinunternehmer && t.VATTotalCents != 0 {
		v.add("totals.vat_total_cents", "must be 0 under the Kleinunternehmer regime")
	}
	if t.GrandTotalCents != t.NetAfterDocumentDiscountCents+t.VATTotalCents {
		v.add("totals.grand_total_cents", "must be net_after_document_discount_cents plus vat_total_cents")
	}
}

// roundHalfAwayFromZero divides n by d (d > 0) and rounds half away from
// zero, the commercial rounding internal/money uses.
func roundHalfAwayFromZero(n, d int64) int64 {
	q, r := n/d, n%d
	if r < 0 {
		r = -r
	}
	if 2*r >= d {
		if n < 0 {
			q--
		} else {
			q++
		}
	}

	return q
}
