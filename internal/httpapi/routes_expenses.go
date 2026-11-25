package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/api"

	"github.com/ostendorf-cloud/solobill/internal/category"
	"github.com/ostendorf-cloud/solobill/internal/money"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// registerExpenses mounts the expenses API. Owned by issue #76 (I10a).
// See docs/api.md, "Expenses".
//
// A draft is created, changed and deleted by any role. Booking and the
// storno are owner only. A booked expense is immutable: a mistake is fixed
// by a storno row plus a new expense (docs/concepts/solobill.md, "Receipt
// storage and GoBD"). The database trigger refuses the change as well.
func registerExpenses(mux *http.ServeMux, route routeFunc, a *app) {
	mux.Handle("GET /api/v1/expenses", route(api.Handle(a.handleListExpenses)))
	mux.Handle("POST /api/v1/expenses", route(api.Handle(a.handleCreateExpense)))
	mux.Handle("GET /api/v1/expenses/{id}", route(api.Handle(a.handleGetExpense)))
	mux.Handle("PUT /api/v1/expenses/{id}", route(api.Handle(a.handleUpdateExpense)))
	mux.Handle("DELETE /api/v1/expenses/{id}", route(api.Handle(a.handleDeleteExpense)))
	mux.Handle("POST /api/v1/expenses/{id}/book", route(api.Handle(a.handleBookExpense)))
	mux.Handle("POST /api/v1/expenses/{id}/storno", route(api.Handle(a.handleStornoExpense)))
}

// Limits of an expense or other income entry. maxBookingAmountCents is
// 1,000,000,000.00 EUR, matching the amount bound of both tables.
const maxBookingAmountCents int64 = 100_000_000_000

const (
	maxBookingSupplierRunes    = 200
	maxBookingDescriptionRunes = 1000
	bookingEarliestYear        = 2000
	bookingLatestYear          = 9999
)

// bookingEarliestDate is the earliest payment or receipt date accepted. A
// date more than one year after today is refused too: a typo in the year
// would otherwise put an entry into a far away EÜR.
var bookingEarliestDate = time.Date(bookingEarliestYear, 1, 1, 0, 0, 0, 0, time.UTC)

