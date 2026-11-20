package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/api"

	"github.com/ostendorf-cloud/solobill/internal/docchain"
	"github.com/ostendorf-cloud/solobill/internal/docissue"
	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/money"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// registerDocuments mounts offers, invoices and corrections. Owned by
// issue #70 (I4). See docs/api.md, "Documents".
//
// The tenant always comes from the session: no request body carries it
// (an unknown field is refused). Issuing and creating a correction need
// the owner role, everything else any role. An issued document is
// immutable: every write to it answers 409, and the database triggers
// refuse it below this layer too.
func registerDocuments(mux *http.ServeMux, route routeFunc, a *app) {
	mux.Handle("GET /api/v1/documents", route(api.Handle(a.handleListDocuments)))
	mux.Handle("POST /api/v1/documents", route(api.Handle(a.handleCreateDocument)))
	mux.Handle("POST /api/v1/documents/preview", route(api.Handle(a.handlePreviewDocument)))
	mux.Handle("GET /api/v1/documents/{id}", route(api.Handle(a.handleGetDocument)))
	mux.Handle("PUT /api/v1/documents/{id}", route(api.Handle(a.handleUpdateDocument)))
	mux.Handle("DELETE /api/v1/documents/{id}", route(api.Handle(a.handleDeleteDocument)))
	mux.Handle("POST /api/v1/documents/{id}/issue", route(api.Handle(a.handleIssueDocument)))
	mux.Handle("POST /api/v1/documents/{id}/correction", route(api.Handle(a.handleCreateCorrection)))
	mux.Handle("POST /api/v1/offers/{id}/to-invoice", route(api.Handle(a.handleOfferToInvoice)))
}

// Error codes of docs/api.md that govue's api package has no constant
// for.
const (
	codeVATRegimeMissing      = "vat_regime_missing"
	codeVATRegimeMismatch     = "vat_regime_mismatch"
	codeCorrectionBaseChanged = "correction_base_changed"
)

const documentDateLayout = docpayload.DateLayout

// documentLineJSON is a line of the document JSON of docs/api.md.
// Position counts from 1.
type documentLineJSON struct {
	Position                     int32      `json:"position"`
	CatalogueItemID              *uuid.UUID `json:"catalogue_item_id"`
	Description                  string     `json:"description"`
	QuantityThousandths          int64      `json:"quantity_thousandths"`
	Unit                         string     `json:"unit"`
	UnitPriceCents               int64      `json:"unit_price_cents"`
	VATRatePermyriad             int32      `json:"vat_rate_permyriad"`
	LineDiscountPercentPermyriad *int32     `json:"line_discount_percent_permyriad"`
	LineDiscountAmountCents      *int64     `json:"line_discount_amount_cents"`
}

// documentJSON is the document JSON of docs/api.md. Lines is omitted in
// a list. Totals is computed by internal/money for a draft and read from
// the frozen payload for an issued document, never recomputed for it.
type documentJSON struct {
	ID                       uuid.UUID           `json:"id"`
	Type                     string              `json:"type"`
	State                    string              `json:"state"`
	CustomerID               uuid.UUID           `json:"customer_id"`
	CorrectsDocumentID       *uuid.UUID          `json:"corrects_document_id"`
	SourceOfferID            *uuid.UUID          `json:"source_offer_id"`
	VATRegime                string              `json:"vat_regime"`
	Language                 string              `json:"language"`
	ServiceDate              *string             `json:"service_date"`
	ServiceDateTo            *string             `json:"service_date_to"`
	DueDate                  *string             `json:"due_date"`
	ValidUntil               *string             `json:"valid_until"`
	DiscountPercentPermyriad *int32              `json:"discount_percent_permyriad"`
	DiscountAmountCents      *int64              `json:"discount_amount_cents"`
	PaymentTerms             *string             `json:"payment_terms"`
	Notes                    *string             `json:"notes"`
	CustomerReference        *string             `json:"customer_reference"`
	Number                   *string             `json:"number"`
	NumberValue              *int64              `json:"number_value"`
	NumberYear               *int32              `json:"number_year"`
	CorrectionSeq            *int32              `json:"correction_seq"`
	CorrectionBaseID         *uuid.UUID          `json:"correction_base_id"`
	CorrectionBaseSeq        *int32              `json:"correction_base_seq"`
	OriginalNumber           *string             `json:"original_number"`
	Imported                 bool                `json:"imported"`
	ChainStatus              *string             `json:"chain_status"`
	IssuedAt                 *time.Time          `json:"issued_at"`
	FrozenPayloadSHA256      *string             `json:"frozen_payload_sha256"`
	Lines                    *[]documentLineJSON `json:"lines,omitempty"`
	Totals                   *docpayload.Totals  `json:"totals"`
}

func documentDate(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(documentDateLayout)

	return &s
}

