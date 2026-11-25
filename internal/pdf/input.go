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
	// Logo is true when the payload names a logo. The template then
	// prints the file "logo", which only BuildSourceWithLogo writes.
	Logo          bool   `json:"logo"`
	Watermark     string `json:"watermark"`
	DocumentTitle string `json:"document_title"`
	Title         string `json:"title"`

	SenderLine     string   `json:"sender_line"`
	SellerName     string   `json:"seller_name"`
	RecipientLines []string `json:"recipient_lines"`
	// RecipientPaths name the payload field of each recipient line, for
	// the template's overflow check.
	RecipientPaths []string `json:"recipient_paths"`
	Meta           []Pair   `json:"meta"`
	CorrectsLine   string   `json:"corrects_line"`

	ShowVAT bool          `json:"show_vat"`
	Columns ColumnHeaders `json:"columns"`
	Lines   []LineRow     `json:"lines"`
	Totals  []TotalRow    `json:"totals"`

	Sections []Section `json:"sections"`

	Footer [][]string `json:"footer"`
	// FooterPaths name the payload field of each footer line.
	FooterPaths [][]string `json:"footer_paths"`
	FooterText  []string   `json:"footer_text"`
	PageLabel   string     `json:"page_label"`
	PageOfLabel string     `json:"page_of_label"`
}

// Pair is one label and value in the meta block. Path names the payload
// field the value prints.
type Pair struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Path  string `json:"path"`
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
	// Path names the payload field the row prints.
	Path string `json:"path"`
}

// Section is a titled block of text below the totals. An empty title
// prints the lines alone.
type Section struct {
	Title string   `json:"title"`
	Lines []string `json:"lines"`
	// Path names the payload field the section prints.
	Path string `json:"path"`
}

// Errors of BuildInput and Render.
var (
	// ErrUnsupported is a payload type, language, regime or currency this
	// template version cannot print.
	ErrUnsupported = errors.New("pdf: unsupported payload")
	// ErrImported is an imported document: its original PDF is the
	// document, solobill never renders one.
	ErrImported = errors.New("pdf: an imported document has its original pdf")
	// ErrLogoMismatch is logo bytes whose SHA-256 differs from the one
	// frozen in the payload, a payload naming a logo rendered without
	// one, or a logo given for a payload naming none. The API answers it
	// as 409 conflict.
	ErrLogoMismatch = errors.New("pdf: the logo does not match the payload")
	// ErrLogoFormat is a logo that is neither a PNG nor a JPEG, or too
	// large.
	ErrLogoFormat = errors.New("pdf: the logo cannot be printed")
	// ErrPayloadTooLarge is a payload whose template data exceeds
	// Limits.DataBytes.
	ErrPayloadTooLarge = errors.New("pdf: payload too large to render")
)

// InvalidPayloadError lists what a payload lacks or holds that the
// template cannot print, in the field path scheme of
// docpayload.Validate. The API answers it as 400 with these details.
type InvalidPayloadError struct {
	Details map[string]string
	// Err is the cause when Typst found the problem: ErrLayoutOverflow.
	Err error
}

func (e *InvalidPayloadError) Error() string {
	return "pdf: payload cannot be printed: " + strings.Join(docpayload.SortedPaths(e.Details), ", ")
}

// Unwrap returns the cause, if any.
func (e *InvalidPayloadError) Unwrap() error { return e.Err }

// draftExempt are the Validate paths a draft may fail: it has no number
// yet and is printed with the current template.
var draftExempt = map[string]bool{
	"number":           true,
	"number_value":     true,
	"number_year":      true,
	"template_version": true,
}

// CheckPrintable reports whether the payload's template version can
// print every string of p with the bundled fonts, within that version's
// Limits. It returns *InvalidPayloadError naming each field that cannot
// be printed, ErrPayloadTooLarge, ErrUnknownTemplate, or ErrUnsupported
// for an unknown language. The issuing code (#70) must call it before it
// freezes a payload, so an issued document can never become
// unrenderable. It checks that the template version can print a logo
// the payload names, not the logo file: issuing never reads the bucket.
func CheckPrintable(p docpayload.FrozenPayloadV1) error {
	if p.Imported {
		return nil
	}
	l, err := supported(p)
	if err != nil {
		return err
	}
	version := p.TemplateVersion
	if version == "" {
		version = CurrentTemplateVersion
	}
	lim, ok := limitsByTemplate[version]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownTemplate, version)
	}
	if err := logoSupported(p, version); err != nil {
		return err
	}
	pr, err := newPrinter(lim)
	if err != nil {
		return err
	}
	_, _, err = finish(p, l, false, version, pr)

	return err
}

