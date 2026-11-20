package docpayload

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/ostendorf-cloud/solobill/internal/money"
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

	v.recompute(p)

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
// by id, number and issue date, has a sequence, and carries the
// original's number plus "-" and that sequence.
//
// An imported historic correction keeps the number it was issued under,
// so the suffix rule is not checked for it, and the original's id may
// be empty when the original itself was never imported.
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
	if !p.Imported {
		v.required("corrects.id", p.Corrects.ID)
	}
	v.required("corrects.number", p.Corrects.Number)
	v.date("corrects.issue_date", p.Corrects.IssueDate)
	if p.CorrectionSeq < 1 {
		v.add("correction_seq", "must be 1 or higher for a correction")

		return
	}
	if p.Imported || p.Corrects.Number == "" {
		return
	}
	if want := p.Corrects.Number + "-" + strconv.Itoa(p.CorrectionSeq); p.Number != want {
		v.add("number", fmt.Sprintf("must be %q for correction %d of %q", want, p.CorrectionSeq, p.Corrects.Number))
	}
}

// recompute runs internal/money over the payload's own inputs and
// compares every frozen figure with the result, so a payload can never
// carry figures its lines do not produce. Only for a payload built by
// solobill: an imported document's totals come from the import and are
// checked by totals alone.
func (v validator) recompute(p FrozenPayloadV1) {
	if len(p.Lines) == 0 {
		return
	}
	regime := money.VATRegime(p.VATRegime)
	if regime != money.VATRegimeStandard && regime != money.VATRegimeKleinunternehmer {
		return
	}

	in := make([]money.Line, len(p.Lines))
	for i, l := range p.Lines {
		in[i] = money.Line{
			QuantityThousandths: money.Thousandths(l.QuantityThousandths),
			UnitPriceCents:      money.Cents(l.UnitPriceCents),
			VATRatePermyriad:    money.Permyriad(l.VATRatePermyriad),
			Discount:            moneyDiscount(l.LineDiscount),
		}
	}
	res, err := money.Calculate(in, moneyDiscount(p.DocumentDiscount), regime)
	if err != nil {
		v.add("lines", "the lines cannot be calculated: "+err.Error())

		return
	}

	for i, r := range res.Lines {
		l := p.Lines[i]
		path := fmt.Sprintf("lines[%d].", i)
		v.equal(path+"net_after_line_discount_cents", l.NetAfterLineDiscountCents, int64(r.NetAfterLineDiscountCents))
		v.equal(path+"document_discount_share_cents", l.DocumentDiscountShareCents, int64(r.DocumentDiscountShareCents))
		v.equal(path+"net_after_all_discounts_cents", l.NetAfterAllDiscountsCents, int64(r.NetAfterAllDiscountsCents))
	}

	want := TotalsFromMoney(res)
	got := p.Totals
	v.equal("totals.net_total_cents", got.NetTotalCents, want.NetTotalCents)
	v.equal("totals.document_discount_cents", got.DocumentDiscountCents, want.DocumentDiscountCents)
	v.equal("totals.net_after_document_discount_cents", got.NetAfterDocumentDiscountCents, want.NetAfterDocumentDiscountCents)
	v.equal("totals.vat_total_cents", got.VATTotalCents, want.VATTotalCents)
	v.equal("totals.grand_total_cents", got.GrandTotalCents, want.GrandTotalCents)
	if len(got.VAT) != len(want.VAT) {
		v.add("totals.vat", fmt.Sprintf("must have %d buckets, one per rate", len(want.VAT)))

		return
	}
	for i := range want.VAT {
		path := fmt.Sprintf("totals.vat[%d].", i)
		v.equal(path+"rate_permyriad", int64(got.VAT[i].RatePermyriad), int64(want.VAT[i].RatePermyriad))
		v.equal(path+"net_cents", got.VAT[i].NetCents, want.VAT[i].NetCents)
		v.equal(path+"vat_cents", got.VAT[i].VATCents, want.VAT[i].VATCents)
	}
}

func (v validator) equal(path string, got, want int64) {
	if got != want {
		v.add(path, fmt.Sprintf("must be %d as internal/money computes it", want))
	}
}

func moneyDiscount(d Discount) money.Discount {
	var out money.Discount
	if d.PercentPermyriad != nil {
		p := money.Permyriad(*d.PercentPermyriad)
		out.PercentPermyriad = &p
	}
	if d.AmountCents != nil {
		a := money.Cents(*d.AmountCents)
		out.AmountCents = &a
	}

	return out
}

// totals checks the document level arithmetic of an imported document:
// net minus discount, the VAT buckets, each bucket's VAT rounded half
// away from zero once, and the grand total. Under the Kleinunternehmer
// regime the VAT total is 0.
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