// documentTotals is the totals of d: the frozen ones of an issued
// document, internal/money's for a draft with lines, nil otherwise.
func documentTotals(d storage.Document, lines []storage.DocumentLine) *docpayload.Totals {
	if d.State == storage.DocumentStateIssued {
		p, err := docpayload.Unmarshal(d.FrozenPayload)
		if err != nil {
			return nil
		}

		return &p.Totals
	}
	if len(lines) == 0 {
		return nil
	}
	res, err := docissue.Calculate(d, lines)
	if err != nil {
		return nil
	}
	t := docpayload.TotalsFromMoney(res)

	return &t
}

// toDocumentJSON builds the document JSON. withLines adds the lines and
// computes a draft's totals from them. status is the derived chain
// status, "" for none.
func toDocumentJSON(d storage.Document, withLines bool, status docchain.Status) documentJSON {
	lang := d.Language
	if lang == "" {
		lang = storage.DocumentLanguageGerman
	}
	out := documentJSON{
		ID:                       d.ID,
		Type:                     string(d.Type),
		State:                    string(d.State),
		CustomerID:               d.CustomerID,
		CorrectsDocumentID:       d.CorrectsDocumentID,
		SourceOfferID:            d.SourceOfferID,
		VATRegime:                string(d.VATRegime),
		Language:                 string(lang),
		ServiceDate:              documentDate(d.ServiceDate),
		ServiceDateTo:            documentDate(d.ServiceDateTo),
		DueDate:                  documentDate(d.DueDate),
		ValidUntil:               documentDate(d.ValidUntil),
		DiscountPercentPermyriad: d.DiscountPercentPermyriad,
		DiscountAmountCents:      d.DiscountAmountCents,
		PaymentTerms:             d.PaymentTerms,
		Notes:                    d.Notes,
		CustomerReference:        d.CustomerReference,
		Number:                   d.DisplayNumber,
		NumberValue:              d.Number,
		NumberYear:               d.NumberYear,
		CorrectionSeq:            d.CorrectionSeq,
		CorrectionBaseID:         d.CorrectionBaseID,
		CorrectionBaseSeq:        d.CorrectionBaseSeq,
		OriginalNumber:           d.OriginalNumber,
		Imported:                 d.Imported,
		IssuedAt:                 masterDataUTC(d.IssuedAt),
		FrozenPayloadSHA256:      d.FrozenPayloadSHA256,
	}
	if status != "" {
		s := string(status)
		out.ChainStatus = &s
	}
	if withLines {
		lines := make([]documentLineJSON, len(d.Lines))
		for i, l := range d.Lines {
			lines[i] = documentLineJSON{
				Position:                     l.Position + 1,
				CatalogueItemID:              l.CatalogueItemID,
				Description:                  l.Description,
				QuantityThousandths:          l.QuantityThousandths,
				Unit:                         l.Unit,
				UnitPriceCents:               l.UnitPriceCents,
				VATRatePermyriad:             l.VATRatePermyriad,
				LineDiscountPercentPermyriad: l.LineDiscountPercentPermyriad,
				LineDiscountAmountCents:      l.LineDiscountAmountCents,
			}
		}
		out.Lines = &lines
		out.Totals = documentTotals(d, d.Lines)
	} else if d.State == storage.DocumentStateIssued {
		out.Totals = documentTotals(d, nil)
	}

	return out
}

// documentLineRequest is a line of the request body. Position is
// accepted for symmetry with the response and ignored: the order of the
// array is the order of the lines.
type documentLineRequest struct {
	Position                     *int32     `json:"position"`
	CatalogueItemID              *uuid.UUID `json:"catalogue_item_id"`
	Description                  string     `json:"description"`
	QuantityThousandths          int64      `json:"quantity_thousandths"`
	Unit                         string     `json:"unit"`
	UnitPriceCents               int64      `json:"unit_price_cents"`
	VATRatePermyriad             int32      `json:"vat_rate_permyriad"`
	LineDiscountPercentPermyriad *int32     `json:"line_discount_percent_permyriad"`
	LineDiscountAmountCents      *int64     `json:"line_discount_amount_cents"`
}

// documentRequest is the body of POST /documents, PUT /documents/{id}
// and POST /documents/preview. It has no tenant, no state, no number and
// no chain field: those come from the session and the server.
type documentRequest struct {
	Type                     string                `json:"type"`
	CustomerID               *uuid.UUID            `json:"customer_id"`
	VATRegime                *string               `json:"vat_regime"`
	Language                 *string               `json:"language"`
	ServiceDate              *string               `json:"service_date"`
	ServiceDateTo            *string               `json:"service_date_to"`
	DueDate                  *string               `json:"due_date"`
	ValidUntil               *string               `json:"valid_until"`
	DiscountPercentPermyriad *int32                `json:"discount_percent_permyriad"`
	DiscountAmountCents      *int64                `json:"discount_amount_cents"`
	PaymentTerms             *string               `json:"payment_terms"`
	Notes                    *string               `json:"notes"`
	CustomerReference        *string               `json:"customer_reference"`
	Lines                    []documentLineRequest `json:"lines"`
}

