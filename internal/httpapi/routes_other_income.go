package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/api"

	"github.com/ostendorf-cloud/solobill/internal/category"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// registerOtherIncome mounts the other income API. Owned by issue #76
// (I10a). See docs/api.md, "Other income".
//
// The rules match the expenses API (routes_expenses.go): any role keeps
// drafts, the owner books and reverses, a booked entry is immutable, and
// received_on is the Zufluss date the EÜR year comes from.
func registerOtherIncome(mux *http.ServeMux, route routeFunc, a *app) {
	mux.Handle("GET /api/v1/other-income", route(api.Handle(a.handleListOtherIncome)))
	mux.Handle("POST /api/v1/other-income", route(api.Handle(a.handleCreateOtherIncome)))
	mux.Handle("GET /api/v1/other-income/{id}", route(api.Handle(a.handleGetOtherIncome)))
	mux.Handle("PUT /api/v1/other-income/{id}", route(api.Handle(a.handleUpdateOtherIncome)))
	mux.Handle("DELETE /api/v1/other-income/{id}", route(api.Handle(a.handleDeleteOtherIncome)))
	mux.Handle("POST /api/v1/other-income/{id}/book", route(api.Handle(a.handleBookOtherIncome)))
	mux.Handle("POST /api/v1/other-income/{id}/storno", route(api.Handle(a.handleStornoOtherIncome)))
}