// BuildInput turns a payload into the template's data. It formats and
// never computes. draft prints the watermark and no number.
func BuildInput(p docpayload.FrozenPayloadV1, draft bool) (Input, error) {
	in, _, err := buildData(p, draft)

	return in, err
}

// buildData is BuildInput plus the encoded data file.
func buildData(p docpayload.FrozenPayloadV1, draft bool) (Input, []byte, error) {
	if p.Imported {
		return Input{}, nil, ErrImported
	}
	l, err := supported(p)
	if err != nil {
		return Input{}, nil, err
	}
	version, err := templateVersion(p, draft)
	if err != nil {
		return Input{}, nil, err
	}
	if err := logoSupported(p, version); err != nil {
		return Input{}, nil, err
	}

	lim, ok := limitsByTemplate[version]
	if !ok {
		return Input{}, nil, fmt.Errorf("%w: %q has no limits", ErrUnknownTemplate, version)
	}
	pr, err := newPrinter(lim)
	if err != nil {
		return Input{}, nil, err
	}
	if draft {
		for path, reason := range docpayload.Validate(p) {
			if !draftExempt[path] {
				pr.problems[path] = reason
			}
		}
	}

	return finish(p, l, draft, version, pr)
}

// logoSupported refuses a payload that names a logo for a template
// version without a place for one: its PDF would silently lack the logo.
func logoSupported(p docpayload.FrozenPayloadV1, version string) error {
	if p.Seller.LogoSHA256 != "" && !logoTemplates[version] {
		return fmt.Errorf("%w: template %s prints no logo", ErrUnsupported, version)
	}

	return nil
}

// supported returns the labels of p's language, or ErrUnsupported for a
// version, language, type, regime or currency the templates cannot
// print.
func supported(p docpayload.FrozenPayloadV1) (labels, error) {
	if p.Version != docpayload.Version1 {
		return labels{}, fmt.Errorf("%w: version %d", ErrUnsupported, p.Version)
	}
	l, ok := labelsByLanguage[p.Language]
	if !ok {
		return labels{}, fmt.Errorf("%w: language %q", ErrUnsupported, p.Language)
	}
	if _, ok := l.Title[p.Type]; !ok {
		return labels{}, fmt.Errorf("%w: type %q", ErrUnsupported, p.Type)
	}
	if p.VATRegime != docpayload.RegimeStandard && p.VATRegime != docpayload.RegimeKleinunternehmer {
		return labels{}, fmt.Errorf("%w: vat regime %q", ErrUnsupported, p.VATRegime)
	}
	if p.Currency != docpayload.CurrencyEUR {
		return labels{}, fmt.Errorf("%w: currency %q", ErrUnsupported, p.Currency)
	}

	return l, nil
}

// finish builds the template data, applies the version's limits and
// checks the data as printed. Every problem is reported at once.
func finish(p docpayload.FrozenPayloadV1, l labels, draft bool, version string, pr *printer) (Input, []byte, error) {
	in := build(p, l, draft, version, pr)
	if len(p.Lines) > pr.lim.Lines {
		pr.add("lines", fmt.Sprintf("at most %d lines can be printed", pr.lim.Lines))
	}
	if len(pr.problems) > 0 {
		return Input{}, nil, &InvalidPayloadError{Details: pr.problems}
	}
	// Only now, with every field fine: a hit here is a renderer bug.
	if err := pr.checkData(in); err != nil {
		return Input{}, nil, err
	}
	pages, tooTall := pr.m.estimatePages(in)
	for _, path := range tooTall {
		pr.add(path, "too long to fit on one page")
	}
	if pages > pr.lim.Pages {
		pr.add("lines", fmt.Sprintf("the document needs up to %d pages, at most %d can be printed", pages, pr.lim.Pages))
	}
	if len(pr.problems) > 0 {
		return Input{}, nil, &InvalidPayloadError{Details: pr.problems}
	}
	data, err := MarshalInput(in)
	if err != nil {
		return Input{}, nil, err
	}
	if len(data) > pr.lim.DataBytes {
		return Input{}, nil, fmt.Errorf("%w: %d bytes of template data, limit %d", ErrPayloadTooLarge, len(data), pr.lim.DataBytes)
	}

	return in, data, nil
}

