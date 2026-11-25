package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/api"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/euer"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// registerReports mounts the EÜR report and the year assignments of
// entries near the turn of the year. Owned by issue #80 (I12). See
// docs/api.md, "EÜR report".
//
// Every figure is computed by internal/euer from the owner's own entries
// on every read. Nothing of the report is stored, and nothing in it is
// tax advice: the response says so in its disclaimer.
//
// Decisions this file makes where docs/api.md leaves room:
//   - A year before 2026 answers 404: the years 2023 to 2025 are imported
//     archive years, and solobill computes no year before 2026. A year
//     without an embedded mapping (2027 today) answers 404 as well. A
//     {year} that is no number names no report: 404.
//   - A line also carries label_en, operator ("-" or "+" where the form
//     prints one, else empty), verified (false when its Zeile or
//     Kennzahl was not checked against the form) and each source's date.
//     A warning carries related_ids (internal/euer.Warning.RelatedIDs).
//   - The assignment of a payment that is a reversal, of an asset expense
//     or of a draft is refused with 400: a reversal follows the payment it
//     reverses, and neither of the others is in the EÜR.
//   - created_by is the caller's account. The assignment JSON does not
//     show it.
func registerReports(mux *http.ServeMux, route routeFunc, a *app) {
	mux.Handle("GET /api/v1/reports/euer/{year}", route(api.Handle(a.handleEuerReport)))
	mux.Handle("GET /api/v1/reports/euer/{year}/csv", route(api.Handle(a.handleEuerReportCSV)))
	mux.Handle("GET /api/v1/euer-assignments", route(api.Handle(a.handleListEuerAssignments)))
	mux.Handle("POST /api/v1/euer-assignments", route(api.Handle(a.handleCreateEuerAssignment)))
}

// euerDisclaimer is the disclaimer of every report response.
const euerDisclaimer = "Computed from your entries. No tax advice."

const (
	maxAssignmentNoteRunes = 1000
	minAssignmentTaxYear   = 2000
	maxAssignmentTaxYear   = 9999
)

// reportsWired reports whether every repository the report reads is
// wired. Until then the report routes answer 501, as their stubs did.
func (a *app) reportsWired() bool {
	r := a.repos
	return r.Scope != nil && r.VATRegimePeriods != nil && r.Documents != nil && r.Payments != nil &&
		r.Expenses != nil && r.OtherIncome != nil && r.Assets != nil && r.EuerAssignments != nil
}

// assignmentsWired reports whether every repository the assignment
// routes need is wired.
func (a *app) assignmentsWired() bool {
	r := a.repos
	return r.Scope != nil && r.EuerAssignments != nil && r.Payments != nil && r.Expenses != nil && r.OtherIncome != nil
}

// reportCaller is requireSession plus a wiring check. owner adds the
// owner role.
func (a *app) reportCaller(w http.ResponseWriter, r *http.Request, wired func() bool, owner bool) (caller, bool, error) {
	c, err := a.requireSession(r)
	if err != nil {
		return caller{}, false, err
	}
	if !wired() {
		return caller{}, false, a.notImplemented(w, r)
	}
	if owner && c.role != storage.AccountRoleOwner {
		return caller{}, false, api.PermissionDenied("owner role required")
	}

	return c, true, nil
}

// reportYear parses {year}. A value that is no four digit year names no
// report.
func reportYear(r *http.Request) (int, error) {
	s := r.PathValue("year")
	year, err := strconv.Atoi(s)
	if err != nil || len(s) != 4 {
		return 0, api.NotFound("not found")
	}

	return year, nil
}

// euerError maps an error of the report to the API error.
func euerError(err error) error {
	var apiErr *api.Error
	var missing *euer.RegimeMissingError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &apiErr):
		return apiErr
	case errors.Is(err, euer.ErrArchiveYear):
		return api.NotFound("no EÜR report before tax year 2026: the years 2023 to 2025 are imported archive years")
	case errors.Is(err, euer.ErrNoMapping):
		return api.NotFound("no EÜR line mapping for this tax year yet")
	case errors.As(err, &missing):
		return api.New(http.StatusBadRequest, codeVATRegimeMissing,
			"no VAT regime period covers "+missing.Date.Format(docpayload.DateLayout))
	default:
		return api.Internal(fmt.Errorf("httpapi: euer report: %w", err))
	}
}

