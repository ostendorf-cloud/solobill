// Package docissue issues a document: the legal core of issue #70 (I4).
// It turns a draft, the seller profile and the customer into a
// docpayload.FrozenPayloadV1, checks it, allocates the number and freezes
// it, all inside the one tenant scoped transaction the caller opened. See
// docs/concepts/solobill.md, "Corrections", "Number format", "VAT regime
// per period", "Seller data validation" and "Frozen payload".
//
// The payload half (this file) is pure: no database, no clock, no I/O.
// Issuer (issue.go) runs it against the repositories.
package docissue

import (
	"fmt"
	"time"
	_ "time/tzdata" // the issue date is a German calendar date, also where the host has no zone database

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/money"
	"github.com/ostendorf-cloud/solobill/internal/sellerprofile"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// berlin is the zone the issue date is taken in. The invoices are
// German: an invoice issued at 00:30 on 1 January in Germany is dated and
// numbered in the new year, although it is still the old year in UTC.
var berlin = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		// Unreachable: time/tzdata embeds the zone database.
		panic(fmt.Sprintf("docissue: load Europe/Berlin: %v", err))
	}

	return loc
}()

// IssueDate is the calendar date of now in Germany, as midnight UTC, the
// form every date of a document is stored in.
func IssueDate(now time.Time) time.Time {
	y, m, d := now.In(berlin).Date()

	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// IssuedAt is now in the German zone, so its Year is the year of
// IssueDate.
func IssuedAt(now time.Time) time.Time {
	return now.In(berlin)
}

// Tax notes frozen into the payload as printed text, per regime and
// language. A Kleinunternehmer must state the exemption of section 19
// UStG (section 34a UStDV). The wording matches the renderer's fixtures
// (issue #74); its legal sufficiency is the owner's to confirm.
const (
	TaxNoteKleinunternehmerDE = "Gemäß § 19 UStG wird keine Umsatzsteuer berechnet."
	TaxNoteKleinunternehmerEN = "No VAT is charged under section 19 of the German VAT Act (Kleinunternehmerregelung)."
)

// TaxNote is the printed tax note of a document under regime in lang.
// The standard regime has none: a tax exempt line under the standard
// regime needs the exemption's own reason, which no draft field carries
// in this milestone, so docpayload.Validate refuses such a payload
// (tax_note required) instead of printing a guessed reason.
func TaxNote(regime storage.VATRegime, lang storage.DocumentLanguage) string {
	if regime != storage.VATRegimeKleinunternehmer {
		return ""
	}
	if lang == storage.DocumentLanguageEnglish {
		return TaxNoteKleinunternehmerEN
	}

	return TaxNoteKleinunternehmerDE
}

// MoneyLines converts document lines to internal/money's input.
func MoneyLines(lines []storage.DocumentLine) []money.Line {
	out := make([]money.Line, len(lines))
	for i, l := range lines {
		out[i] = money.Line{
			QuantityThousandths: money.Thousandths(l.QuantityThousandths),
			UnitPriceCents:      money.Cents(l.UnitPriceCents),
			VATRatePermyriad:    money.Permyriad(l.VATRatePermyriad),
			Discount:            moneyDiscount(l.LineDiscountPercentPermyriad, l.LineDiscountAmountCents),
		}
	}

	return out
}

func moneyDiscount(percent *int32, amount *int64) money.Discount {
	var d money.Discount
	if percent != nil {
		p := money.Permyriad(*percent)
		d.PercentPermyriad = &p
	}
	if amount != nil {
		a := money.Cents(*amount)
		d.AmountCents = &a
	}

	return d
}

// Calculate runs internal/money over a document's lines and discount
// under its regime. The API never computes money anywhere else.
func Calculate(doc storage.Document, lines []storage.DocumentLine) (money.Result, error) {
	res, err := money.Calculate(MoneyLines(lines),
		moneyDiscount(doc.DiscountPercentPermyriad, doc.DiscountAmountCents),
		money.VATRegime(doc.VATRegime))
	if err != nil {
		return money.Result{}, fmt.Errorf("docissue: calculate: %w", err)
	}

	return res, nil
}

// PayloadInput is everything a frozen payload is built from.
type PayloadInput struct {
	// Document is the draft, with its Lines.
	Document storage.Document
	Seller   storage.SellerProfile
	// LogoSHA256 is the hash of the logo file printed, empty without one.
	LogoSHA256 string
	Customer   storage.Customer
	IssueDate  time.Time
	// Number is the display number, NumberValue and NumberYear its counter
	// key. A correction carries its original's NumberValue and NumberYear.
	Number      string
	NumberValue int64
	NumberYear  int
	// CorrectionSeq and Corrects are set for a correction only.
	CorrectionSeq int
	Corrects      *docpayload.DocumentReference
}

func date(t *time.Time) string {
	if t == nil {
		return ""
	}

	return t.UTC().Format(docpayload.DateLayout)
}

func text(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

// BuildPayload builds the frozen payload of in. It copies, it never
// decides: every figure comes from internal/money, every text from the
// draft, the profile and the customer as they are. When internal/money
// refuses the lines the figures stay 0, and docpayload.Validate reports
// the lines.
func BuildPayload(in PayloadInput) docpayload.FrozenPayloadV1 {
	doc := in.Document
	lang := doc.Language
	if lang == "" {
		lang = storage.DocumentLanguageGerman
	}

	p := docpayload.FrozenPayloadV1{
		Version:           docpayload.Version1,
		Type:              string(doc.Type),
		Language:          string(lang),
		VATRegime:         string(doc.VATRegime),
		TemplateVersion:   docpayload.TemplateCurrent,
		Currency:          docpayload.CurrencyEUR,
		Number:            in.Number,
		NumberValue:       in.NumberValue,
		NumberYear:        in.NumberYear,
		CorrectionSeq:     in.CorrectionSeq,
		IssueDate:         in.IssueDate.Format(docpayload.DateLayout),
		DueDate:           date(doc.DueDate),
		ValidUntil:        date(doc.ValidUntil),
		CustomerReference: text(doc.CustomerReference),
		Corrects:          in.Corrects,
		DocumentDiscount: docpayload.Discount{
			PercentPermyriad: doc.DiscountPercentPermyriad,
			AmountCents:      doc.DiscountAmountCents,
		},
		TaxNote:      TaxNote(doc.VATRegime, lang),
		PaymentTerms: text(doc.PaymentTerms),
		Notes:        text(doc.Notes),
	}
	if doc.ServiceDate != nil {
		to := doc.ServiceDate
		if doc.ServiceDateTo != nil {
			to = doc.ServiceDateTo
		}
		p.ServicePeriod = &docpayload.ServicePeriod{From: date(doc.ServiceDate), To: date(to)}
	}

	s := in.Seller
	p.Seller = docpayload.SellerSnapshot{
		LegalName: s.LegalName,
		Address: docpayload.Address{
			Street:      s.Street,
			Line2:       text(s.AddressLine2),
			PostalCode:  s.PostalCode,
			City:        s.City,
			CountryCode: s.CountryCode,
		},
		TaxNumber:          text(s.TaxNumber),
		VATID:              text(s.VATID),
		KleinunternehmerID: text(s.KleinunternehmerID),
		BankName:           text(s.BankName),
		IBAN:               text(s.IBAN),
		BIC:                text(s.BIC),
		Email:              text(s.Email),
		Phone:              text(s.Phone),
		Website:            text(s.Website),
		FooterText:         text(s.FooterText),
		LogoSHA256:         in.LogoSHA256,
	}

	c := in.Customer
	p.Customer = docpayload.CustomerSnapshot{
		ID:     c.ID.String(),
		Number: text(c.Number),
		Name:   c.Name,
		Address: docpayload.Address{
			Street:      text(c.Street),
			Line2:       text(c.AddressLine2),
			PostalCode:  text(c.PostalCode),
			City:        text(c.City),
			CountryCode: text(c.CountryCode),
		},
		VATID: text(c.VATID),
	}

	p.Lines = make([]docpayload.Line, len(doc.Lines))
	for i, l := range doc.Lines {
		p.Lines[i] = docpayload.Line{
			Position:            l.Position + 1,
			Description:         l.Description,
			QuantityThousandths: l.QuantityThousandths,
			Unit:                l.Unit,
			UnitPriceCents:      l.UnitPriceCents,
			VATRatePermyriad:    l.VATRatePermyriad,
			LineDiscount: docpayload.Discount{
				PercentPermyriad: l.LineDiscountPercentPermyriad,
				AmountCents:      l.LineDiscountAmountCents,
			},
		}
	}
	if res, err := Calculate(doc, doc.Lines); err == nil {
		// The counts always agree: one result per line.
		_ = docpayload.ApplyLineResults(p.Lines, res.Lines)
		p.Totals = docpayload.TotalsFromMoney(res)
	}

	return p
}

// Check reports every problem of a payload that is about to be frozen:
// docpayload.Validate (section 14 UStG, section 34a UStDV, the figures),
// docpayload.CheckPrintable (the renderer's characters and limits) and
// the two country codes against the assigned ISO 3166-1 alpha-2 codes.
// An empty map means the payload can be frozen.
func Check(p docpayload.FrozenPayloadV1) map[string]string {
	problems := docpayload.Validate(p)
	for path, reason := range docpayload.CheckPrintable(p) {
		if _, seen := problems[path]; !seen {
			problems[path] = reason
		}
	}
	country := func(path, code string) {
		if code == "" || sellerprofile.ValidCountryCode(code) {
			return
		}
		if _, seen := problems[path]; !seen {
			problems[path] = "must be an assigned ISO 3166-1 alpha-2 code such as DE"
		}
	}
	country("customer.address.country_code", p.Customer.Address.CountryCode)
	country("seller.address.country_code", p.Seller.Address.CountryCode)

	return problems
}