// build formats p. Every payload string passes through pr, which records
// what cannot be printed under the payload's field path. Nothing here
// returns early, so every field is checked.
func build(p docpayload.FrozenPayloadV1, l labels, draft bool, version string, pr *printer) Input {
	lang := p.Language
	money := func(c int64) string { return formatCents(c, p.Currency, lang) }
	// A date is checked as entered and fitted as printed.
	date := func(path, s string, c cell) string {
		v := formatDate(pr.line(path, s), lang)
		pr.fit(path, v, c)

		return v
	}
	isOffer := p.Type == docpayload.TypeOffer
	showVAT := p.VATRegime == docpayload.RegimeStandard
	pr.showVAT = showVAT
	title := l.Title[p.Type]

	seller := checkedSeller(p.Seller, pr)
	customer := checkedCustomer(p.Customer, pr)
	number := pr.line("number", p.Number, cellMetaValue)

	in := Input{
		TemplateVersion: version,
		Lang:            lang,
		Draft:           draft,
		Logo:            p.Seller.LogoSHA256 != "",
		Watermark:       l.Watermark,
		Title:           title,
		SenderLine:      pr.composed("seller.address", senderLine(seller), cellSender),
		SellerName:      seller.LegalName,
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
		FooterText:  pr.footerText("seller.footer_text", p.Seller.FooterText),
	}
	in.RecipientLines, in.RecipientPaths = recipientLines(customer, seller.Address.CountryCode, l, pr)
	in.Footer, in.FooterPaths = footerColumns(seller, l, pr)
	if draft {
		in.DocumentTitle = title + " (" + l.Watermark + ")"
	} else {
		in.DocumentTitle = title + " " + number
	}

	// Meta block: section 14 Abs. 4 Nr. 3, 4 and 6, plus the references.
	if !draft {
		in.Meta = append(in.Meta, Pair{l.Number[p.Type], number, "number"})
	}
	if p.IssueDate != "" {
		in.Meta = append(in.Meta, Pair{l.Date, date("issue_date", p.IssueDate, cellMetaValue), "issue_date"})
	}
	if sp := p.ServicePeriod; sp != nil {
		from := date("service_period.from", sp.From, cellMetaValue)
		to := date("service_period.to", sp.To, cellMetaValue)
		if sp.From == sp.To {
			in.Meta = append(in.Meta, Pair{l.ServiceDate, from, "service_period.from"})
		} else {
			period := pr.composed("service_period", from+" "+l.PeriodTo+" "+to, cellMetaValue)
			in.Meta = append(in.Meta, Pair{l.ServicePeriod, period, "service_period"})
		}
	}
	if customer.Number != "" {
		in.Meta = append(in.Meta, Pair{l.CustomerNumber, customer.Number, "customer.number"})
	}
	if p.CustomerReference != "" {
		in.Meta = append(in.Meta, Pair{
			l.CustomerReference, pr.line("customer_reference", p.CustomerReference, cellMetaValue), "customer_reference",
		})
	}
	if customer.VATID != "" {
		in.Meta = append(in.Meta, Pair{l.CustomerVATID, customer.VATID, "customer.vat_id"})
	}
	if isOffer && p.ValidUntil != "" {
		in.Meta = append(in.Meta, Pair{l.ValidUntil, date("valid_until", p.ValidUntil, cellMetaValue), "valid_until"})
	}
	if !isOffer && p.DueDate != "" {
		in.Meta = append(in.Meta, Pair{l.DueDate, date("due_date", p.DueDate, cellMetaValue), "due_date"})
	}
	if c := p.Corrects; c != nil {
		in.CorrectsLine = pr.composed("corrects.number",
			l.Corrects+" "+pr.line("corrects.number", c.Number, cellFullText)+" "+l.CorrectsOf+" "+
				date("corrects.issue_date", c.IssueDate, cellFullText)+".", cellFullText)
	}

	// Nr. 5 and Nr. 7: quantity, kind and the reductions per line. The
	// figures never wrap: each must fit its fixed column on one line.
	for i, line := range p.Lines {
		path := fmt.Sprintf("lines[%d].", i)
		row := LineRow{
			Position:    pr.fitWhole(path+"position", fmt.Sprint(line.Position), cellPosition),
			Description: pr.description(path+"description", line.Description),
			Quantity:    pr.fitWhole(path+"quantity_thousandths", formatQuantity(line.QuantityThousandths, lang), cellQuantity),
			Unit:        pr.unit(path+"unit", line.Unit),
			UnitPrice:   pr.fitWhole(path+"unit_price_cents", money(line.UnitPriceCents), cellUnitPrice),
			Amount:      pr.fitWhole(path+"net_after_line_discount_cents", money(line.NetAfterLineDiscountCents), cellAmount),
		}
		if showVAT {
			row.VATRate = pr.fitWhole(path+"vat_rate_permyriad", formatPercent(int64(line.VATRatePermyriad), lang), cellVAT)
		}
		if d := discountText(line.LineDiscount, p.Currency, lang); d != "" {
			row.Discount = l.Discount + " " + d
			pr.fit(path+"line_discount", row.Discount, cellDiscount(showVAT))
		}
		in.Lines = append(in.Lines, row)
	}

	in.Totals = totalRows(p, l, isOffer, showVAT)
	for _, t := range in.Totals {
		label, value := cellTotalLabel, cellTotalValue
		if t.Strong {
			label, value = cellTotalStrongLabel, cellTotalStrong
		}
		pr.fit(t.Path, t.Label, label)
		pr.fitWhole(t.Path, t.Value, value)
	}

	// Nr. 8 note (exemption, reverse charge) or the section 19 note, as
	// frozen, then payment terms (never on an offer) and free notes.
	if lines := pr.note("tax_note", p.TaxNote); len(lines) > 0 {
		in.Sections = append(in.Sections, Section{Lines: lines, Path: "tax_note"})
	}
	if !isOffer {
		if lines := pr.note("payment_terms", p.PaymentTerms); len(lines) > 0 {
			in.Sections = append(in.Sections, Section{Title: l.PaymentTerms, Lines: lines, Path: "payment_terms"})
		}
	}
	if lines := pr.note("notes", p.Notes); len(lines) > 0 {
		in.Sections = append(in.Sections, Section{Title: l.Notes, Lines: lines, Path: "notes"})
	}

	return in
}