// expenseJSON is the expense JSON of docs/api.md.
type expenseJSON struct {
	ID               uuid.UUID  `json:"id"`
	State            string     `json:"state"`
	PaidOn           *string    `json:"paid_on"`
	ReceiptDate      *string    `json:"receipt_date"`
	Supplier         string     `json:"supplier"`
	Description      string     `json:"description"`
	GrossCents       int64      `json:"gross_cents"`
	NetCents         int64      `json:"net_cents"`
	VATRatePermyriad int32      `json:"vat_rate_permyriad"`
	VATCents         int64      `json:"vat_cents"`
	Category         string     `json:"category"`
	FileID           *uuid.UUID `json:"file_id"`
	// MissingReceipt is derived from state and file_id, never stored
	// and never accepted in a request.
	MissingReceipt bool       `json:"missing_receipt"`
	IsAsset        bool       `json:"is_asset"`
	StornoOfID     *uuid.UUID `json:"storno_of_id"`
	Imported       bool       `json:"imported"`
	BookedAt       *time.Time `json:"booked_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func toExpenseJSON(e storage.Expense) expenseJSON {
	return expenseJSON{
		ID:               e.ID,
		State:            string(e.State),
		PaidOn:           bookingDateString(e.PaidOn),
		ReceiptDate:      bookingDateString(e.ReceiptDate),
		Supplier:         e.Supplier,
		Description:      e.Description,
		GrossCents:       e.GrossCents,
		NetCents:         e.NetCents,
		VATRatePermyriad: e.VATRatePermyriad,
		VATCents:         e.VATCents,
		Category:         string(e.Category),
		FileID:           e.FileID,
		MissingReceipt:   e.MissingReceipt(),
		IsAsset:          e.IsAsset,
		StornoOfID:       e.StornoOfID,
		Imported:         e.Imported,
		BookedAt:         masterDataUTC(e.BookedAt),
		CreatedAt:        e.CreatedAt.UTC(),
		UpdatedAt:        e.UpdatedAt.UTC(),
	}
}

// expenseRequest is the body of POST and PUT: the expense JSON without
// id, state, missing_receipt, storno_of_id, imported, booked_at and the
// timestamps. Those and the tenant are refused as unknown fields by the
// decoder.
type expenseRequest struct {
	PaidOn           *string `json:"paid_on"`
	ReceiptDate      *string `json:"receipt_date"`
	Supplier         string  `json:"supplier"`
	Description      string  `json:"description"`
	GrossCents       int64   `json:"gross_cents"`
	NetCents         int64   `json:"net_cents"`
	VATRatePermyriad int32   `json:"vat_rate_permyriad"`
	VATCents         int64   `json:"vat_cents"`
	Category         string  `json:"category"`
	FileID           *string `json:"file_id"`
	IsAsset          bool    `json:"is_asset"`
}

// expense checks what needs no tenant data and builds the domain value,
// without id and tenant.
func (r expenseRequest) expense() (storage.Expense, error) {
	details := map[string]any{}

	e := storage.Expense{
		PaidOn:           parseBookingDate("paid_on", r.PaidOn, details),
		ReceiptDate:      parseBookingDate("receipt_date", r.ReceiptDate, details),
		Supplier:         strings.TrimSpace(r.Supplier),
		Description:      strings.TrimSpace(r.Description),
		GrossCents:       r.GrossCents,
		NetCents:         r.NetCents,
		VATRatePermyriad: r.VATRatePermyriad,
		VATCents:         r.VATCents,
		Category:         category.Key(strings.TrimSpace(r.Category)),
		FileID:           parseBookingFileID(r.FileID, details),
		IsAsset:          r.IsAsset,
	}

	checkBookingText("supplier", e.Supplier, maxBookingSupplierRunes, details)
	checkBookingText("description", e.Description, maxBookingDescriptionRunes, details)
	checkBookingCategory(e.Category, category.KindExpense, details)
	bookingAmountsOf(e.GrossCents, e.NetCents, e.VATRatePermyriad, e.VATCents).checkStatic(details)

	if len(details) > 0 {
		return storage.Expense{}, masterDataInvalid(details)
	}

	return e, nil
}

// bookingDateString formats an optional date as YYYY-MM-DD.
func bookingDateString(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(vatDateLayout)

	return &s
}

// parseBookingDate parses an optional date. Null and blank mean "not
// set". A malformed or out of range date is a field error under field.
func parseBookingDate(field string, raw *string, details map[string]any) *time.Time {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil
	}
	t, err := time.Parse(vatDateLayout, strings.TrimSpace(*raw))
	if err != nil {
		details[field] = "must be a date such as 2026-11-18"

		return nil
	}
	if t.Before(bookingEarliestDate) || t.After(time.Now().UTC().AddDate(1, 0, 0)) {
		details[field] = "must be between 2000-01-01 and one year from today"

		return nil
	}

	return &t
}

// parseBookingFileID parses an optional receipt file id. Whether the file
// exists in the caller's tenant is checked later, in the tenant scope.
func parseBookingFileID(raw *string, details map[string]any) *uuid.UUID {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil
	}
	id, err := uuid.Parse(strings.TrimSpace(*raw))
	if err != nil {
		details["file_id"] = "must be a file id"

		return nil
	}

	return &id
}

func checkBookingText(field, value string, maxRunes int, details map[string]any) {
	if utf8.RuneCountInString(value) > maxRunes {
		details[field] = fmt.Sprintf("must be at most %d characters", maxRunes)
	}
}

// checkBookingCategory accepts an empty key, which a draft may carry, and
// otherwise only a key of kind. The value set is internal/category's.
func checkBookingCategory(key category.Key, kind category.Kind, details map[string]any) {
	if key == "" {
		return
	}
	if !category.Valid(key, kind) {
		details["category"] = fmt.Sprintf("must be a key of kind %s from GET /api/v1/categories", kind)
	}
}

// bookingAmounts are the four figures of an expense or other income.
type bookingAmounts struct {
	gross, net, vat int64
	rate            int32
}

func bookingAmountsOf(gross, net int64, rate int32, vat int64) bookingAmounts {
	return bookingAmounts{gross: gross, net: net, vat: vat, rate: rate}
}

// checkStatic checks what holds under both regimes: a positive gross
// amount within the bound, no negative part, a rate in [0, 10000], and
// net plus VAT adding up to gross. Negative amounts exist only on a storno
// row, which the server builds itself.
func (b bookingAmounts) checkStatic(details map[string]any) {
	switch {
	case b.gross <= 0:
		details["gross_cents"] = "must be greater than 0"
	case b.gross > maxBookingAmountCents:
		details["gross_cents"] = "too large"
	}
	if b.net < 0 {
		details["net_cents"] = "must not be negative"
	}
	if b.vat < 0 {
		details["vat_cents"] = "must not be negative"
	}
	if b.rate < 0 || b.rate > 10000 {
		details["vat_rate_permyriad"] = "must be between 0 and 10000"
	}
	if len(details) == 0 && b.net+b.vat != b.gross {
		details["vat_cents"] = "net_cents plus vat_cents must equal gross_cents"
	}
}

// checkRegime checks the figures against the VAT regime of the payment
// date. regime is nil while a draft has no date yet; the rate check of
// the standard regime applies then, and booking checks again with the
// regime of the date.
//
// Kleinunternehmer: VAT stays inside gross, so net equals gross and the
// rate and the VAT are 0 (no Vorsteuer deduction).
//
// Standard: the VAT must be what the rate gives, computed either way a
// receipt computes it: from the net amount through internal/money (VAT
// rounded once, half away from zero), or extracted from the gross amount
// (gross times rate over 10000 plus rate, rounded the same way). One
// expense carries one rate; a receipt with two rates is two expenses.
func (b bookingAmounts) checkRegime(regime *storage.VATRegime, details map[string]any) {
	if regime != nil && *regime == storage.VATRegimeKleinunternehmer {
		if b.net != b.gross {
			details["net_cents"] = "must equal gross_cents under the Kleinunternehmer regime"
		}
		if b.vat != 0 {
			details["vat_cents"] = "must be 0 under the Kleinunternehmer regime"
		}
		if b.rate != 0 {
			details["vat_rate_permyriad"] = "must be 0 under the Kleinunternehmer regime"
		}

		return
	}

	fromNet, err := vatFromNet(b.net, b.rate)
	if err != nil {
		details["vat_cents"] = "cannot be computed from net_cents and vat_rate_permyriad"

		return
	}
	if b.vat != fromNet && b.vat != vatFromGross(b.gross, b.rate) {
		details["vat_cents"] = fmt.Sprintf("does not match net_cents at vat_rate_permyriad, expected %d", fromNet)
	}
}

// vatFromNet is the VAT of one line of net cents at rate, through
// internal/money.
func vatFromNet(net int64, rate int32) (int64, error) {
	if net == 0 {
		return 0, nil
	}
	res, err := money.Calculate([]money.Line{{
		QuantityThousandths: 1000,
		UnitPriceCents:      money.Cents(net),
		VATRatePermyriad:    money.Permyriad(rate),
	}}, money.Discount{}, money.VATRegimeStandard)
	if err != nil {
		return 0, fmt.Errorf("httpapi: vat from net: %w", err)
	}

	return int64(res.VATTotalCents), nil
}

// vatFromGross extracts the VAT contained in gross at rate, rounded half
// away from zero, for gross >= 0. The products stay far below the int64
// range for amounts within maxBookingAmountCents.
func vatFromGross(gross int64, rate int32) int64 {
	divisor := 10000 + int64(rate)

	return (2*gross*int64(rate) + divisor) / (2 * divisor)
}

// errVATRegimeMissing is the 400 of docs/api.md when no period covers the
// date an entry counts on.
func errVATRegimeMissing(date time.Time) error {
	return api.New(http.StatusBadRequest, codeVATRegimeMissing,
		"no VAT regime period covers "+date.UTC().Format(vatDateLayout))
}

// regimeOn returns the regime of the effective period on date, or
// vat_regime_missing.
func (a *app) regimeOn(ctx context.Context, date time.Time) (storage.VATRegime, error) {
	period, err := a.repos.VATRegimePeriods.RegimeOn(ctx, date)
	if errors.Is(err, storage.ErrNotFound) {
		return "", errVATRegimeMissing(date)
	}
	if err != nil {
		return "", fmt.Errorf("httpapi: regime on %s: %w", date.Format(vatDateLayout), err)
	}

	return period.Regime, nil
}

// checkBookingInTenant checks what needs the tenant's data, inside its
// scope: the regime of date (vat_regime_missing without one) with the
// figures under it, and the receipt. A file id that is unknown, of
// another tenant or not a receipt is one and the same field error, so a
// foreign id tells nothing about the other tenant.
func (a *app) checkBookingInTenant(ctx context.Context, c caller, date *time.Time, amounts bookingAmounts, fileID *uuid.UUID) error {
	details := map[string]any{}

	var regime *storage.VATRegime
	if date != nil {
		r, err := a.regimeOn(ctx, *date)
		if err != nil {
			return err
		}
		regime = &r
	}
	amounts.checkRegime(regime, details)

	if fileID != nil {
		ok, err := a.isReceiptOfTenant(ctx, c, *fileID)
		if err != nil {
			return err
		}
		if !ok {
			details["file_id"] = "no such receipt"
		}
	}

	if len(details) > 0 {
		return masterDataInvalid(details)
	}

	return nil
}

// isReceiptOfTenant reports whether id is a receipt file of the caller's
// tenant. Row level security already hides another tenant's file; the
// tenant is compared again here, as findFile does.
func (a *app) isReceiptOfTenant(ctx context.Context, c caller, id uuid.UUID) (bool, error) {
	if a.repos.Files == nil {
		return false, nil
	}
	file, err := a.repos.Files.FindByID(ctx, id)
	if errors.Is(err, storage.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("httpapi: find receipt: %w", err)
	}

	return file.TenantID == c.tenantID && file.Purpose == storage.FilePurposeReceipt, nil
}

// bookingError maps an error from a tenant scope: an *api.Error passes
// through, a repository sentinel becomes 404 or 409, anything else 500.
func bookingError(err error, conflict string) error {
	if err == nil {
		return nil
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}

	return masterDataError(err, conflict)
}

// bookingFilter is the parsed query of a list endpoint.
type bookingFilter struct {
	year     *int
	state    *storage.BookingState
	category *category.Key
}

// parseBookingFilter reads year, state and category from the query. A
// value outside its set is a field error, never an empty result.
func parseBookingFilter(q url.Values, kind category.Kind) (bookingFilter, error) {
	details := map[string]any{}
	var f bookingFilter

	if raw := q.Get("year"); raw != "" {
		year, err := strconv.Atoi(raw)
		if err != nil || year < bookingEarliestYear || year > bookingLatestYear {
			details["year"] = "must be a year from 2000"
		} else {
			f.year = &year
		}
	}
	if raw := q.Get("state"); raw != "" {
		state := storage.BookingState(raw)
		if state != storage.BookingStateDraft && state != storage.BookingStateBooked {
			details["state"] = "must be draft or booked"
		} else {
			f.state = &state
		}
	}
	if raw := q.Get("category"); raw != "" {
		key := category.Key(raw)
		if !category.Valid(key, kind) {
			details["category"] = fmt.Sprintf("must be a key of kind %s from GET /api/v1/categories", kind)
		} else {
			f.category = &key
		}
	}

	if len(details) > 0 {
		return bookingFilter{}, masterDataInvalid(details)
	}

	return f, nil
}

// expensesReady answers 501 while a repository the expense routes need is
// not wired.
func (a *app) expensesReady() bool {
	return a.repos.Expenses != nil && a.repos.Scope != nil && a.repos.VATRegimePeriods != nil
}

// findExpense reads the expense in the caller's tenant. Row level
// security already hides another tenant's row; a row of another tenant is
// answered as not found here too.
func (a *app) findExpense(ctx context.Context, c caller, id uuid.UUID) (storage.Expense, error) {
	e, err := a.repos.Expenses.FindByID(ctx, id)
	if err != nil {
		return storage.Expense{}, err
	}
	if e.TenantID != c.tenantID {
		return storage.Expense{}, storage.ErrNotFound
	}

	return e, nil
}

func (a *app) handleListExpenses(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if !a.expensesReady() {
		return a.notImplemented(w, r)
	}

	f, err := parseBookingFilter(r.URL.Query(), category.KindExpense)
	if err != nil {
		return err
	}

	var list []storage.Expense
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		var err error
		list, err = a.repos.Expenses.List(ctx, storage.ExpenseFilter{Year: f.year, State: f.state, Category: f.category})

		return err
	})
	if err != nil {
		return bookingError(err, "")
	}

	out := make([]expenseJSON, 0, len(list))
	var gross, net int64
	for _, e := range list {
		if e.TenantID != c.tenantID {
			continue
		}
		out = append(out, toExpenseJSON(e))
		gross += e.GrossCents
		net += e.NetCents
	}

	return api.Write(w, http.StatusOK, map[string]any{
		"expenses":          out,
		"total_gross_cents": gross,
		"total_net_cents":   net,
	})
}

func (a *app) handleCreateExpense(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if !a.expensesReady() {
		return a.notImplemented(w, r)
	}

	var req expenseRequest
	if err := api.Decode(r, &req); err != nil {
		return err
	}
	e, err := req.expense()
	if err != nil {
		return err
	}
	// The tenant comes from the session, never from the body.
	e.TenantID = c.tenantID

	var created storage.Expense
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		if err := a.checkBookingInTenant(ctx, c, e.PaidOn, expenseAmounts(e), e.FileID); err != nil {
			return err
		}
		var err error
		created, err = a.repos.Expenses.Create(ctx, e)

		return err
	})
	if err != nil {
		return bookingError(err, "the expense conflicts with an existing row")
	}

	return api.Write(w, http.StatusCreated, toExpenseJSON(created))
}

func (a *app) handleGetExpense(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if !a.expensesReady() {
		return a.notImplemented(w, r)
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	var found storage.Expense
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		var err error
		found, err = a.findExpense(ctx, c, id)

		return err
	})
	if err != nil {
		return bookingError(err, "")
	}

	return api.Write(w, http.StatusOK, toExpenseJSON(found))
}

func (a *app) handleUpdateExpense(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if !a.expensesReady() {
		return a.notImplemented(w, r)
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	var req expenseRequest
	if err := api.Decode(r, &req); err != nil {
		return err
	}
	e, err := req.expense()
	if err != nil {
		return err
	}
	e.ID = id
	e.TenantID = c.tenantID

	var updated storage.Expense
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		existing, err := a.findExpense(ctx, c, id)
		if err != nil {
			return err
		}
		if existing.State != storage.BookingStateDraft {
			return api.Conflict("the expense is booked; book a storno instead")
		}
		if err := a.checkBookingInTenant(ctx, c, e.PaidOn, expenseAmounts(e), e.FileID); err != nil {
			return err
		}
		updated, err = a.repos.Expenses.UpdateDraft(ctx, e)

		return err
	})
	if err != nil {
		return bookingError(err, "the expense is booked; book a storno instead")
	}

	return api.Write(w, http.StatusOK, toExpenseJSON(updated))
}

func (a *app) handleDeleteExpense(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if !a.expensesReady() {
		return a.notImplemented(w, r)
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		existing, err := a.findExpense(ctx, c, id)
		if err != nil {
			return err
		}
		if existing.State != storage.BookingStateDraft {
			return api.Conflict("the expense is booked; book a storno instead")
		}

		return a.repos.Expenses.DeleteDraft(ctx, id)
	})
	if err != nil {
		return bookingError(err, "the expense is booked; book a storno instead")
	}

	w.WriteHeader(http.StatusNoContent)

	return nil
}

// handleBookExpense books a draft, owner only. It checks the draft again
// in full against the regime of its payment date, because a period may
// have been added or superseded since the draft was saved. Booking
// without a receipt is allowed. The expense JSON flags it as
// missing_receipt, and the EÜR report warns about it.
func (a *app) handleBookExpense(w http.ResponseWriter, r *http.Request) error {
	c, err := a.profileOwner(r)
	if err != nil {
		return err
	}
	if !a.expensesReady() {
		return a.notImplemented(w, r)
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	var booked storage.Expense
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		draft, err := a.findExpense(ctx, c, id)
		if err != nil {
			return err
		}
		if draft.State != storage.BookingStateDraft {
			return api.Conflict("the expense is booked already")
		}

		details := map[string]any{}
		if draft.PaidOn == nil {
			details["paid_on"] = "required for booking"
		}
		if draft.Category == "" {
			details["category"] = "required for booking"
		} else {
			checkBookingCategory(draft.Category, category.KindExpense, details)
		}
		expenseAmounts(draft).checkStatic(details)
		if len(details) > 0 {
			return masterDataInvalid(details)
		}
		if err := a.checkBookingInTenant(ctx, c, draft.PaidOn, expenseAmounts(draft), draft.FileID); err != nil {
			return err
		}

		booked, err = a.repos.Expenses.Book(ctx, id, time.Now().UTC())
		if err != nil {
			return err
		}
		// The row booked is the row checked: a change that slipped in
		// between rolls the booking back.
		if !sameExpenseContent(draft, booked) {
			return api.Conflict("the expense changed while it was booked, try again")
		}

		return nil
	})
	if err != nil {
		return bookingError(err, "the expense is booked already")
	}

	return api.Write(w, http.StatusOK, toExpenseJSON(booked))
}

// expenseStornoRequest is the body of POST /api/v1/expenses/{id}/storno.
type expenseStornoRequest struct {
	PaidOn string `json:"paid_on"`
}

// handleStornoExpense books the reversing row of a booked expense, owner
// only. paid_on is the date the money flowed back, which decides the EÜR
// year of the storno row.
func (a *app) handleStornoExpense(w http.ResponseWriter, r *http.Request) error {
	c, err := a.profileOwner(r)
	if err != nil {
		return err
	}
	if !a.expensesReady() {
		return a.notImplemented(w, r)
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	var req expenseStornoRequest
	if err := api.Decode(r, &req); err != nil {
		return err
	}
	details := map[string]any{}
	paidOn := parseBookingDate("paid_on", &req.PaidOn, details)
	if paidOn == nil && len(details) == 0 {
		details["paid_on"] = "required"
	}
	if len(details) > 0 {
		return masterDataInvalid(details)
	}

	var storno storage.Expense
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		original, err := a.findExpense(ctx, c, id)
		if err != nil {
			return err
		}
		if original.State != storage.BookingStateBooked || original.StornoOfID != nil {
			return api.Conflict("only a booked expense that is not a storno can be reversed")
		}
		if original.PaidOn != nil && paidOn.Before(*original.PaidOn) {
			return masterDataInvalid(map[string]any{"paid_on": "must not be before the paid_on of the original"})
		}
		if _, err := a.regimeOn(ctx, *paidOn); err != nil {
			return err
		}

		storno, err = a.repos.Expenses.Storno(ctx, id, *paidOn, time.Now().UTC())

		return err
	})
	if err != nil {
		return bookingError(err, "the expense is a draft, a storno or reversed already")
	}

	return api.Write(w, http.StatusCreated, toExpenseJSON(storno))
}

func expenseAmounts(e storage.Expense) bookingAmounts {
	return bookingAmountsOf(e.GrossCents, e.NetCents, e.VATRatePermyriad, e.VATCents)
}

// sameExpenseContent compares what a booking freezes.
func sameExpenseContent(a, b storage.Expense) bool {
	return sameBookingDate(a.PaidOn, b.PaidOn) &&
		sameBookingDate(a.ReceiptDate, b.ReceiptDate) &&
		a.Supplier == b.Supplier &&
		a.Description == b.Description &&
		expenseAmounts(a) == expenseAmounts(b) &&
		a.Category == b.Category &&
		sameBookingFile(a.FileID, b.FileID) &&
		a.IsAsset == b.IsAsset
}

func sameBookingDate(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}

	return a.UTC().Format(vatDateLayout) == b.UTC().Format(vatDateLayout)
}

func sameBookingFile(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}
