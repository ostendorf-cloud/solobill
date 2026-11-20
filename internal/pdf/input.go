package pdf

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
)

// Input is the data the template prints, written as data.json next to
// the template and read there with Typst's json(). Every payload string
// reaches Typst as a JSON string value and is placed as text, never as
// markup: a "#", "$", "[" or backslash in a customer name prints as is
// and never executes. Every figure is already formatted here, the
// template only lays it out.
type Input struct {
	TemplateVersion string `json:"template_version"`
	Lang            string `json:"lang"`
	Draft           bool   `json:"draft"`
	Watermark       string `json:"watermark"`
	DocumentTitle   string `json:"document_title"`
	Title           string `json:"title"`

	SenderLine     string   `json:"sender_line"`
	SellerName     string   `json:"seller_name"`
	RecipientLines []string `json:"recipient_lines"`
	Meta           []Pair   `json:"meta"`
	CorrectsLine   string   `json:"corrects_line"`

	ShowVAT bool          `json:"show_vat"`
	Columns ColumnHeaders `json:"columns"`
	Lines   []LineRow     `json:"lines"`
	Totals  []TotalRow    `json:"totals"`

	Sections []Section `json:"sections"`

	Footer      [][]string `json:"footer"`
	FooterText  []string   `json:"footer_text"`
	PageLabel   string     `json:"page_label"`
	PageOfLabel string     `json:"page_of_label"`
}

// Pair is one label and value in the meta block.
type Pair struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ColumnHeaders are the item table's column labels.
type ColumnHeaders struct {
	Position    string `json:"position"`
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	Unit        string `json:"unit"`
	UnitPrice   string `json:"unit_price"`
	VATRate     string `json:"vat_rate"`
	Amount      string `json:"amount"`
}

// LineRow is one formatted item line.
type LineRow struct {
	Position    string   `json:"position"`
	Description []string `json:"description"`
	Discount    string   `json:"discount"`
	Quantity    string   `json:"quantity"`
	Unit        string   `json:"unit"`
	UnitPrice   string   `json:"unit_price"`
	VATRate     string   `json:"vat_rate"`
	Amount      string   `json:"amount"`
}

// TotalRow is one row of the totals block. Strong rows print bold.
type TotalRow struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Strong bool   `json:"strong"`
}

// Section is a titled block of text below the totals. An empty title
// prints the lines alone.
type Section struct {
	Title string   `json:"title"`
	Lines []string `json:"lines"`
}

// Size limits checked before Typst runs, so a payload can never make
// Typst write an unbounded file. Measured with typst 0.14.2: MaxLines
// lines with a long description each, at MaxDataBytes, render to about
// 4.4 MB, below DefaultMaxOutputBytes (see TestCapsKeepOutputSmall).
const (
	// MaxLines is the most item lines a document can print.
	MaxLines = 1000
	// MaxDataBytes bounds data.json. The API limits a JSON body to 1 MiB
	// (docs/api.md), and the formatted data of such a body stays below
	// this.
	MaxDataBytes = 2 << 20
)

// Errors of BuildInput and Render.
var (
	// ErrUnsupported is a payload type, language, regime or currency this
	// template version cannot print.
	ErrUnsupported = errors.New("pdf: unsupported payload")
	// ErrImported is an imported document: its original PDF is the
	// document, solobill never renders one.
	ErrImported = errors.New("pdf: an imported document has its original pdf")
	// ErrLogoUnsupported is a payload whose seller carries a logo. The
	// renderer cannot print a logo yet (issue #74, part 2), and a stored
	// PDF must never silently lack it.
	ErrLogoUnsupported = errors.New("pdf: the renderer cannot print a logo yet")
	// ErrPayloadTooLarge is a payload whose template data exceeds
	// MaxDataBytes.
	ErrPayloadTooLarge = errors.New("pdf: payload too large to render")
)