// documentProblems collects field errors under the path scheme of
// docs/api.md.
type documentProblems map[string]string

func (p documentProblems) add(path, reason string) {
	if reason == "" {
		return
	}
	if _, seen := p[path]; !seen {
		p[path] = reason
	}
}

func (p documentProblems) err() error {
	if len(p) == 0 {
		return nil
	}

	return profileInvalid("the document has invalid values", p)
}

func parseDocumentDate(p documentProblems, path string, s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse(documentDateLayout, *s)
	if err != nil {
		p.add(path, "must be a date such as 2026-11-30")

		return nil
	}

	return &t
}

// documentText trims nothing and maps an empty value to nil: a printed
// text is frozen exactly as entered. It checks the text against the
// limits and the characters of the template, so a draft that saves can
// be printed as far as its own texts go.
func documentText(p documentProblems, path string, s *string, multiline bool) *string {
	if s == nil || *s == "" {
		return nil
	}
	if multiline {
		p.add(path, docpayload.TextFieldProblem(*s, docpayload.MaxTextChars, docpayload.MaxTextLines))
	} else {
		p.add(path, docpayload.LineFieldProblem(*s, docpayload.MaxLineChars))
	}

	return s
}

// draft validates the request for a document of docType and builds the
// header and the lines. The customer, the regime and the language are
// resolved by the caller inside the tenant scope.
func (r documentRequest) draft(docType storage.DocumentType, p documentProblems) (storage.Document, []storage.DocumentLine) {
	d := storage.Document{Type: docType, State: storage.DocumentStateDraft}

	if r.VATRegime != nil {
		switch regime := storage.VATRegime(*r.VATRegime); regime {
		case storage.VATRegimeStandard, storage.VATRegimeKleinunternehmer:
			d.VATRegime = regime
		default:
			p.add("vat_regime", "must be standard or kleinunternehmer")
		}
	}
	if r.Language != nil {
		switch lang := storage.DocumentLanguage(*r.Language); lang {
		case storage.DocumentLanguageGerman, storage.DocumentLanguageEnglish:
			d.Language = lang
		default:
			p.add("language", "must be de or en")
		}
	}

	d.ServiceDate = parseDocumentDate(p, "service_date", r.ServiceDate)
	d.ServiceDateTo = parseDocumentDate(p, "service_date_to", r.ServiceDateTo)
	d.DueDate = parseDocumentDate(p, "due_date", r.DueDate)
	d.ValidUntil = parseDocumentDate(p, "valid_until", r.ValidUntil)
	if d.ServiceDateTo != nil && d.ServiceDate == nil {
		p.add("service_date", "required with service_date_to")
	}
	if d.ServiceDate != nil && d.ServiceDateTo != nil && d.ServiceDateTo.Before(*d.ServiceDate) {
		p.add("service_date_to", "must not be before service_date")
	}
	if docType == storage.DocumentTypeOffer {
		if d.ServiceDate != nil {
			p.add("service_date", "an offer has no service period")
		}
		if d.ServiceDateTo != nil {
			p.add("service_date_to", "an offer has no service period")
		}
	} else if d.ValidUntil != nil {
		p.add("valid_until", "only an offer has a validity date")
	}

	d.DiscountPercentPermyriad = r.DiscountPercentPermyriad
	d.DiscountAmountCents = r.DiscountAmountCents
	checkDiscount(p, "discount", r.DiscountPercentPermyriad, r.DiscountAmountCents)

	d.PaymentTerms = documentText(p, "payment_terms", r.PaymentTerms, true)
	d.Notes = documentText(p, "notes", r.Notes, true)
	d.CustomerReference = documentText(p, "customer_reference", r.CustomerReference, false)

	if len(r.Lines) > docpayload.MaxLines {
		p.add("lines", fmt.Sprintf("at most %d lines", docpayload.MaxLines))
	}
	lines := make([]storage.DocumentLine, len(r.Lines))
	for i, l := range r.Lines {
		path := fmt.Sprintf("lines[%d].", i)
		if l.Description == "" {
			p.add(path+"description", "required")
		} else {
			p.add(path+"description", docpayload.TextFieldProblem(l.Description, docpayload.MaxTextChars, docpayload.MaxTextLines))
		}
		if l.QuantityThousandths <= 0 {
			p.add(path+"quantity_thousandths", "must be positive")
		}
		switch l.Unit {
		case "piece", "hour", "day":
		default:
			p.add(path+"unit", "must be piece, hour or day")
		}
		if l.UnitPriceCents < 0 {
			p.add(path+"unit_price_cents", "must not be negative")
		}
		if l.VATRatePermyriad < 0 || l.VATRatePermyriad > 10000 {
			p.add(path+"vat_rate_permyriad", "must be 0 to 10000")
		}
		checkDiscount(p, path+"line_discount", l.LineDiscountPercentPermyriad, l.LineDiscountAmountCents)
		lines[i] = storage.DocumentLine{
			Position:                     int32(i), //nolint:gosec // at most docpayload.MaxLines
			CatalogueItemID:              l.CatalogueItemID,
			Description:                  l.Description,
			QuantityThousandths:          l.QuantityThousandths,
			Unit:                         l.Unit,
			UnitPriceCents:               l.UnitPriceCents,
			VATRatePermyriad:             l.VATRatePermyriad,
			LineDiscountPercentPermyriad: l.LineDiscountPercentPermyriad,
			LineDiscountAmountCents:      l.LineDiscountAmountCents,
		}
	}

	return d, lines
}