// loadEuerInput reads everything the report of year needs, inside the
// tenant scope. Every row of another tenant is dropped: row level
// security already hides it, the handler does not rely on it alone.
func (a *app) loadEuerInput(ctx context.Context, c caller, year int) (euer.Input, error) {
	in := euer.Input{Year: year}

	periods, err := a.repos.VATRegimePeriods.List(ctx)
	if err != nil {
		return euer.Input{}, err
	}
	for _, p := range periods {
		if p.TenantID == c.tenantID {
			in.Periods = append(in.Periods, p)
		}
	}

	from, to := euer.LoadRange(year)
	payments, err := a.repos.Payments.ListReceivedBetween(ctx, from, to)
	if err != nil {
		return euer.Input{}, err
	}
	seen := map[uuid.UUID]bool{}
	for _, p := range payments {
		if p.TenantID != c.tenantID || seen[p.DocumentID] {
			continue
		}
		seen[p.DocumentID] = true
		chain, err := a.loadEuerChain(ctx, c, p.DocumentID)
		if err != nil {
			return euer.Input{}, err
		}
		in.Chains = append(in.Chains, chain)
	}

	booked := storage.BookingStateBooked
	expenses, err := a.repos.Expenses.List(ctx, storage.ExpenseFilter{State: &booked})
	if err != nil {
		return euer.Input{}, err
	}
	for _, e := range expenses {
		if e.TenantID == c.tenantID {
			in.Expenses = append(in.Expenses, e)
		}
	}

	income, err := a.repos.OtherIncome.List(ctx, storage.OtherIncomeFilter{State: &booked})
	if err != nil {
		return euer.Input{}, err
	}
	for _, o := range income {
		if o.TenantID == c.tenantID {
			in.OtherIncome = append(in.OtherIncome, o)
		}
	}

	assets, err := a.repos.Assets.List(ctx)
	if err != nil {
		return euer.Input{}, err
	}
	for _, as := range assets {
		if as.TenantID == c.tenantID {
			in.Assets = append(in.Assets, as)
		}
	}

	assignments, err := a.repos.EuerAssignments.ListEffective(ctx)
	if err != nil {
		return euer.Input{}, err
	}
	for _, as := range assignments {
		if as.TenantID == c.tenantID {
			in.Assignments = append(in.Assignments, as)
		}
	}

	return in, nil
}

// loadEuerChain reads one chain with its whole payment history: the VAT
// share of a payment depends on every payment recorded before it
// (internal/paymentvat).
func (a *app) loadEuerChain(ctx context.Context, c caller, rootID uuid.UUID) (euer.Chain, error) {
	docs, err := a.repos.Documents.Chain(ctx, rootID)
	if err != nil {
		return euer.Chain{}, err
	}
	own := make([]storage.Document, 0, len(docs))
	for _, d := range docs {
		if d.TenantID == c.tenantID {
			own = append(own, d)
		}
	}
	list, err := a.repos.Payments.ListByChain(ctx, rootID)
	if err != nil {
		return euer.Chain{}, err
	}
	payments := make([]storage.Payment, 0, len(list))
	for _, p := range list {
		if p.TenantID == c.tenantID && p.DocumentID == rootID {
			payments = append(payments, p)
		}
	}

	return euer.ChainOf(own, payments)
}

// euerReport computes the report of the request's year.
func (a *app) euerReport(w http.ResponseWriter, r *http.Request) (euer.Report, bool, error) {
	c, ok, err := a.reportCaller(w, r, a.reportsWired, false)
	if !ok {
		return euer.Report{}, false, err
	}
	year, err := reportYear(r)
	if err != nil {
		return euer.Report{}, false, err
	}
	// Refuse an archive or unmapped year before reading anything.
	if year < euer.FirstReportYear {
		return euer.Report{}, false, euerError(euer.ErrArchiveYear)
	}
	if _, ok := euer.MappingFor(year); !ok {
		return euer.Report{}, false, euerError(euer.ErrNoMapping)
	}

	var report euer.Report
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		in, err := a.loadEuerInput(ctx, c, year)
		if err != nil {
			return err
		}
		report, err = euer.Compute(in)

		return err
	})
	if err != nil {
		return euer.Report{}, false, euerError(err)
	}

	return report, true, nil
}

type euerSourceJSON struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	Date        string `json:"date"`
	AmountCents int64  `json:"amount_cents"`
}

type euerLineJSON struct {
	Key         string           `json:"key"`
	Zeile       int              `json:"zeile"`
	Kennzahl    string           `json:"kennzahl"`
	Operator    string           `json:"operator"`
	Verified    bool             `json:"verified"`
	LabelDE     string           `json:"label_de"`
	LabelEN     string           `json:"label_en"`
	AmountCents int64            `json:"amount_cents"`
	Sources     []euerSourceJSON `json:"sources"`
}