// InvalidPayloadError lists what a payload lacks or holds that the
// template cannot print, in the field path scheme of
// docpayload.Validate. The API answers it as 400 with these details.
type InvalidPayloadError struct {
	Details map[string]string
}

func (e *InvalidPayloadError) Error() string {
	return "pdf: payload cannot be printed: " + strings.Join(docpayload.SortedPaths(e.Details), ", ")
}

// draftExempt are the Validate paths a draft may fail: it has no number
// yet and is printed with the current template.
var draftExempt = map[string]bool{
	"number":           true,
	"number_value":     true,
	"number_year":      true,
	"template_version": true,
}

// CheckPrintable reports whether the template can print every string
// of p with the bundled fonts, within MaxLines and MaxDataBytes. It
// returns *InvalidPayloadError naming each field that cannot be
// printed, ErrPayloadTooLarge, or ErrUnsupported for an unknown
// language. The issuing code (#70) calls it before it freezes a
// payload, so an issued document can never become unrenderable. It
// ignores the logo: that is a renderer gap, not a payload problem.
func CheckPrintable(p docpayload.FrozenPayloadV1) error {
	if p.Imported {
		return nil
	}
	l, ok := labelsByLanguage[p.Language]
	if !ok {
		return fmt.Errorf("%w: language %q", ErrUnsupported, p.Language)
	}
	pr, err := newPrinter()
	if err != nil {
		return err
	}
	in := build(p, l, false, p.TemplateVersion, pr)
	capLines(p, pr.problems)
	if len(pr.problems) > 0 {
		return &InvalidPayloadError{Details: pr.problems}
	}
	_, err = MarshalInput(in)

	return err
}

// BuildInput turns a payload into the template's data. It formats and
// never computes. draft prints the watermark and no number.
func BuildInput(p docpayload.FrozenPayloadV1, draft bool) (Input, error) {
	if p.Imported {
		return Input{}, ErrImported
	}
	if p.Version != docpayload.Version1 {
		return Input{}, fmt.Errorf("%w: version %d", ErrUnsupported, p.Version)
	}
	l, ok := labelsByLanguage[p.Language]
	if !ok {
		return Input{}, fmt.Errorf("%w: language %q", ErrUnsupported, p.Language)
	}
	if _, ok := l.Title[p.Type]; !ok {
		return Input{}, fmt.Errorf("%w: type %q", ErrUnsupported, p.Type)
	}
	if p.VATRegime != docpayload.RegimeStandard && p.VATRegime != docpayload.RegimeKleinunternehmer {
		return Input{}, fmt.Errorf("%w: vat regime %q", ErrUnsupported, p.VATRegime)
	}
	if p.Currency != docpayload.CurrencyEUR {
		return Input{}, fmt.Errorf("%w: currency %q", ErrUnsupported, p.Currency)
	}
	if p.Seller.LogoSHA256 != "" {
		return Input{}, ErrLogoUnsupported
	}
	version, err := templateVersion(p, draft)
	if err != nil {
		return Input{}, err
	}

	pr, err := newPrinter()
	if err != nil {
		return Input{}, err
	}
	if draft {
		for path, reason := range docpayload.Validate(p) {
			if !draftExempt[path] {
				pr.problems[path] = reason
			}
		}
	}
	in := build(p, l, draft, version, pr)
	capLines(p, pr.problems)
	if len(pr.problems) > 0 {
		return Input{}, &InvalidPayloadError{Details: pr.problems}
	}

	return in, nil
}

func capLines(p docpayload.FrozenPayloadV1, problems map[string]string) {
	if len(p.Lines) > MaxLines {
		problems["lines"] = fmt.Sprintf("at most %d lines can be printed", MaxLines)
	}
}