// checkDiscount checks a percentage or an amount, never both. prefix is
// "discount" or "lines[i].line_discount".
func checkDiscount(p documentProblems, prefix string, percent *int32, amount *int64) {
	if percent != nil && amount != nil {
		p.add(prefix+"_amount_cents", "a percentage and an amount are mutually exclusive")
	}
	if percent != nil && (*percent < 0 || *percent > 10000) {
		p.add(prefix+"_percent_permyriad", "must be 0 to 10000")
	}
	if amount != nil && *amount < 0 {
		p.add(prefix+"_amount_cents", "must not be negative")
	}
}

// checkMoney runs internal/money over a draft with lines. A draft that
// saves always has totals.
// Under the Kleinunternehmer regime every line's rate must be 0 (section
// 19 UStG); the path names the line, before internal/money refuses the
// whole calculation.
func checkMoney(p documentProblems, d storage.Document, lines []storage.DocumentLine) *money.Result {
	if d.VATRegime == storage.VATRegimeKleinunternehmer {
		for i, l := range lines {
			if l.VATRatePermyriad != 0 {
				p.add(fmt.Sprintf("lines[%d].vat_rate_permyriad", i), "must be 0 under the Kleinunternehmer regime")
			}
		}
	}
	if len(p) > 0 || len(lines) == 0 || d.VATRegime == "" {
		return nil
	}
	res, err := docissue.Calculate(d, lines)
	if err != nil {
		p.add("lines", "the lines cannot be calculated: "+err.Error())

		return nil
	}

	return &res
}

// documentsWired reports whether every repository this file needs is
// wired. Until then the routes answer 501, as their stubs did.
func (a *app) documentsWired() bool {
	r := a.repos

	return r.Scope != nil && r.Documents != nil && r.DocumentNumbers != nil && r.Customers != nil &&
		r.VATRegimePeriods != nil && r.SellerProfiles != nil
}

// documentsCaller is requireSession plus the wiring check. owner adds
// the owner role.
func (a *app) documentsCaller(w http.ResponseWriter, r *http.Request, owner bool) (caller, bool, error) {
	c, err := a.requireSession(r)
	if err != nil {
		return caller{}, false, err
	}
	if !a.documentsWired() {
		return caller{}, false, a.notImplemented(w, r)
	}
	if owner && c.role != storage.AccountRoleOwner {
		return caller{}, false, api.PermissionDenied("owner role required")
	}

	return c, true, nil
}

// documentsInTenant runs fn in the caller's tenant scope. An *api.Error
// from fn passes through, a repository sentinel becomes 404 or 409,
// anything else an internal error.
func (a *app) documentsInTenant(ctx context.Context, c caller, fn func(ctx context.Context) error) error {
	err := a.inTenant(ctx, c.tenantID, fn)

	return documentsError(err)
}

func documentsError(err error) error {
	if err == nil {
		return nil
	}
	// api.AsError would classify every other error as internal, so the
	// *api.Error a handler returned is picked out with errors.As.
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}

	var invalid *docissue.ValidationError
	var mismatch *docissue.RegimeMismatchError
	var baseChanged *docissue.BaseChangedError

	switch {
	case errors.As(err, &invalid):
		return profileInvalid("the document is incomplete", invalid.Details)
	case errors.As(err, &mismatch):
		return api.New(http.StatusConflict, codeVATRegimeMismatch,
			"the draft's VAT regime differs from the regime of the period on the issue date").
			WithDetails(map[string]any{"vat_regime": string(mismatch.Regime)})
	case errors.As(err, &baseChanged):
		return api.New(http.StatusConflict, codeCorrectionBaseChanged,
			"another correction was issued since this draft was created; start a new correction").
			WithDetails(map[string]any{"correction_base_seq": strconv.Itoa(baseChanged.Seq)})
	case errors.Is(err, docissue.ErrRegimeMissing):
		return api.New(http.StatusBadRequest, codeVATRegimeMissing, "no VAT regime period covers the issue date")
	case errors.Is(err, docissue.ErrNotDraft):
		return api.Conflict("the document is issued already")
	case errors.Is(err, docissue.ErrNumberTaken):
		return api.Conflict("the number exists already in this tenant")
	case errors.Is(err, storage.ErrNotFound):
		return api.NotFound("not found")
	case errors.Is(err, storage.ErrConflict):
		return api.Conflict("the document's state forbids this change")
	default:
		return api.Internal(fmt.Errorf("httpapi: documents: %w", err))
	}
}

