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

// Errors of BuildInput and Render.
var (
	// ErrUnsupported is a payload type, language, regime or currency this
	// template version cannot print.
	ErrUnsupported = errors.New("pdf: unsupported payload")
	// ErrImported is an imported document: its original PDF is the
	// document, solobill never renders one.
	ErrImported = errors.New("pdf: an imported document has its original pdf")
)

// InvalidPayloadError lists what a draft lacks for the template, in the
// field path scheme of docpayload.Validate. The API answers it as 400
// with these details.
type InvalidPayloadError struct {
	Details map[string]string
}

func (e *InvalidPayloadError) Error() string {
	return "pdf: payload incomplete: " + strings.Join(docpayload.SortedPaths(e.Details), ", ")
}

// draftExempt are the Validate paths a draft may fail: it has no number
// yet and is printed with the current template.
var draftExempt = map[string]bool{
	"number":           true,
	"number_value":     true,
	"number_year":      true,
	"template_version": true,
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
	title, ok := map[string]string{
		docpayload.TypeInvoice:    l.Invoice,
		docpayload.TypeCorrection: l.Correction,
		docpayload.TypeOffer:      l.Offer,
	}[p.Type]
	if !ok {
		return Input{}, fmt.Errorf("%w: type %q", ErrUnsupported, p.Type)
	}
	if p.VATRegime != docpayload.RegimeStandard && p.VATRegime != docpayload.RegimeKleinunternehmer {
		return Input{}, fmt.Errorf("%w: vat regime %q", ErrUnsupported, p.VATRegime)
	}
	if p.Currency != docpayload.CurrencyEUR {
		return Input{}, fmt.Errorf("%w: currency %q", ErrUnsupported, p.Currency)
	}
	if draft {
		problems := docpayload.Validate(p)
		for path := range problems {
			if draftExempt[path] {
				delete(problems, path)
			}
		}
		if len(problems) > 0 {
			return Input{}, &InvalidPayloadError{Details: problems}
		}
	}

	version, err := templateVersion(p, draft)
	if err != nil {
		return Input{}, err
	}

	lang := p.Language
	money := func(c int64) string { return formatCents(c, p.Currency, lang) }
	isOffer := p.Type == docpayload.TypeOffer
	showVAT := p.VATRegime == docpayload.RegimeStandard

	in := Input{
		TemplateVersion: version,
		Lang:            lang,
		Draft:           draft,
		Watermark:       l.Watermark,
		Title:           title,
		SenderLine:      senderLine(p.Seller),
		SellerName:      p.Seller.LegalName,
		RecipientLines:  recipientLines(p, l),
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
		PageLabel:   l.Page,
		PageOfLabel: l.PageOf,
		Footer:      footerColumns(p.Seller, l),
		FooterText:  splitLines(p.Seller.FooterText),
	}
	if draft {
		in.DocumentTitle = title + " (" + l.Watermark + ")"
	} else {
		in.DocumentTitle = title + " " + p.Number
	}

	// Meta block: section 14 Abs. 4 Nr. 3, 4 and 6, plus the references.
	if !draft {
		in.Meta = append(in.Meta, Pair{l.Number[p.Type], p.Number})
	}
	if p.IssueDate != "" {
		in.Meta = append(in.Meta, Pair{l.Date, formatDate(p.IssueDate, lang)})
	}
	if sp := p.ServicePeriod; sp != nil {
		if sp.From == sp.To {
			in.Meta = append(in.Meta, Pair{l.ServiceDate, formatDate(sp.From, lang)})
		} else {
			in.Meta = append(in.Meta, Pair{
				l.ServicePeriod,
				formatDate(sp.From, lang) + " " + l.PeriodTo + " " + formatDate(sp.To, lang),
			})
		}
	}
	if p.Customer.Number != "" {
		in.Meta = append(in.Meta, Pair{l.CustomerNumber, p.Customer.Number})
	}
	if p.CustomerReference != "" {
		in.Meta = append(in.Meta, Pair{l.CustomerReference, p.CustomerReference})
	}
	if p.Customer.VATID != "" {
		in.Meta = append(in.Meta, Pair{l.CustomerVATID, p.Customer.VATID})
	}
	if isOffer && p.ValidUntil != "" {
		in.Meta = append(in.Meta, Pair{l.ValidUntil, formatDate(p.ValidUntil, lang)})
	}
	if !isOffer && p.DueDate != "" {
		in.Meta = append(in.Meta, Pair{l.DueDate, formatDate(p.DueDate, lang)})
	}
	if p.Corrects != nil {
		in.CorrectsLine = l.Corrects + " " + p.Corrects.Number + " " + l.CorrectsOf + " " +
			formatDate(p.Corrects.IssueDate, lang) + "."
	}

	// Nr. 5 and Nr. 7: quantity, kind and the reductions per line.
	for _, line := range p.Lines {
		row := LineRow{
			Position:    fmt.Sprint(line.Position),
			Description: splitLines(line.Description),
			Quantity:    formatQuantity(line.QuantityThousandths, lang),
			Unit:        line.Unit,
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
	if lines := splitLines(p.TaxNote); len(lines) > 0 {
		in.Sections = append(in.Sections, Section{Lines: lines})
	}
	if lines := splitLines(p.PaymentTerms); len(lines) > 0 && !isOffer {
		in.Sections = append(in.Sections, Section{Title: l.PaymentTerms, Lines: lines})
	}
	if lines := splitLines(p.Notes); len(lines) > 0 {
		in.Sections = append(in.Sections, Section{Title: l.Notes, Lines: lines})
	}
	if in.Sections == nil {
		in.Sections = []Section{}
	}
	if in.Meta == nil {
		in.Meta = []Pair{}
	}

	return in, nil
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
func recipientLines(p docpayload.FrozenPayloadV1, l labels) []string {
	c := p.Customer
	lines := nonEmpty([]string{c.Name, c.Address.Street, c.Address.Line2, cityLine(c.Address)})
	if cc := c.Address.CountryCode; cc != "" && cc != p.Seller.Address.CountryCode {
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