// build formats p. Every payload string passes through pr, which records
// what cannot be printed under the payload's field path. Nothing here
// returns early, so every field is checked.
func build(p docpayload.FrozenPayloadV1, l labels, draft bool, version string, pr *printer) Input {
	lang := p.Language
	money := func(c int64) string { return formatCents(c, p.Currency, lang) }
	date := func(path, s string) string { return formatDate(pr.line(path, s), lang) }
	isOffer := p.Type == docpayload.TypeOffer
	showVAT := p.VATRegime == docpayload.RegimeStandard
	title := l.Title[p.Type]

	seller := checkedSeller(p.Seller, pr)
	customer := checkedCustomer(p.Customer, pr)
	number := pr.line("number", p.Number)

	in := Input{
		TemplateVersion: version,
		Lang:            lang,
		Draft:           draft,
		Watermark:       l.Watermark,
		Title:           title,
		SenderLine:      senderLine(seller),
		SellerName:      seller.LegalName,
		RecipientLines:  recipientLines(customer, seller.Address.CountryCode, l),
		ShowVAT:         showVAT,
		Columns: ColumnHeaders{
			Position:    l.Position,
			Description: l.Description,
			Quantity:    l.Quantity,
			Unit:        l.Unit,
			UnitPrice:   l.UnitPrice,
			VATRate:     l.VATRate,
			Amount:      l.Amount,
		},
		Lines:       make([]LineRow, 0, len(p.Lines)),
		Meta:        []Pair{},
		Sections:    []Section{},
		PageLabel:   l.Page,
		PageOfLabel: l.PageOf,
		Footer:      footerColumns(seller, l),
		FooterText:  pr.text("seller.footer_text", p.Seller.FooterText),
	}
	if draft {
		in.DocumentTitle = title + " (" + l.Watermark + ")"
	} else {
		in.DocumentTitle = title + " " + number
	}

	// Meta block: section 14 Abs. 4 Nr. 3, 4 and 6, plus the references.
	if !draft {
		in.Meta = append(in.Meta, Pair{l.Number[p.Type], number})
	}
	if p.IssueDate != "" {
		in.Meta = append(in.Meta, Pair{l.Date, date("issue_date", p.IssueDate)})
	}
	if sp := p.ServicePeriod; sp != nil {
		from := date("service_period.from", sp.From)
		to := date("service_period.to", sp.To)
		if sp.From == sp.To {
			in.Meta = append(in.Meta, Pair{l.ServiceDate, from})
		} else {
			in.Meta = append(in.Meta, Pair{l.ServicePeriod, from + " " + l.PeriodTo + " " + to})
		}
	}
	if customer.Number != "" {
		in.Meta = append(in.Meta, Pair{l.CustomerNumber, customer.Number})
	}
	if p.CustomerReference != "" {
		in.Meta = append(in.Meta, Pair{l.CustomerReference, pr.line("customer_reference", p.CustomerReference)})
	}
	if customer.VATID != "" {
		in.Meta = append(in.Meta, Pair{l.CustomerVATID, customer.VATID})
	}
	if isOffer && p.ValidUntil != "" {
		in.Meta = append(in.Meta, Pair{l.ValidUntil, date("valid_until", p.ValidUntil)})
	}
	if !isOffer && p.DueDate != "" {
		in.Meta = append(in.Meta, Pair{l.DueDate, date("due_date", p.DueDate)})
	}
	if c := p.Corrects; c != nil {
		in.CorrectsLine = l.Corrects + " " + pr.line("corrects.number", c.Number) + " " + l.CorrectsOf + " " +
			date("corrects.issue_date", c.IssueDate) + "."
	}

	// Nr. 5 and Nr. 7: quantity, kind and the reductions per line.
	for i, line := range p.Lines {
		path := fmt.Sprintf("lines[%d].", i)
		row := LineRow{
			Position:    fmt.Sprint(line.Position),
			Description: pr.text(path+"description", line.Description),
			Quantity:    formatQuantity(line.QuantityThousandths, lang),
			Unit:        pr.unit(path+"unit", line.Unit),
			UnitPrice:   money(line.UnitPriceCents),
			Amount:      money(line.NetAfterLineDiscountCents),
		}
		if showVAT {
			row.VATRate = formatPercent(int64(line.VATRatePermyriad), lang)
		}
		if d := discountText(line.LineDiscount, p.Currency, lang); d != "" {
			row.Discount = l.Discount + " " + d
		}
		in.Lines = append(in.Lines, row)
	}

	in.Totals = totalRows(p, l, isOffer, showVAT)

	// Nr. 8 note (exemption, reverse charge) or the section 19 note, as
	// frozen, then payment terms (never on an offer) and free notes.
	if lines := pr.text("tax_note", p.TaxNote); len(lines) > 0 {
		in.Sections = append(in.Sections, Section{Lines: lines})
	}
	if !isOffer {
		if lines := pr.text("payment_terms", p.PaymentTerms); len(lines) > 0 {
			in.Sections = append(in.Sections, Section{Title: l.PaymentTerms, Lines: lines})
		}
	}
	if lines := pr.text("notes", p.Notes); len(lines) > 0 {
		in.Sections = append(in.Sections, Section{Title: l.Notes, Lines: lines})
	}

	return in
}