type euerBoundaryJSON struct {
	Kind            string `json:"kind"`
	ID              string `json:"id"`
	Date            string `json:"date"`
	AmountCents     int64  `json:"amount_cents"`
	AssignedTaxYear *int   `json:"assigned_tax_year"`
}

type euerWarningJSON struct {
	Code       string   `json:"code"`
	Kind       string   `json:"kind"`
	ID         string   `json:"id"`
	RelatedIDs []string `json:"related_ids"`
}

type euerMappingJSON struct {
	TaxYear int    `json:"tax_year"`
	Status  string `json:"status"`
	Source  string `json:"source"`
}

type euerRegimeJSON struct {
	ValidFrom string `json:"valid_from"`
	Regime    string `json:"regime"`
}

type euerReportJSON struct {
	Year            int                `json:"year"`
	Mapping         euerMappingJSON    `json:"mapping"`
	VATRegimes      []euerRegimeJSON   `json:"vat_regimes"`
	Lines           []euerLineJSON     `json:"lines"`
	BoundaryEntries []euerBoundaryJSON `json:"boundary_entries"`
	Warnings        []euerWarningJSON  `json:"warnings"`
	Disclaimer      string             `json:"disclaimer"`
}

func toEuerReportJSON(rep euer.Report) euerReportJSON {
	day := func(t time.Time) string { return t.Format(docpayload.DateLayout) }
	out := euerReportJSON{
		Year:            rep.Year,
		Mapping:         euerMappingJSON{TaxYear: rep.Mapping.TaxYear, Status: string(rep.Mapping.Status), Source: rep.Mapping.Source},
		VATRegimes:      make([]euerRegimeJSON, 0, len(rep.Regimes)),
		Lines:           make([]euerLineJSON, 0, len(rep.Lines)),
		BoundaryEntries: make([]euerBoundaryJSON, 0, len(rep.Boundary)),
		Warnings:        make([]euerWarningJSON, 0, len(rep.Warnings)),
		Disclaimer:      euerDisclaimer,
	}
	for _, p := range rep.Regimes {
		out.VATRegimes = append(out.VATRegimes, euerRegimeJSON{ValidFrom: day(p.ValidFrom), Regime: string(p.Regime)})
	}
	for _, l := range rep.Lines {
		lj := euerLineJSON{
			Key: string(l.Key), Zeile: l.Zeile, Kennzahl: l.Kennzahl, Operator: l.Operator, Verified: l.Verified,
			LabelDE: l.LabelDE, LabelEN: l.LabelEN, AmountCents: l.AmountCents,
			Sources: make([]euerSourceJSON, 0, len(l.Sources)),
		}
		for _, s := range l.Sources {
			lj.Sources = append(lj.Sources, euerSourceJSON{Kind: string(s.Kind), ID: s.ID.String(), Date: day(s.Date), AmountCents: s.AmountCents})
		}
		out.Lines = append(out.Lines, lj)
	}
	for _, b := range rep.Boundary {
		out.BoundaryEntries = append(out.BoundaryEntries, euerBoundaryJSON{
			Kind: string(b.Kind), ID: b.ID.String(), Date: day(b.Date), AmountCents: b.AmountCents, AssignedTaxYear: b.AssignedTaxYear,
		})
	}
	for _, wn := range rep.Warnings {
		wj := euerWarningJSON{Code: wn.Code, Kind: string(wn.Kind), ID: wn.ID.String(), RelatedIDs: make([]string, 0, len(wn.RelatedIDs))}
		for _, id := range wn.RelatedIDs {
			wj.RelatedIDs = append(wj.RelatedIDs, id.String())
		}
		out.Warnings = append(out.Warnings, wj)
	}

	return out
}

func (a *app) handleEuerReport(w http.ResponseWriter, r *http.Request) error {
	rep, ok, err := a.euerReport(w, r)
	if !ok {
		return err
	}

	return api.Write(w, http.StatusOK, toEuerReportJSON(rep))
}

// handleEuerReportCSV writes the CSV only once it is complete, so a
// failure is an error response, never a truncated 200.
func (a *app) handleEuerReportCSV(w http.ResponseWriter, r *http.Request) error {
	rep, ok, err := a.euerReport(w, r)
	if !ok {
		return err
	}
	var buf bytes.Buffer
	if err := euer.WriteCSV(&buf, rep); err != nil {
		return api.Internal(fmt.Errorf("httpapi: euer csv: %w", err))
	}

	h := w.Header()
	h.Set("Content-Type", "text/csv; charset=utf-8")
	h.Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"euer-%d.csv\"", rep.Year))
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())

	return nil
}

