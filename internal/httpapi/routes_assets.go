package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/api"

	"github.com/ostendorf-cloud/solobill/internal/afa"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// registerAssets mounts the asset register. Owned by issue #78 (I11a).
// See docs/api.md, "Assets". Every route answers 501 while
// Repositories.Assets is not wired.
func registerAssets(mux *http.ServeMux, route routeFunc, a *app) {
	mux.Handle("GET /api/v1/assets", route(api.Handle(a.handleListAssets)))
	mux.Handle("POST /api/v1/assets", route(api.Handle(a.handleCreateAsset)))
	mux.Handle("GET /api/v1/assets/{id}", route(api.Handle(a.handleGetAsset)))
	mux.Handle("PUT /api/v1/assets/{id}", route(api.Handle(a.handleUpdateAsset)))
	mux.Handle("POST /api/v1/assets/{id}/disposal", route(api.Handle(a.handleAssetDisposal)))
	mux.Handle("GET /api/v1/assets/{id}/schedule", route(api.Handle(a.handleAssetSchedule)))
}

const (
	assetDateLayout = "2006-01-02"
	maxAssetName    = 200
	// codeVATRegimeMissing is docs/api.md's 400 when no VAT regime period
	// covers a date.
	codeVATRegimeMissing = "vat_regime_missing"
)