// checkedSeller passes every printed seller string through pr.
func checkedSeller(s docpayload.SellerSnapshot, pr *printer) docpayload.SellerSnapshot {
	s.LegalName = pr.seller("seller.legal_name", s.LegalName)
	s.Address = docpayload.Address{
		Street:      pr.seller("seller.address.street", s.Address.Street),
		Line2:       pr.seller("seller.address.line2", s.Address.Line2),
		PostalCode:  pr.seller("seller.address.postal_code", s.Address.PostalCode),
		City:        pr.seller("seller.address.city", s.Address.City),
		CountryCode: pr.seller("seller.address.country_code", s.Address.CountryCode),
	}
	s.TaxNumber = pr.seller("seller.tax_number", s.TaxNumber)
	s.VATID = pr.seller("seller.vat_id", s.VATID)
	s.KleinunternehmerID = pr.seller("seller.kleinunternehmer_id", s.KleinunternehmerID)
	s.BankName = pr.seller("seller.bank_name", s.BankName)
	s.IBAN = groupIBAN(pr.seller("seller.iban", s.IBAN))
	s.BIC = pr.seller("seller.bic", s.BIC)
	s.Email = pr.seller("seller.email", s.Email)
	s.Phone = pr.seller("seller.phone", s.Phone)
	s.Website = pr.seller("seller.website", s.Website)
	pr.limit("seller.footer_text", s.FooterText, MaxFooterTextChars)

	return s
}

// checkedCustomer passes every printed customer string through pr.
func checkedCustomer(c docpayload.CustomerSnapshot, pr *printer) docpayload.CustomerSnapshot {
	c.Number = pr.line("customer.number", c.Number)
	c.Name = pr.line("customer.name", c.Name)
	c.Address = checkedAddress("customer.address", c.Address, pr)
	c.VATID = pr.line("customer.vat_id", c.VATID)

	return c
}

func checkedAddress(path string, a docpayload.Address, pr *printer) docpayload.Address {
	a.Street = pr.line(path+".street", a.Street)
	a.Line2 = pr.line(path+".line2", a.Line2)
	a.PostalCode = pr.line(path+".postal_code", a.PostalCode)
	a.City = pr.line(path+".city", a.City)
	a.CountryCode = pr.line(path+".country_code", a.CountryCode)

	return a
}

// MarshalInput encodes the template data as data.json, indented so a
// golden file diff reads line by line. It refuses data over
// MaxDataBytes.
func MarshalInput(in Input) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// The JSON is read by Typst, not by a browser: "<", ">" and "&" stay
	// readable in the golden files.
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(in); err != nil {
		return nil, fmt.Errorf("pdf: encode input: %w", err)
	}
	if buf.Len() > MaxDataBytes {
		return nil, fmt.Errorf("%w: %d bytes of template data, limit %d", ErrPayloadTooLarge, buf.Len(), MaxDataBytes)
	}

	return buf.Bytes(), nil
}