// assignmentJSON is the assignment JSON of docs/api.md.
type assignmentJSON struct {
	ID           string  `json:"id"`
	EntryKind    string  `json:"entry_kind"`
	EntryID      string  `json:"entry_id"`
	TaxYear      int32   `json:"tax_year"`
	Note         *string `json:"note"`
	SupersedesID *string `json:"supersedes_id"`
	CreatedAt    string  `json:"created_at"`
}

func toAssignmentJSON(as storage.EuerAssignment) assignmentJSON {
	out := assignmentJSON{
		ID:        as.ID.String(),
		EntryKind: string(as.EntryKind),
		EntryID:   as.EntryID.String(),
		TaxYear:   as.TaxYear,
		Note:      as.Note,
		CreatedAt: as.CreatedAt.UTC().Format(time.RFC3339),
	}
	if as.SupersedesID != nil {
		s := as.SupersedesID.String()
		out.SupersedesID = &s
	}

	return out
}

func (a *app) handleListEuerAssignments(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.reportCaller(w, r, a.assignmentsWired, false)
	if !ok {
		return err
	}
	var year *int32
	if s := r.URL.Query().Get("tax_year"); s != "" {
		y, err := strconv.Atoi(s)
		if err != nil || y < minAssignmentTaxYear || y > maxAssignmentTaxYear {
			return masterDataInvalid(map[string]any{"tax_year": "must be a year from 2000 to 9999"})
		}
		y32 := int32(y)
		year = &y32
	}

	var list []storage.EuerAssignment
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		var err error
		list, err = a.repos.EuerAssignments.ListEffective(ctx)

		return err
	})
	if err != nil {
		return masterDataError(err, "conflict")
	}

	out := make([]assignmentJSON, 0, len(list))
	for _, as := range list {
		if as.TenantID != c.tenantID || (year != nil && as.TaxYear != *year) {
			continue
		}
		out = append(out, toAssignmentJSON(as))
	}

	return api.Write(w, http.StatusOK, map[string]any{"assignments": out})
}

// assignmentRequest is the body of POST /api/v1/euer-assignments. Every
// field is a pointer so a missing one is told apart from a zero.
// api.Decode refuses unknown fields: the tenant, created_by and the
// timestamps never come from the body.
type assignmentRequest struct {
	EntryKind    *string `json:"entry_kind"`
	EntryID      *string `json:"entry_id"`
	TaxYear      *int    `json:"tax_year"`
	Note         *string `json:"note"`
	SupersedesID *string `json:"supersedes_id"`
}

// assignment checks the body on its own.
func (req assignmentRequest) assignment() (storage.EuerAssignment, error) {
	details := map[string]any{}
	var as storage.EuerAssignment

	switch k := storage.EuerEntryKind(deref(req.EntryKind)); k {
	case storage.EuerEntryPayment, storage.EuerEntryExpense, storage.EuerEntryOtherIncome:
		as.EntryKind = k
	case "":
		details["entry_kind"] = "required"
	default:
		details["entry_kind"] = "must be payment, expense or other_income"
	}

	if req.EntryID == nil || strings.TrimSpace(*req.EntryID) == "" {
		details["entry_id"] = "required"
	} else if id, err := uuid.Parse(strings.TrimSpace(*req.EntryID)); err != nil {
		details["entry_id"] = "must be an id"
	} else {
		as.EntryID = id
	}

	switch {
	case req.TaxYear == nil:
		details["tax_year"] = "required"
	case *req.TaxYear < minAssignmentTaxYear || *req.TaxYear > maxAssignmentTaxYear:
		details["tax_year"] = "must be a year from 2000 to 9999"
	default:
		as.TaxYear = int32(*req.TaxYear)
	}

	if req.Note != nil && *req.Note != "" {
		switch {
		case paymentNoteHasBadRune(*req.Note):
			details["note"] = "must not contain control or formatting characters other than a line break"
		case utf8.RuneCountInString(*req.Note) > maxAssignmentNoteRunes:
			details["note"] = fmt.Sprintf("must be at most %d characters", maxAssignmentNoteRunes)
		default:
			note := *req.Note
			as.Note = &note
		}
	}

	if req.SupersedesID != nil && strings.TrimSpace(*req.SupersedesID) != "" {
		id, err := uuid.Parse(strings.TrimSpace(*req.SupersedesID))
		if err != nil {
			details["supersedes_id"] = "must be an assignment id"
		} else {
			as.SupersedesID = &id
		}
	}

	if len(details) > 0 {
		return storage.EuerAssignment{}, masterDataInvalid(details)
	}

	return as, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return strings.TrimSpace(*s)
}