// checkedSeller passes every printed seller string through pr.
func checkedSeller(s docpayload.SellerSnapshot, pr *printer) docpayload.SellerSnapshot {
	s.LegalName = pr.seller("seller.legal_name", s.LegalName, cellLetterhead, cellSender)
	s.Address = docpayload.Address{
		Street:     pr.seller("seller.address.street", s.Address.Street, cellSender),
		Line2:      pr.seller("seller.address.line2", s.Address.Line2),
		PostalCode: pr.seller("seller.address.postal_code", s.Address.PostalCode, cellSender),
		City:       pr.seller("seller.address.city", s.Address.City, cellSender),
		// Never printed: it only decides whether the customer's country
		// line is printed.
		CountryCode: s.Address.CountryCode,
	}
	s.TaxNumber = pr.seller("seller.tax_number", s.TaxNumber)
	s.VATID = pr.seller("seller.vat_id", s.VATID)
	s.KleinunternehmerID = pr.seller("seller.kleinunternehmer_id", s.KleinunternehmerID)
	s.BankName = pr.seller("seller.bank_name", s.BankName)
	// The IBAN prints in groups of four, so its words are checked as
	// grouped and its length as entered.
	pr.limit("seller.iban", s.IBAN, pr.lim.SellerChars)
	s.IBAN = groupIBAN(s.IBAN)
	pr.check("seller.iban", s.IBAN, false)
	pr.fit("seller.iban", s.IBAN, cellFooter)
	s.BIC = pr.seller("seller.bic", s.BIC)
	s.Email = pr.seller("seller.email", s.Email)
	s.Phone = pr.seller("seller.phone", s.Phone)
	s.Website = pr.seller("seller.website", s.Website)

	return s
}

// checkedCustomer passes every printed customer string through pr.
func checkedCustomer(c docpayload.CustomerSnapshot, pr *printer) docpayload.CustomerSnapshot {
	c.Number = pr.line("customer.number", c.Number, cellMetaValue)
	c.Name = pr.line("customer.name", c.Name, cellAddress)
	c.Address = checkedAddress("customer.address", c.Address, pr)
	c.VATID = pr.line("customer.vat_id", c.VATID, cellMetaValue)

	return c
}

func checkedAddress(path string, a docpayload.Address, pr *printer) docpayload.Address {
	a.Street = pr.line(path+".street", a.Street, cellAddress)
	a.Line2 = pr.line(path+".line2", a.Line2, cellAddress)
	a.PostalCode = pr.line(path+".postal_code", a.PostalCode, cellAddress)
	a.City = pr.line(path+".city", a.City, cellAddress)
	a.CountryCode = pr.country(path+".country_code", a.CountryCode)

	return a
}