// otherIncomeJSON is the other income JSON of docs/api.md: the expense
// JSON with received_on instead of paid_on and without supplier,
// receipt_date and is_asset.
type otherIncomeJSON struct {
	ID               uuid.UUID  `json:"id"`
	State            string     `json:"state"`
	ReceivedOn       *string    `json:"received_on"`
	Description      string     `json:"description"`
	GrossCents       int64      `json:"gross_cents"`
	NetCents         int64      `json:"net_cents"`
	VATRatePermyriad int32      `json:"vat_rate_permyriad"`
	VATCents         int64      `json:"vat_cents"`
	Category         string     `json:"category"`
	FileID           *uuid.UUID `json:"file_id"`
	StornoOfID       *uuid.UUID `json:"storno_of_id"`
	Imported         bool       `json:"imported"`
	BookedAt         *time.Time `json:"booked_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func toOtherIncomeJSON(o storage.OtherIncome) otherIncomeJSON {
	return otherIncomeJSON{
		ID:               o.ID,
		State:            string(o.State),
		ReceivedOn:       bookingDateString(o.ReceivedOn),
		Description:      o.Description,
		GrossCents:       o.GrossCents,
		NetCents:         o.NetCents,
		VATRatePermyriad: o.VATRatePermyriad,
		VATCents:         o.VATCents,
		Category:         string(o.Category),
		FileID:           o.FileID,
		StornoOfID:       o.StornoOfID,
		Imported:         o.Imported,
		BookedAt:         masterDataUTC(o.BookedAt),
		CreatedAt:        o.CreatedAt.UTC(),
		UpdatedAt:        o.UpdatedAt.UTC(),
	}
}

// otherIncomeRequest is the body of POST and PUT.
type otherIncomeRequest struct {
	ReceivedOn       *string `json:"received_on"`
	Description      string  `json:"description"`
	GrossCents       int64   `json:"gross_cents"`
	NetCents         int64   `json:"net_cents"`
	VATRatePermyriad int32   `json:"vat_rate_permyriad"`
	VATCents         int64   `json:"vat_cents"`
	Category         string  `json:"category"`
	FileID           *string `json:"file_id"`
}

func (r otherIncomeRequest) otherIncome() (storage.OtherIncome, error) {
	details := map[string]any{}

	o := storage.OtherIncome{
		ReceivedOn:       parseBookingDate("received_on", r.ReceivedOn, details),
		Description:      strings.TrimSpace(r.Description),
		GrossCents:       r.GrossCents,
		NetCents:         r.NetCents,
		VATRatePermyriad: r.VATRatePermyriad,
		VATCents:         r.VATCents,
		Category:         category.Key(strings.TrimSpace(r.Category)),
		FileID:           parseBookingFileID(r.FileID, details),
	}

	checkBookingText("description", o.Description, maxBookingDescriptionRunes, details)
	checkBookingCategory(o.Category, category.KindIncome, details)
	otherIncomeAmounts(o).checkStatic(details)

	if len(details) > 0 {
		return storage.OtherIncome{}, masterDataInvalid(details)
	}

	return o, nil
}

func otherIncomeAmounts(o storage.OtherIncome) bookingAmounts {
	return bookingAmountsOf(o.GrossCents, o.NetCents, o.VATRatePermyriad, o.VATCents)
}

func (a *app) otherIncomeReady() bool {
	return a.repos.OtherIncome != nil && a.repos.Scope != nil && a.repos.VATRegimePeriods != nil
}

// findOtherIncome is findExpense for other income.
func (a *app) findOtherIncome(ctx context.Context, c caller, id uuid.UUID) (storage.OtherIncome, error) {
	o, err := a.repos.OtherIncome.FindByID(ctx, id)
	if err != nil {
		return storage.OtherIncome{}, err
	}
	if o.TenantID != c.tenantID {
		return storage.OtherIncome{}, storage.ErrNotFound
	}

	return o, nil
}

func (a *app) handleListOtherIncome(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if !a.otherIncomeReady() {
		return a.notImplemented(w, r)
	}

	f, err := parseBookingFilter(r.URL.Query(), category.KindIncome)
	if err != nil {
		return err
	}

	var list []storage.OtherIncome
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		var err error
		list, err = a.repos.OtherIncome.List(ctx, storage.OtherIncomeFilter{Year: f.year, State: f.state, Category: f.category})

		return err
	})
	if err != nil {
		return bookingError(err, "")
	}

	out := make([]otherIncomeJSON, 0, len(list))
	var gross int64
	for _, o := range list {
		if o.TenantID != c.tenantID {
			continue
		}
		out = append(out, toOtherIncomeJSON(o))
		gross += o.GrossCents
	}

	return api.Write(w, http.StatusOK, map[string]any{
		"other_income":      out,
		"total_gross_cents": gross,
	})
}

func (a *app) handleCreateOtherIncome(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if !a.otherIncomeReady() {
		return a.notImplemented(w, r)
	}

	var req otherIncomeRequest
	if err := api.Decode(r, &req); err != nil {
		return err
	}
	o, err := req.otherIncome()
	if err != nil {
		return err
	}
	// The tenant comes from the session, never from the body.
	o.TenantID = c.tenantID

	var created storage.OtherIncome
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		if err := a.checkBookingInTenant(ctx, c, o.ReceivedOn, otherIncomeAmounts(o), o.FileID); err != nil {
			return err
		}
		var err error
		created, err = a.repos.OtherIncome.Create(ctx, o)

		return err
	})
	if err != nil {
		return bookingError(err, "the entry conflicts with an existing row")
	}

	return api.Write(w, http.StatusCreated, toOtherIncomeJSON(created))
}

func (a *app) handleGetOtherIncome(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if !a.otherIncomeReady() {
		return a.notImplemented(w, r)
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	var found storage.OtherIncome
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		var err error
		found, err = a.findOtherIncome(ctx, c, id)

		return err
	})
	if err != nil {
		return bookingError(err, "")
	}

	return api.Write(w, http.StatusOK, toOtherIncomeJSON(found))
}

func (a *app) handleUpdateOtherIncome(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if !a.otherIncomeReady() {
		return a.notImplemented(w, r)
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	var req otherIncomeRequest
	if err := api.Decode(r, &req); err != nil {
		return err
	}
	o, err := req.otherIncome()
	if err != nil {
		return err
	}
	o.ID = id
	o.TenantID = c.tenantID

	var updated storage.OtherIncome
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		existing, err := a.findOtherIncome(ctx, c, id)
		if err != nil {
			return err
		}
		if existing.State != storage.BookingStateDraft {
			return api.Conflict("the entry is booked; book a storno instead")
		}
		if err := a.checkBookingInTenant(ctx, c, o.ReceivedOn, otherIncomeAmounts(o), o.FileID); err != nil {
			return err
		}
		updated, err = a.repos.OtherIncome.UpdateDraft(ctx, o)

		return err
	})
	if err != nil {
		return bookingError(err, "the entry is booked; book a storno instead")
	}

	return api.Write(w, http.StatusOK, toOtherIncomeJSON(updated))
}

func (a *app) handleDeleteOtherIncome(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if !a.otherIncomeReady() {
		return a.notImplemented(w, r)
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		existing, err := a.findOtherIncome(ctx, c, id)
		if err != nil {
			return err
		}
		if existing.State != storage.BookingStateDraft {
			return api.Conflict("the entry is booked; book a storno instead")
		}

		return a.repos.OtherIncome.DeleteDraft(ctx, id)
	})
	if err != nil {
		return bookingError(err, "the entry is booked; book a storno instead")
	}

	w.WriteHeader(http.StatusNoContent)

	return nil
}

// handleBookOtherIncome books a draft, owner only, after the same full
// check as handleBookExpense.
func (a *app) handleBookOtherIncome(w http.ResponseWriter, r *http.Request) error {
	c, err := a.profileOwner(r)
	if err != nil {
		return err
	}
	if !a.otherIncomeReady() {
		return a.notImplemented(w, r)
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	var booked storage.OtherIncome
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		draft, err := a.findOtherIncome(ctx, c, id)
		if err != nil {
			return err
		}
		if draft.State != storage.BookingStateDraft {
			return api.Conflict("the entry is booked already")
		}

		details := map[string]any{}
		if draft.ReceivedOn == nil {
			details["received_on"] = "required for booking"
		}
		if draft.Category == "" {
			details["category"] = "required for booking"
		} else {
			checkBookingCategory(draft.Category, category.KindIncome, details)
		}
		otherIncomeAmounts(draft).checkStatic(details)
		if len(details) > 0 {
			return masterDataInvalid(details)
		}
		if err := a.checkBookingInTenant(ctx, c, draft.ReceivedOn, otherIncomeAmounts(draft), draft.FileID); err != nil {
			return err
		}

		booked, err = a.repos.OtherIncome.Book(ctx, id, time.Now().UTC())
		if err != nil {
			return err
		}
		if !sameOtherIncomeContent(draft, booked) {
			return api.Conflict("the entry changed while it was booked, try again")
		}

		return nil
	})
	if err != nil {
		return bookingError(err, "the entry is booked already")
	}

	return api.Write(w, http.StatusOK, toOtherIncomeJSON(booked))
}

// otherIncomeStornoRequest is the body of POST
// /api/v1/other-income/{id}/storno.
type otherIncomeStornoRequest struct {
	ReceivedOn string `json:"received_on"`
}

func (a *app) handleStornoOtherIncome(w http.ResponseWriter, r *http.Request) error {
	c, err := a.profileOwner(r)
	if err != nil {
		return err
	}
	if !a.otherIncomeReady() {
		return a.notImplemented(w, r)
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	var req otherIncomeStornoRequest
	if err := api.Decode(r, &req); err != nil {
		return err
	}
	details := map[string]any{}
	receivedOn := parseBookingDate("received_on", &req.ReceivedOn, details)
	if receivedOn == nil && len(details) == 0 {
		details["received_on"] = "required"
	}
	if len(details) > 0 {
		return masterDataInvalid(details)
	}

	var storno storage.OtherIncome
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		original, err := a.findOtherIncome(ctx, c, id)
		if err != nil {
			return err
		}
		if original.State != storage.BookingStateBooked || original.StornoOfID != nil {
			return api.Conflict("only a booked entry that is not a storno can be reversed")
		}
		if original.ReceivedOn != nil && receivedOn.Before(*original.ReceivedOn) {
			return masterDataInvalid(map[string]any{"received_on": "must not be before the received_on of the original"})
		}
		if _, err := a.regimeOn(ctx, *receivedOn); err != nil {
			return err
		}

		storno, err = a.repos.OtherIncome.Storno(ctx, id, *receivedOn, time.Now().UTC())

		return err
	})
	if err != nil {
		return bookingError(err, "the entry is a draft, a storno or reversed already")
	}

	return api.Write(w, http.StatusCreated, toOtherIncomeJSON(storno))
}

// sameOtherIncomeContent compares what a booking freezes.
func sameOtherIncomeContent(a, b storage.OtherIncome) bool {
	return sameBookingDate(a.ReceivedOn, b.ReceivedOn) &&
		a.Description == b.Description &&
		otherIncomeAmounts(a) == otherIncomeAmounts(b) &&
		a.Category == b.Category &&
		sameBookingFile(a.FileID, b.FileID)
}