// assetJSON is the asset of docs/api.md.
type assetJSON struct {
	ID                    uuid.UUID  `json:"id"`
	Name                  string     `json:"name"`
	AcquiredOn            string     `json:"acquired_on"`
	CostBasisCents        int64      `json:"cost_basis_cents"`
	Method                string     `json:"method"`
	Group                 string     `json:"group"`
	UsefulLifeMonths      *int32     `json:"useful_life_months"`
	ExpenseID             *uuid.UUID `json:"expense_id"`
	OpeningBookValueCents *int64     `json:"opening_book_value_cents"`
	OpeningBookValueYear  *int32     `json:"opening_book_value_year"`
	DisposedOn            *string    `json:"disposed_on"`
	DisposalProceedsCents *int64     `json:"disposal_proceeds_cents"`
	Imported              bool       `json:"imported"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

func toAssetJSON(a storage.Asset) assetJSON {
	var disposed *string
	if a.DisposedOn != nil {
		s := a.DisposedOn.Format(assetDateLayout)
		disposed = &s
	}

	return assetJSON{
		ID:                    a.ID,
		Name:                  a.Name,
		AcquiredOn:            a.AcquiredOn.Format(assetDateLayout),
		CostBasisCents:        a.CostBasisCents,
		Method:                string(a.Method),
		Group:                 string(a.Group),
		UsefulLifeMonths:      a.UsefulLifeMonths,
		ExpenseID:             a.ExpenseID,
		OpeningBookValueCents: a.OpeningBookValueCents,
		OpeningBookValueYear:  a.OpeningBookValueYear,
		DisposedOn:            disposed,
		DisposalProceedsCents: a.DisposalProceedsCents,
		Imported:              a.Imported,
		CreatedAt:             a.CreatedAt.UTC(),
		UpdatedAt:             a.UpdatedAt.UTC(),
	}
}

// assetRequest is the body of POST and PUT: the asset JSON without id,
// the disposal, imported and the timestamps, which are refused as unknown
// fields. The tenant comes from the session only.
type assetRequest struct {
	Name                  string  `json:"name"`
	AcquiredOn            string  `json:"acquired_on"`
	CostBasisCents        int64   `json:"cost_basis_cents"`
	Method                string  `json:"method"`
	Group                 string  `json:"group"`
	UsefulLifeMonths      *int32  `json:"useful_life_months"`
	ExpenseID             *string `json:"expense_id"`
	OpeningBookValueCents *int64  `json:"opening_book_value_cents"`
	OpeningBookValueYear  *int32  `json:"opening_book_value_year"`
}

// asset validates the request with internal/afa's rules and builds the
// domain value, without id and tenant. today bounds acquired_on: an asset
// is entered once it is acquired.
func (r assetRequest) asset(today time.Time) (storage.Asset, error) {
	p := map[string]string{}

	name := strings.TrimSpace(r.Name)
	switch {
	case name == "":
		p["name"] = "required"
	case len([]rune(name)) > maxAssetName:
		p["name"] = "too long"
	}

	acquired, err := time.Parse(assetDateLayout, r.AcquiredOn)
	if err != nil {
		p["acquired_on"] = "must be a date such as 2026-03-10"
	} else if acquired.After(today) {
		p["acquired_on"] = "must not be in the future"
	}

	var expenseID *uuid.UUID
	if r.ExpenseID != nil {
		id, err := uuid.Parse(*r.ExpenseID)
		if err != nil {
			p["expense_id"] = "must be an expense id"
		} else {
			expenseID = &id
		}
	}

	if (r.OpeningBookValueCents == nil) != (r.OpeningBookValueYear == nil) {
		p["opening_book_value_year"] = "opening_book_value_cents and opening_book_value_year go together"
	}

	a := storage.Asset{
		Name:                  name,
		AcquiredOn:            acquired,
		CostBasisCents:        r.CostBasisCents,
		Method:                storage.DepreciationMethod(r.Method),
		Group:                 storage.AssetGroup(r.Group),
		UsefulLifeMonths:      r.UsefulLifeMonths,
		ExpenseID:             expenseID,
		OpeningBookValueCents: r.OpeningBookValueCents,
		OpeningBookValueYear:  r.OpeningBookValueYear,
	}

	if _, dateBad := p["acquired_on"]; !dateBad {
		if invalid := afa.Validate(toAFAAsset(a)); invalid != nil {
			for field, reason := range invalid.Problems {
				if _, set := p[field]; !set {
					p[field] = reason
				}
			}
		}
	}

	if len(p) > 0 {
		return storage.Asset{}, profileInvalid("the asset is invalid", p)
	}

	return a, nil
}

// toAFAAsset is the afa view of a stored or requested asset. A useful
// life on a non linear method is kept, so afa.Validate refuses it.
func toAFAAsset(a storage.Asset) afa.Asset {
	out := afa.Asset{
		AcquiredOn:     a.AcquiredOn,
		CostBasisCents: a.CostBasisCents,
		Method:         afa.Method(a.Method),
		Group:          afa.Group(a.Group),
		DisposedOn:     a.DisposedOn,
	}
	if a.UsefulLifeMonths != nil {
		out.UsefulLifeMonths = int(*a.UsefulLifeMonths)
		if out.UsefulLifeMonths == 0 {
			// 0 is "not set" in afa; an explicit 0 must still be refused.
			out.UsefulLifeMonths = -1
		}
	}
	if a.OpeningBookValueCents != nil && a.OpeningBookValueYear != nil {
		out.Opening = &afa.Opening{Year: int(*a.OpeningBookValueYear), BookValueCents: *a.OpeningBookValueCents}
	}

	return out
}

// assetError maps a repository error to the API error. An *api.Error
// from inside the scope passes through unchanged.
func assetError(err error, conflict string) error {
	var apiErr *api.Error
	switch {
	case err == nil:
		return nil
	case errors.As(err, &apiErr):
		return apiErr
	case errors.Is(err, storage.ErrNotFound):
		return api.NotFound("not found")
	case errors.Is(err, storage.ErrConflict):
		return api.Conflict(conflict)
	default:
		return api.Internal(fmt.Errorf("httpapi: assets: %w", err))
	}
}

// assetsReady answers the session check, then 501 while the register is
// not wired.
func (a *app) assetsReady(w http.ResponseWriter, r *http.Request, owner bool) (caller, bool, error) {
	c, err := a.requireSession(r)
	if err != nil {
		return caller{}, false, err
	}
	if a.repos.Assets == nil || a.repos.Scope == nil {
		return caller{}, false, a.notImplemented(w, r)
	}
	if owner && c.role != storage.AccountRoleOwner {
		return caller{}, false, api.PermissionDenied("owner role required")
	}

	return c, true, nil
}

// checkAssetExpense refuses an expense_id that does not name a booked,
// non storno expense with is_asset set, and a cost basis that differs
// from that expense's: the net price under the Regelbesteuerung, the
// gross price for a Kleinunternehmer on the day it was paid
// (afa.CostBasis, not verified with a Steuerberater).
//
// While the expense repository of issue #76 is not wired, every
// expense_id is refused: the assets table has no foreign key to expenses
// yet, so nothing else would catch a wrong id.
func (a *app) checkAssetExpense(ctx context.Context, asset storage.Asset) error {
	if asset.ExpenseID == nil {
		return nil
	}
	if a.repos.Expenses == nil || a.repos.VATRegimePeriods == nil {
		return profileInvalid("the asset is invalid", map[string]string{"expense_id": "expenses are not available yet"})
	}

	expense, err := a.repos.Expenses.FindByID(ctx, *asset.ExpenseID)
	if errors.Is(err, storage.ErrNotFound) {
		return profileInvalid("the asset is invalid", map[string]string{"expense_id": "no such expense"})
	}
	if err != nil {
		return err
	}

	switch {
	case expense.State != storage.BookingStateBooked || expense.PaidOn == nil:
		return profileInvalid("the asset is invalid", map[string]string{"expense_id": "must be a booked expense"})
	case !expense.IsAsset:
		return profileInvalid("the asset is invalid", map[string]string{"expense_id": "must be an expense with is_asset set"})
	case expense.StornoOfID != nil:
		return profileInvalid("the asset is invalid", map[string]string{"expense_id": "must not be a storno row"})
	}

	period, err := a.repos.VATRegimePeriods.RegimeOn(ctx, *expense.PaidOn)
	if errors.Is(err, storage.ErrNotFound) {
		return api.New(http.StatusBadRequest, codeVATRegimeMissing, "no VAT regime period covers the payment date of the expense")
	}
	if err != nil {
		return err
	}

	basis, err := afa.CostBasis(afa.Regime(period.Regime), expense.GrossCents, expense.NetCents)
	if err != nil {
		return profileInvalid("the asset is invalid", map[string]string{"expense_id": "the expense amounts do not give a cost basis"})
	}
	if basis != asset.CostBasisCents {
		return profileInvalid("the asset is invalid", map[string]string{
			"cost_basis_cents": fmt.Sprintf("must be %d, the expense's cost basis under the %s regime", basis, period.Regime),
		})
	}

	return nil
}

func (a *app) handleListAssets(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.assetsReady(w, r, false)
	if !ok {
		return err
	}

	var list []storage.Asset
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		var err error
		list, err = a.repos.Assets.List(ctx)

		return err
	})
	if err != nil {
		return assetError(err, "")
	}

	out := make([]assetJSON, len(list))
	for i, item := range list {
		out[i] = toAssetJSON(item)
	}

	return api.Write(w, http.StatusOK, map[string]any{"assets": out})
}

func (a *app) handleCreateAsset(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.assetsReady(w, r, true)
	if !ok {
		return err
	}

	var req assetRequest
	if err := api.Decode(r, &req); err != nil {
		return err
	}
	asset, err := req.asset(time.Now().UTC())
	if err != nil {
		return err
	}
	asset.TenantID = c.tenantID

	var created storage.Asset
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		if err := a.checkAssetExpense(ctx, asset); err != nil {
			return err
		}
		var err error
		created, err = a.repos.Assets.Create(ctx, asset)

		return err
	})
	if err != nil {
		return assetError(err, "the expense is already an asset")
	}

	return api.Write(w, http.StatusCreated, toAssetJSON(created))
}

func (a *app) handleGetAsset(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.assetsReady(w, r, false)
	if !ok {
		return err
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	found, err := a.findAsset(r.Context(), c, id)
	if err != nil {
		return err
	}

	return api.Write(w, http.StatusOK, toAssetJSON(found))
}

func (a *app) findAsset(ctx context.Context, c caller, id uuid.UUID) (storage.Asset, error) {
	var found storage.Asset
	err := a.inTenant(ctx, c.tenantID, func(ctx context.Context) error {
		var err error
		found, err = a.repos.Assets.FindByID(ctx, id)

		return err
	})

	return found, assetError(err, "")
}

// handleUpdateAsset answers PUT /api/v1/assets/{id}. The repository
// refuses with 409 once the first schedule year of the stored asset, or
// of the replacement, has ended, and for a disposed asset.
func (a *app) handleUpdateAsset(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.assetsReady(w, r, true)
	if !ok {
		return err
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	var req assetRequest
	if err := api.Decode(r, &req); err != nil {
		return err
	}
	now := time.Now().UTC()
	asset, err := req.asset(now)
	if err != nil {
		return err
	}
	asset.ID = id
	asset.TenantID = c.tenantID

	var updated storage.Asset
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		if err := a.checkAssetExpense(ctx, asset); err != nil {
			return err
		}
		var err error
		updated, err = a.repos.Assets.Update(ctx, asset, now)

		return err
	})
	if err != nil {
		return assetError(err, "the first schedule year of the asset has ended or it is disposed, record a disposal instead")
	}

	return api.Write(w, http.StatusOK, toAssetJSON(updated))
}

// assetDisposalRequest is the body of POST /api/v1/assets/{id}/disposal.
type assetDisposalRequest struct {
	DisposedOn    string `json:"disposed_on"`
	ProceedsCents *int64 `json:"proceeds_cents"`
}

func (a *app) handleAssetDisposal(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.assetsReady(w, r, true)
	if !ok {
		return err
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	var req assetDisposalRequest
	if err := api.Decode(r, &req); err != nil {
		return err
	}

	p := map[string]string{}
	disposedOn, err := time.Parse(assetDateLayout, req.DisposedOn)
	if err != nil {
		p["disposed_on"] = "must be a date such as 2026-12-01"
	} else if disposedOn.After(time.Now().UTC()) {
		p["disposed_on"] = "must not be in the future"
	}
	if req.ProceedsCents != nil && *req.ProceedsCents < 0 {
		p["proceeds_cents"] = "must not be negative"
	}
	if len(p) > 0 {
		return profileInvalid("the disposal is invalid", p)
	}

	var disposed storage.Asset
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		current, err := a.repos.Assets.FindByID(ctx, id)
		if err != nil {
			return err
		}
		if current.DisposedOn != nil {
			return storage.ErrConflict
		}
		current.DisposedOn = &disposedOn
		if invalid := afa.Validate(toAFAAsset(current)); invalid != nil {
			if reason, found := invalid.Problems["disposed_on"]; found {
				return profileInvalid("the disposal is invalid", map[string]string{"disposed_on": reason})
			}

			return fmt.Errorf("stored asset %s is invalid: %w", id, invalid)
		}

		disposed, err = a.repos.Assets.RecordDisposal(ctx, id, disposedOn, req.ProceedsCents)

		return err
	})
	if err != nil {
		return assetError(err, "the asset is already disposed")
	}

	return api.Write(w, http.StatusOK, toAssetJSON(disposed))
}

// assetScheduleYearJSON is one year of docs/api.md's schedule.
type assetScheduleYearJSON struct {
	Year                  int    `json:"year"`
	OpeningBookValueCents int64  `json:"opening_book_value_cents"`
	DepreciationCents     int64  `json:"depreciation_cents"`
	ClosingBookValueCents int64  `json:"closing_book_value_cents"`
	EuerKey               string `json:"euer_key"`
}

func (a *app) handleAssetSchedule(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.assetsReady(w, r, false)
	if !ok {
		return err
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	found, err := a.findAsset(r.Context(), c, id)
	if err != nil {
		return err
	}

	years, err := afa.Schedule(toAFAAsset(found))
	if err != nil {
		return api.Internal(fmt.Errorf("httpapi: schedule of asset %s: %w", id, err))
	}

	out := make([]assetScheduleYearJSON, len(years))
	for i, y := range years {
		out[i] = assetScheduleYearJSON{
			Year:                  y.Year,
			OpeningBookValueCents: y.OpeningBookValueCents,
			DepreciationCents:     y.DepreciationCents,
			ClosingBookValueCents: y.ClosingBookValueCents,
			EuerKey:               string(y.Key),
		}
	}

	return api.Write(w, http.StatusOK, map[string]any{
		"asset_id": found.ID,
		"method":   string(found.Method),
		"years":    out,
	})
}