// MarshalInput encodes the template data as data.json, indented so a
// golden file diff reads line by line.
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
		rows = append(rows, TotalRow{Label: l.NetTotal, Value: money(t.NetTotalCents), Path: "totals.net_total_cents"})
	}
	if hasDiscount {
		label := l.DocumentDiscount
		if d := p.DocumentDiscount.PercentPermyriad; d != nil && *d != 0 {
			label += " " + formatPercent(int64(*d), lang)
		}
		rows = append(rows,
			TotalRow{Label: label, Value: money(-t.DocumentDiscountCents), Path: "totals.document_discount_cents"},
			TotalRow{
				Label: l.NetAfterDiscount, Value: money(t.NetAfterDocumentDiscountCents),
				Path: "totals.net_after_document_discount_cents",
			},
		)
	}
	if showVAT {
		for i, b := range t.VAT {
			rows = append(rows, TotalRow{
				Label: l.VAT + " " + formatPercent(int64(b.RatePermyriad), lang) + " " + l.VATOn + " " + money(b.NetCents),
				Value: money(b.VATCents),
				Path:  fmt.Sprintf("totals.vat[%d]", i),
			})
		}
	}
	total := l.GrandTotal
	if isOffer {
		total = l.OfferTotal
	}
	rows = append(rows, TotalRow{Label: total, Value: money(t.GrandTotalCents), Strong: true, Path: "totals.grand_total_cents"})

	return rows
}

func cityLine(a docpayload.Address) string {
	return strings.TrimSpace(a.PostalCode + " " + a.City)
}

func senderLine(s docpayload.SellerSnapshot) string {
	parts := []string{s.LegalName, s.Address.Street, cityLine(s.Address)}

	return strings.Join(nonEmpty(parts), " · ")
}

// printedLine is one line of a composed block and the payload field it
// prints.
type printedLine struct {
	path, text string
}

// keep drops the lines without text, fits each as printed in c and
// returns the texts and paths.
func keep(pr *printer, c cell, lines []printedLine) ([]string, []string) {
	texts, paths := make([]string, 0, len(lines)), make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l.text) == "" {
			continue
		}
		texts = append(texts, pr.composed(l.path, l.text, c))
		paths = append(paths, l.path)
	}

	return texts, paths
}

// recipientLines is the address field with each line's field path. The
// country line is printed only when it differs from the seller's, in
// the document language, upper case. Every line is checked as printed.
func recipientLines(c docpayload.CustomerSnapshot, sellerCountry string, l labels, pr *printer) ([]string, []string) {
	lines := []printedLine{
		{"customer.name", c.Name},
		{"customer.address.street", c.Address.Street},
		{"customer.address.line2", c.Address.Line2},
		{"customer.address.city", cityLine(c.Address)},
	}
	if cc := c.Address.CountryCode; cc != "" && cc != sellerCountry {
		name, ok := l.Countries[cc]
		if !ok {
			name = cc
		}
		lines = append(lines, printedLine{"customer.address.country_code", pr.printed("customer.address.country_code", strings.ToUpper(name), cellAddress)})
	}

	return keep(pr, cellAddress, lines)
}

// footerColumns are the seller's data printed on every page: address,
// contact, tax identity (section 14 Abs. 4 Nr. 2) and bank, each line
// with its field path and checked as printed, label and value together.
func footerColumns(s docpayload.SellerSnapshot, l labels, pr *printer) ([][]string, [][]string) {
	labelled := func(path, label, value string) printedLine {
		if value == "" {
			return printedLine{path, ""}
		}

		return printedLine{path, label + " " + value}
	}
	cols := [][]printedLine{
		{
			{"seller.legal_name", s.LegalName},
			{"seller.address.street", s.Address.Street},
			{"seller.address.line2", s.Address.Line2},
			{"seller.address.city", cityLine(s.Address)},
		},
		{
			labelled("seller.phone", l.Phone, s.Phone),
			labelled("seller.email", l.Email, s.Email),
			labelled("seller.website", l.Web, s.Website),
		},
		{
			labelled("seller.tax_number", l.TaxNumber, s.TaxNumber),
			labelled("seller.vat_id", l.VATID, s.VATID),
			labelled("seller.kleinunternehmer_id", l.KleinunternehmerID, s.KleinunternehmerID),
		},
		{
			labelled("seller.bank_name", l.Bank, s.BankName),
			labelled("seller.iban", l.IBAN, s.IBAN),
			labelled("seller.bic", l.BIC, s.BIC),
		},
	}
	texts, paths := make([][]string, 0, len(cols)), make([][]string, 0, len(cols))
	for _, c := range cols {
		t, p := keep(pr, cellFooter, c)
		if len(t) > 0 {
			texts = append(texts, t)
			paths = append(paths, p)
		}
	}

	return texts, paths
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