func discountText(d docpayload.Discount, currency, lang string) string {
	switch {
	case d.PercentPermyriad != nil && *d.PercentPermyriad != 0:
		return formatPercent(int64(*d.PercentPermyriad), lang)
	case d.AmountCents != nil && *d.AmountCents != 0:
		return formatCents(*d.AmountCents, currency, lang)
	default:
		return ""
	}
}

func totalRows(p docpayload.FrozenPayloadV1, l labels, isOffer, showVAT bool) []TotalRow {
	lang := p.Language
	money := func(c int64) string { return formatCents(c, p.Currency, lang) }
	t := p.Totals

	var rows []TotalRow
	hasDiscount := t.DocumentDiscountCents != 0
	if showVAT || hasDiscount {
		rows = append(rows, TotalRow{Label: l.NetTotal, Value: money(t.NetTotalCents)})
	}
	if hasDiscount {
		label := l.DocumentDiscount
		if d := p.DocumentDiscount.PercentPermyriad; d != nil && *d != 0 {
			label += " " + formatPercent(int64(*d), lang)
		}
		rows = append(rows,
			TotalRow{Label: label, Value: money(-t.DocumentDiscountCents)},
			TotalRow{Label: l.NetAfterDiscount, Value: money(t.NetAfterDocumentDiscountCents)},
		)
	}
	if showVAT {
		for _, b := range t.VAT {
			rows = append(rows, TotalRow{
				Label: l.VAT + " " + formatPercent(int64(b.RatePermyriad), lang) + " " + l.VATOn + " " + money(b.NetCents),
				Value: money(b.VATCents),
			})
		}
	}
	total := l.GrandTotal
	if isOffer {
		total = l.OfferTotal
	}
	rows = append(rows, TotalRow{Label: total, Value: money(t.GrandTotalCents), Strong: true})

	return rows
}

func cityLine(a docpayload.Address) string {
	return strings.TrimSpace(a.PostalCode + " " + a.City)
}

func senderLine(s docpayload.SellerSnapshot) string {
	parts := []string{s.LegalName, s.Address.Street, cityLine(s.Address)}

	return strings.Join(nonEmpty(parts), " · ")
}

// recipientLines is the address field. The country line is printed only
// when it differs from the seller's, in the document language.
func recipientLines(c docpayload.CustomerSnapshot, sellerCountry string, l labels) []string {
	lines := nonEmpty([]string{c.Name, c.Address.Street, c.Address.Line2, cityLine(c.Address)})
	if cc := c.Address.CountryCode; cc != "" && cc != sellerCountry {
		name, ok := l.Countries[cc]
		if !ok {
			name = cc
		}
		lines = append(lines, strings.ToUpper(name))
	}

	return lines
}

// footerColumns are the seller's data printed on every page: address,
// contact, tax identity (section 14 Abs. 4 Nr. 2) and bank.
func footerColumns(s docpayload.SellerSnapshot, l labels) [][]string {
	labelled := func(label, value string) string {
		if value == "" {
			return ""
		}

		return label + " " + value
	}
	cols := [][]string{
		nonEmpty([]string{s.LegalName, s.Address.Street, s.Address.Line2, cityLine(s.Address)}),
		nonEmpty([]string{
			labelled(l.Phone, s.Phone),
			labelled(l.Email, s.Email),
			labelled(l.Web, s.Website),
		}),
		nonEmpty([]string{
			labelled(l.TaxNumber, s.TaxNumber),
			labelled(l.VATID, s.VATID),
			labelled(l.KleinunternehmerID, s.KleinunternehmerID),
		}),
		nonEmpty([]string{
			labelled(l.Bank, s.BankName),
			labelled(l.IBAN, s.IBAN),
			labelled(l.BIC, s.BIC),
		}),
	}
	out := make([][]string, 0, len(cols))
	for _, c := range cols {
		if len(c) > 0 {
			out = append(out, c)
		}
	}

	return out
}

func nonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}

	return out
}