// documentID parses the {id} path value; an id that is no UUID answers
// 404 like a missing one.
func documentID(r *http.Request) (uuid.UUID, error) {
	return masterDataID(r)
}

// decodeEmptyBody accepts an empty body or an empty JSON object and
// refuses anything else, so an endpoint documented with an empty body
// never silently ignores a field such as a number or an issue date.
func decodeEmptyBody(r *http.Request) error {
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, api.DefaultMaxBytes))
	if err != nil {
		return api.New(http.StatusRequestEntityTooLarge, api.CodeRequestTooLarge, "the body is too large")
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	clone := r.Clone(r.Context())
	clone.Body = io.NopCloser(bytes.NewReader(body))
	var empty struct{}

	return api.Decode(clone, &empty)
}

// resolveDraft completes a draft inside the tenant scope: the customer
// must exist and be active, the regime defaults to the period of today
// and only without any period to the profile's default, the language to
// the customer's. Catalogue items must exist.
func (a *app) resolveDraft(ctx context.Context, d *storage.Document, lines []storage.DocumentLine, regimeGiven bool, p documentProblems) error {
	customer, err := a.repos.Customers.FindByID(ctx, d.CustomerID)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		p.add("customer_id", "no such customer")
	case err != nil:
		return err
	case customer.DeletedAt != nil:
		p.add("customer_id", "the customer is deleted")
	case d.Language == "":
		d.Language = customer.DocumentLanguage
	}
	if d.Language == "" {
		d.Language = storage.DocumentLanguageGerman
	}

	if !regimeGiven {
		regime, err := a.defaultRegime(ctx)
		if err != nil {
			return err
		}
		if regime == "" {
			p.add("vat_regime", "required: no VAT regime period and no profile default exist")
		}
		d.VATRegime = regime
	}

	if a.repos.CatalogueItems != nil {
		for i, l := range lines {
			if l.CatalogueItemID == nil {
				continue
			}
			if _, err := a.repos.CatalogueItems.FindByID(ctx, *l.CatalogueItemID); errors.Is(err, storage.ErrNotFound) {
				p.add(fmt.Sprintf("lines[%d].catalogue_item_id", i), "no such catalogue item")
			} else if err != nil {
				return err
			}
		}
	}

	return nil
}

