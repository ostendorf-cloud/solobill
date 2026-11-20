package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/api"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// registerVATRegimes mounts the VAT regime history. Owned by issue #68
// (I2). See docs/api.md, "VAT regime periods".
func registerVATRegimes(mux *http.ServeMux, route routeFunc, a *app) {
	mux.Handle("GET /api/v1/vat-regimes", route(api.Handle(a.handleListVATRegimes)))
	mux.Handle("POST /api/v1/vat-regimes", route(api.Handle(a.handleCreateVATRegime)))
}

const vatDateLayout = "2006-01-02"

// vatEarliestValidFrom is the earliest valid_from a period may carry. A
// later one than one year from today is refused too: a typo in the year
// would otherwise decide the regime of every later date.
var vatEarliestValidFrom = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// maxVATPeriodNote bounds the owner's free text reason.
const maxVATPeriodNote = 500

// vatRegimeResponse is the period JSON of docs/api.md.
type vatRegimeResponse struct {
	ID           uuid.UUID  `json:"id"`
	ValidFrom    string     `json:"valid_from"`
	Regime       string     `json:"regime"`
	Note         *string    `json:"note"`
	SupersedesID *uuid.UUID `json:"supersedes_id"`
	Superseded   bool       `json:"superseded"`
	CreatedAt    time.Time  `json:"created_at"`
}

func newVATRegimeResponse(p storage.VATRegimePeriod, superseded bool) vatRegimeResponse {
	return vatRegimeResponse{
		ID:           p.ID,
		ValidFrom:    p.ValidFrom.UTC().Format(vatDateLayout),
		Regime:       string(p.Regime),
		Note:         p.Note,
		SupersedesID: p.SupersedesID,
		Superseded:   superseded,
		CreatedAt:    p.CreatedAt.UTC(),
	}
}

// handleListVATRegimes answers GET /api/v1/vat-regimes for any role.
func (a *app) handleListVATRegimes(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}

	var periods []storage.VATRegimePeriod
	err = a.profileInTenant(r.Context(), c, func(ctx context.Context) error {
		var err error
		periods, err = a.repos.VATRegimePeriods.List(ctx)

		return err
	})
	if err != nil {
		return err
	}

	superseded := map[uuid.UUID]bool{}
	for _, p := range periods {
		if p.SupersedesID != nil {
			superseded[*p.SupersedesID] = true
		}
	}

	out := make([]vatRegimeResponse, len(periods))
	for i, p := range periods {
		out[i] = newVATRegimeResponse(p, superseded[p.ID])
	}

	return api.Write(w, http.StatusOK, map[string]any{"periods": out})
}

// vatRegimeBody is the request of POST /api/v1/vat-regimes.
type vatRegimeBody struct {
	ValidFrom    string  `json:"valid_from"`
	Regime       string  `json:"regime"`
	Note         *string `json:"note"`
	SupersedesID *string `json:"supersedes_id"`
}

// handleCreateVATRegime answers POST /api/v1/vat-regimes, owner only. A
// period is never edited or deleted: a wrong one is corrected by a new
// one naming it in supersedes_id.
func (a *app) handleCreateVATRegime(w http.ResponseWriter, r *http.Request) error {
	c, err := a.profileOwner(r)
	if err != nil {
		return err
	}

	var body vatRegimeBody
	if err := api.Decode(r, &body); err != nil {
		return err
	}

	problems := map[string]string{}

	validFrom, err := time.Parse(vatDateLayout, body.ValidFrom)
	if err != nil {
		problems["valid_from"] = "must be a date such as 2026-10-14"
	} else if validFrom.Before(vatEarliestValidFrom) || validFrom.After(time.Now().UTC().AddDate(1, 0, 0)) {
		problems["valid_from"] = "must be between 2000-01-01 and one year from today"
	}

	regime := storage.VATRegime(body.Regime)
	if regime != storage.VATRegimeStandard && regime != storage.VATRegimeKleinunternehmer {
		problems["regime"] = "must be standard or kleinunternehmer"
	}

	var note *string
	if body.Note != nil {
		if v := strings.TrimSpace(*body.Note); v != "" {
			note = &v
		}
	}
	if note != nil && len([]rune(*note)) > maxVATPeriodNote {
		problems["note"] = "too long"
	}

	var supersedes *uuid.UUID
	if body.SupersedesID != nil {
		id, err := uuid.Parse(*body.SupersedesID)
		if err != nil {
			problems["supersedes_id"] = "must be a period id"
		} else {
			supersedes = &id
		}
	}

	if len(problems) > 0 {
		return profileInvalid("the period has invalid values", problems)
	}

	var out vatRegimeResponse
	err = a.profileInTenant(r.Context(), c, func(ctx context.Context) error {
		created, err := a.repos.VATRegimePeriods.Create(ctx, storage.VATRegimePeriod{
			TenantID:     c.tenantID,
			ValidFrom:    validFrom,
			Regime:       regime,
			Note:         note,
			SupersedesID: supersedes,
		})
		if errors.Is(err, storage.ErrConflict) {
			return api.Conflict("an effective period with this valid_from exists, or the named period is superseded already")
		}
		if errors.Is(err, storage.ErrNotFound) {
			return api.NotFound("no such period")
		}
		if err != nil {
			return err
		}
		out = newVATRegimeResponse(created, false)

		return nil
	})
	if err != nil {
		return err
	}

	return api.Write(w, http.StatusCreated, out)
}