// errNoAssignableEntry is the 400 for an entry_id that names no entry the
// owner may assign.
func errNoAssignableEntry(reason string) error {
	return masterDataInvalid(map[string]any{"entry_id": reason})
}

// entryDate reads the date of the assigned entry: received_on of a
// payment that is no reversal, paid_on of a booked expense that is no
// asset expense, received_on of booked other income. A payment is looked
// up among the payments that could be assigned to taxYear at all, since
// the payment repository reads by date, not by id.
func (a *app) entryDate(ctx context.Context, c caller, as storage.EuerAssignment) (time.Time, error) {
	switch as.EntryKind {
	case storage.EuerEntryPayment:
		from, to := euer.LoadRange(int(as.TaxYear))
		list, err := a.repos.Payments.ListReceivedBetween(ctx, from, to)
		if err != nil {
			return time.Time{}, err
		}
		for _, p := range list {
			if p.ID != as.EntryID || p.TenantID != c.tenantID {
				continue
			}
			if p.ReversesPaymentID != nil {
				return time.Time{}, errNoAssignableEntry("a reversal follows the payment it reverses")
			}

			return p.ReceivedOn, nil
		}

		return time.Time{}, errNoAssignableEntry("must name a payment received from 22 December to 10 January next to tax_year")
	case storage.EuerEntryExpense:
		e, err := a.repos.Expenses.FindByID(ctx, as.EntryID)
		if errors.Is(err, storage.ErrNotFound) || (err == nil && e.TenantID != c.tenantID) {
			return time.Time{}, errNoAssignableEntry("names no expense")
		}
		if err != nil {
			return time.Time{}, err
		}
		if e.State != storage.BookingStateBooked || e.PaidOn == nil {
			return time.Time{}, errNoAssignableEntry("the expense is not booked")
		}
		if e.IsAsset {
			return time.Time{}, errNoAssignableEntry("an asset expense is not in the EÜR; its depreciation is")
		}

		return *e.PaidOn, nil
	default:
		o, err := a.repos.OtherIncome.FindByID(ctx, as.EntryID)
		if errors.Is(err, storage.ErrNotFound) || (err == nil && o.TenantID != c.tenantID) {
			return time.Time{}, errNoAssignableEntry("names no other income entry")
		}
		if err != nil {
			return time.Time{}, err
		}
		if o.State != storage.BookingStateBooked || o.ReceivedOn == nil {
			return time.Time{}, errNoAssignableEntry("the other income entry is not booked")
		}

		return *o.ReceivedOn, nil
	}
}

// checkAssignmentDate applies the window rule of internal/euer.
func checkAssignmentDate(d time.Time, taxYear int32) error {
	switch err := euer.CheckAssignment(d, int(taxYear)); {
	case errors.Is(err, euer.ErrOutsideWindow):
		return errNoAssignableEntry(fmt.Sprintf("the entry's date %s lies outside 22 December to 10 January",
			d.Format(docpayload.DateLayout)))
	case errors.Is(err, euer.ErrWrongTaxYear):
		other := d.Year() + 1
		if d.Month() == time.January {
			other = d.Year() - 1
		}

		return masterDataInvalid(map[string]any{"tax_year": fmt.Sprintf("must be %d or %d", d.Year(), other)})
	default:
		return err
	}
}

func (a *app) handleCreateEuerAssignment(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.reportCaller(w, r, a.assignmentsWired, true)
	if !ok {
		return err
	}
	var req assignmentRequest
	if err := api.Decode(r, &req); err != nil {
		return err
	}
	as, err := req.assignment()
	if err != nil {
		return err
	}
	// The tenant and the author come from the session, never from the
	// body.
	as.TenantID = c.tenantID
	as.CreatedBy = &c.accountID

	var created storage.EuerAssignment
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		d, err := a.entryDate(ctx, c, as)
		if err != nil {
			return err
		}
		if err := checkAssignmentDate(d, as.TaxYear); err != nil {
			return err
		}
		created, err = a.repos.EuerAssignments.Create(ctx, as)

		return err
	})
	if err != nil {
		var apiErr *api.Error
		if errors.As(err, &apiErr) {
			return apiErr
		}

		return masterDataError(err, "the entry already has an effective assignment: supersedes_id must name it")
	}

	return api.Write(w, http.StatusCreated, toAssignmentJSON(created))
}