// defaultRegime is the regime a new draft starts with: the one of the
// effective period that applies today, or without any period the
// profile's default_vat_regime, or "" without either.
func (a *app) defaultRegime(ctx context.Context) (storage.VATRegime, error) {
	period, err := a.repos.VATRegimePeriods.RegimeOn(ctx, docissue.IssueDate(time.Now()))
	if err == nil {
		return period.Regime, nil
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return "", err
	}
	periods, err := a.repos.VATRegimePeriods.List(ctx)
	if err != nil {
		return "", err
	}
	if len(periods) > 0 {
		// Periods exist, but none covers today: the default must never
		// decide a treatment the history speaks about.
		return "", nil
	}
	profile, err := a.repos.SellerProfiles.Get(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return profile.DefaultVATRegime, nil
}

// documentChainStatus derives the chain status of one issued invoice or
// correction.
func (a *app) documentChainStatus(ctx context.Context, d storage.Document) (docchain.Status, error) {
	if d.State != storage.DocumentStateIssued {
		return "", nil
	}
	root, ok := docissue.RootOf(d)
	if !ok {
		return "", nil
	}
	chain, err := a.repos.Documents.Chain(ctx, root)
	if err != nil {
		return "", err
	}

	return docissue.Statuses(chain)[d.ID], nil
}

func (a *app) handleListDocuments(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.documentsCaller(w, r, false)
	if !ok {
		return err
	}

	q := r.URL.Query()
	p := documentProblems{}
	typeFilter := q.Get("type")
	switch storage.DocumentType(typeFilter) {
	case "", storage.DocumentTypeInvoice, storage.DocumentTypeCorrection, storage.DocumentTypeOffer:
	default:
		p.add("type", "must be invoice, correction or offer")
	}
	stateFilter := q.Get("state")
	switch storage.DocumentState(stateFilter) {
	case "", storage.DocumentStateDraft, storage.DocumentStateIssued:
	default:
		p.add("state", "must be draft or issued")
	}
	yearFilter := 0
	if s := q.Get("year"); s != "" {
		yearFilter, err = strconv.Atoi(s)
		if err != nil || yearFilter < 1000 || yearFilter > 9999 {
			p.add("year", "must be a four digit year")
		}
	}
	if err := p.err(); err != nil {
		return err
	}

	var all []storage.Document
	err = a.documentsInTenant(r.Context(), c, func(ctx context.Context) error {
		var err error
		all, err = a.repos.Documents.List(ctx)

		return err
	})
	if err != nil {
		return err
	}

	statuses := docissue.Statuses(all)
	out := make([]documentJSON, 0, len(all))
	for _, d := range all {
		if typeFilter != "" && string(d.Type) != typeFilter {
			continue
		}
		if stateFilter != "" && string(d.State) != stateFilter {
			continue
		}
		if yearFilter != 0 && documentYear(d) != yearFilter {
			continue
		}
		out = append(out, toDocumentJSON(d, false, statuses[d.ID]))
	}

	return api.Write(w, http.StatusOK, map[string]any{"documents": out})
}

// documentYear is the year the list's year filter matches: the number
// year of an issued document, the creation year (in Germany) of a
// draft.
func documentYear(d storage.Document) int {
	if d.NumberYear != nil {
		return int(*d.NumberYear)
	}

	return docissue.IssueDate(d.CreatedAt).Year()
}

func (a *app) handleCreateDocument(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.documentsCaller(w, r, false)
	if !ok {
		return err
	}

	var req documentRequest
	if err := api.Decode(r, &req); err != nil {
		return err
	}
	p := documentProblems{}
	docType := storage.DocumentType(req.Type)
	if docType != storage.DocumentTypeInvoice && docType != storage.DocumentTypeOffer {
		// A correction draft is created by POST /documents/{id}/correction.
		p.add("type", "must be invoice or offer")
	}
	if req.CustomerID == nil {
		p.add("customer_id", "required")
	}
	draft, lines := req.draft(docType, p)
	if err := p.err(); err != nil {
		return err
	}
	draft.CustomerID = *req.CustomerID
	// The tenant comes from the session, never from the body.
	draft.TenantID = c.tenantID

	var created storage.Document
	err = a.documentsInTenant(r.Context(), c, func(ctx context.Context) error {
		if err := a.resolveDraft(ctx, &draft, lines, req.VATRegime != nil, p); err != nil {
			return err
		}
		checkMoney(p, draft, lines)
		if err := p.err(); err != nil {
			return err
		}
		var err error
		created, err = a.repos.Documents.Create(ctx, draft, lines)

		return err
	})
	if err != nil {
		return err
	}

	return api.Write(w, http.StatusCreated, toDocumentJSON(created, true, ""))
}

// previewLineJSON is a line of the preview response.
type previewLineJSON struct {
	NetAfterLineDiscountCents  int64 `json:"net_after_line_discount_cents"`
	DocumentDiscountShareCents int64 `json:"document_discount_share_cents"`
	NetAfterAllDiscountsCents  int64 `json:"net_after_all_discounts_cents"`
}

// handlePreviewDocument answers POST /api/v1/documents/preview: the
// totals of a body as POST /documents takes it, computed by
// internal/money. Nothing is stored. The Vue app never computes money.
func (a *app) handlePreviewDocument(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.documentsCaller(w, r, false)
	if !ok {
		return err
	}

	var req documentRequest
	if err := api.Decode(r, &req); err != nil {
		return err
	}
	p := documentProblems{}
	docType := storage.DocumentType(req.Type)
	switch docType {
	case "":
		docType = storage.DocumentTypeInvoice
	case storage.DocumentTypeInvoice, storage.DocumentTypeOffer, storage.DocumentTypeCorrection:
	default:
		p.add("type", "must be invoice, correction or offer")
	}
	draft, lines := req.draft(docType, p)
	if len(lines) == 0 {
		p.add("lines", "at least one line is required")
	}
	if err := p.err(); err != nil {
		return err
	}

	if req.VATRegime == nil {
		err = a.documentsInTenant(r.Context(), c, func(ctx context.Context) error {
			var err error
			draft.VATRegime, err = a.defaultRegime(ctx)

			return err
		})
		if err != nil {
			return err
		}
		if draft.VATRegime == "" {
			p.add("vat_regime", "required: no VAT regime period and no profile default exist")
		}
	}

	res := checkMoney(p, draft, lines)
	if err := p.err(); err != nil {
		return err
	}
	if res == nil {
		return api.Internal(errors.New("httpapi: preview without a result"))
	}

	out := make([]previewLineJSON, len(res.Lines))
	for i, l := range res.Lines {
		out[i] = previewLineJSON{
			NetAfterLineDiscountCents:  int64(l.NetAfterLineDiscountCents),
			DocumentDiscountShareCents: int64(l.DocumentDiscountShareCents),
			NetAfterAllDiscountsCents:  int64(l.NetAfterAllDiscountsCents),
		}
	}

	return api.Write(w, http.StatusOK, map[string]any{
		"totals": docpayload.TotalsFromMoney(*res),
		"lines":  out,
	})
}

func (a *app) handleGetDocument(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.documentsCaller(w, r, false)
	if !ok {
		return err
	}
	id, err := documentID(r)
	if err != nil {
		return err
	}

	var out documentJSON
	err = a.documentsInTenant(r.Context(), c, func(ctx context.Context) error {
		d, err := a.repos.Documents.FindByID(ctx, id)
		if err != nil {
			return err
		}
		status, err := a.documentChainStatus(ctx, d)
		if err != nil {
			return err
		}
		out = toDocumentJSON(d, true, status)

		return nil
	})
	if err != nil {
		return err
	}

	return api.Write(w, http.StatusOK, out)
}

// handleUpdateDocument answers PUT /api/v1/documents/{id}: it replaces
// a draft's header and all its lines. The type, the chain fields and
// source_offer_id never change. An issued document answers 409.
func (a *app) handleUpdateDocument(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.documentsCaller(w, r, false)
	if !ok {
		return err
	}
	id, err := documentID(r)
	if err != nil {
		return err
	}

	var req documentRequest
	if err := api.Decode(r, &req); err != nil {
		return err
	}

	var updated storage.Document
	err = a.documentsInTenant(r.Context(), c, func(ctx context.Context) error {
		existing, err := a.repos.Documents.FindByID(ctx, id)
		if err != nil {
			return err
		}
		if existing.State != storage.DocumentStateDraft {
			return api.Conflict("the document is issued and immutable; create a correction instead")
		}

		p := documentProblems{}
		if req.Type != "" && storage.DocumentType(req.Type) != existing.Type {
			p.add("type", "the type of a draft never changes")
		}
		if req.CustomerID == nil {
			p.add("customer_id", "required")
		}
		draft, lines := req.draft(existing.Type, p)
		if err := p.err(); err != nil {
			return err
		}
		draft.ID = existing.ID
		draft.TenantID = c.tenantID
		draft.CustomerID = *req.CustomerID
		draft.CorrectsDocumentID = existing.CorrectsDocumentID
		draft.SourceOfferID = existing.SourceOfferID
		draft.CorrectionBaseID = existing.CorrectionBaseID
		draft.CorrectionBaseSeq = existing.CorrectionBaseSeq

		if err := a.resolveDraft(ctx, &draft, lines, req.VATRegime != nil, p); err != nil {
			return err
		}
		checkMoney(p, draft, lines)
		if err := p.err(); err != nil {
			return err
		}

		updated, err = a.repos.Documents.Update(ctx, draft, lines)

		return err
	})
	if err != nil {
		return err
	}

	return api.Write(w, http.StatusOK, toDocumentJSON(updated, true, ""))
}

func (a *app) handleDeleteDocument(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.documentsCaller(w, r, false)
	if !ok {
		return err
	}
	id, err := documentID(r)
	if err != nil {
		return err
	}

	err = a.documentsInTenant(r.Context(), c, func(ctx context.Context) error {
		existing, err := a.repos.Documents.FindByID(ctx, id)
		if err != nil {
			return err
		}
		if existing.State != storage.DocumentStateDraft {
			return api.Conflict("the document is issued and immutable")
		}

		return a.repos.Documents.DeleteDraft(ctx, id)
	})
	if err != nil {
		return err
	}

	api.NoContent(w)

	return nil
}

// issuer is the docissue.Issuer over the wired repositories. The
// renderer of issue #74 adds its deeper printable check when it
// implements docissue.PayloadChecker.
func (a *app) issuer() docissue.Issuer {
	i := docissue.Issuer{
		Documents:          a.repos.Documents,
		DocumentNumbers:    a.repos.DocumentNumbers,
		VATRegimePeriods:   a.repos.VATRegimePeriods,
		Customers:          a.repos.Customers,
		SellerProfileLogos: a.repos.SellerProfileLogos,
		Files:              a.repos.Files,
	}
	if checker, ok := a.repos.Renderer.(docissue.PayloadChecker); ok {
		i.Checker = checker
	}

	return i
}

// handleIssueDocument answers POST /api/v1/documents/{id}/issue, owner
// only. The body is empty: the issue date is today in Germany and the
// number comes from the counter, never from the caller. Everything runs
// in one transaction; see docissue.Issuer.Issue.
func (a *app) handleIssueDocument(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.documentsCaller(w, r, true)
	if !ok {
		return err
	}
	id, err := documentID(r)
	if err != nil {
		return err
	}
	if err := decodeEmptyBody(r); err != nil {
		return err
	}

	issuer := a.issuer()
	now := time.Now()

	var out documentJSON
	err = a.documentsInTenant(r.Context(), c, func(ctx context.Context) error {
		issued, err := issuer.Issue(ctx, id, now)
		if err != nil {
			return err
		}
		status, err := a.documentChainStatus(ctx, issued)
		if err != nil {
			return err
		}
		out = toDocumentJSON(issued, true, status)

		return nil
	})
	if err != nil {
		return err
	}

	return api.Write(w, http.StatusOK, out)
}

// copyDraft is a new draft of docType with the header and the lines of
// from, for a correction or an invoice from an offer.
func copyDraft(from storage.Document, docType storage.DocumentType) (storage.Document, []storage.DocumentLine) {
	d := storage.Document{
		TenantID:                 from.TenantID,
		Type:                     docType,
		State:                    storage.DocumentStateDraft,
		CustomerID:               from.CustomerID,
		VATRegime:                from.VATRegime,
		Language:                 from.Language,
		ServiceDate:              from.ServiceDate,
		ServiceDateTo:            from.ServiceDateTo,
		DueDate:                  from.DueDate,
		DiscountPercentPermyriad: from.DiscountPercentPermyriad,
		DiscountAmountCents:      from.DiscountAmountCents,
		PaymentTerms:             from.PaymentTerms,
		Notes:                    from.Notes,
		CustomerReference:        from.CustomerReference,
	}
	lines := make([]storage.DocumentLine, len(from.Lines))
	for i, l := range from.Lines {
		lines[i] = storage.DocumentLine{
			Position:                     l.Position,
			CatalogueItemID:              l.CatalogueItemID,
			Description:                  l.Description,
			QuantityThousandths:          l.QuantityThousandths,
			Unit:                         l.Unit,
			UnitPriceCents:               l.UnitPriceCents,
			VATRatePermyriad:             l.VATRatePermyriad,
			LineDiscountPercentPermyriad: l.LineDiscountPercentPermyriad,
			LineDiscountAmountCents:      l.LineDiscountAmountCents,
		}
	}

	return d, lines
}

// handleCreateCorrection answers POST /api/v1/documents/{id}/correction,
// owner only. {id} is any issued invoice or correction of a chain. The
// new draft copies the lines of the chain's effective document,
// corrects_document_id names the chain's root, and the base records the
// effective document copied. The original is never changed.
func (a *app) handleCreateCorrection(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.documentsCaller(w, r, true)
	if !ok {
		return err
	}
	id, err := documentID(r)
	if err != nil {
		return err
	}
	if err := decodeEmptyBody(r); err != nil {
		return err
	}

	var created storage.Document
	err = a.documentsInTenant(r.Context(), c, func(ctx context.Context) error {
		from, err := a.repos.Documents.FindByID(ctx, id)
		if err != nil {
			return err
		}
		root, ok := docissue.RootOf(from)
		if !ok || from.State != storage.DocumentStateIssued {
			return api.Conflict("only an issued invoice or correction can be corrected")
		}

		members, err := a.repos.Documents.Chain(ctx, root)
		if err != nil {
			return err
		}
		chain, err := docissue.ChainOf(members)
		if err != nil {
			return err
		}
		base := chain.BaseOf()
		effective, err := a.repos.Documents.FindByID(ctx, base.DocumentID)
		if err != nil {
			return err
		}

		draft, lines := copyDraft(effective, storage.DocumentTypeCorrection)
		draft.TenantID = c.tenantID
		draft.CorrectsDocumentID = &root
		baseID := base.DocumentID
		baseSeq := int32(base.Seq) //nolint:gosec // at most numbering.MaxCorrectionSeq
		draft.CorrectionBaseID = &baseID
		draft.CorrectionBaseSeq = &baseSeq

		created, err = a.repos.Documents.Create(ctx, draft, lines)

		return err
	})
	if err != nil {
		return err
	}

	return api.Write(w, http.StatusCreated, toDocumentJSON(created, true, ""))
}

// handleOfferToInvoice answers POST /api/v1/offers/{id}/to-invoice. {id}
// is an offer, draft or issued. The new invoice draft copies its lines
// and header and names it in source_offer_id. The validity date is the
// offer's own and is not copied. The regime is the offer's: issuing the
// invoice checks it against the period of its own issue date.
func (a *app) handleOfferToInvoice(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.documentsCaller(w, r, false)
	if !ok {
		return err
	}
	id, err := documentID(r)
	if err != nil {
		return err
	}
	if err := decodeEmptyBody(r); err != nil {
		return err
	}

	var created storage.Document
	err = a.documentsInTenant(r.Context(), c, func(ctx context.Context) error {
		offer, err := a.repos.Documents.FindByID(ctx, id)
		if err != nil {
			return err
		}
		if offer.Type != storage.DocumentTypeOffer {
			return api.Conflict("only an offer becomes an invoice")
		}

		draft, lines := copyDraft(offer, storage.DocumentTypeInvoice)
		draft.TenantID = c.tenantID
		offerID := offer.ID
		draft.SourceOfferID = &offerID

		created, err = a.repos.Documents.Create(ctx, draft, lines)

		return err
	})
	if err != nil {
		return err
	}

	return api.Write(w, http.StatusCreated, toDocumentJSON(created, true, ""))
}
