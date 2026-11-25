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
	"github.com/ostendorf-cloud/solobill/internal/docissue"
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
	// BookValueCents, BookValueYear, DisposalRestCents and Editable are
	// derived on every answer and never accepted in a request (see
	// docs/api.md).
	BookValueCents    *int64    `json:"book_value_cents"`
	BookValueYear     *int      `json:"book_value_year"`
	DisposalRestCents *int64    `json:"disposal_rest_cents"`
	Editable          bool      `json:"editable"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// toAssetJSON is the asset JSON at the German calendar year currentYear.
// A stored asset internal/afa refuses does not fail the answer: it is
// logged by id and answered with null derived figures and editable false,
// so one bad row does not hide the rest of the register.
func (a *app) toAssetJSON(ctx context.Context, asset storage.Asset, currentYear int) assetJSON {
	var disposed *string
	if asset.DisposedOn != nil {
		s := asset.DisposedOn.Format(assetDateLayout)
		disposed = &s
	}

	out := assetJSON{
		ID:                    asset.ID,
		Name:                  asset.Name,
		AcquiredOn:            asset.AcquiredOn.Format(assetDateLayout),
		CostBasisCents:        asset.CostBasisCents,
		Method:                string(asset.Method),
		Group:                 string(asset.Group),
		UsefulLifeMonths:      asset.UsefulLifeMonths,
		ExpenseID:             asset.ExpenseID,
		OpeningBookValueCents: asset.OpeningBookValueCents,
		OpeningBookValueYear:  asset.OpeningBookValueYear,
		DisposedOn:            disposed,
		DisposalProceedsCents: asset.DisposalProceedsCents,
		Imported:              asset.Imported,
		CreatedAt:             asset.CreatedAt.UTC(),
		UpdatedAt:             asset.UpdatedAt.UTC(),
	}

	value, err := afa.CurrentBookValue(toAFAAsset(asset), currentYear)
	if err != nil {
		// The afa reasons are fixed texts about fields, never the
		// owner's own text.
		a.logger.WarnContext(ctx, "stored asset has no book value", "asset_id", asset.ID, "error", err)

		return out
	}
	out.BookValueCents = &value.Cents
	if value.Year != 0 {
		out.BookValueYear = &value.Year
	}
	out.DisposalRestCents = value.DisposalRestCents
	out.Editable = assetEditConflict(asset, asset, currentYear) == nil

	return out
}

// Codes of the asset routes' 409 answers. The client tells the reasons
// apart by these, never by the message.
const (
	// codeAssetYearEnded: the first schedule year of the stored asset or
	// of the replacement has ended, or a disposal is dated in an ended
	// year.
	codeAssetYearEnded = "asset_year_ended"
	// codeAssetDisposed: the asset is disposed already.
	codeAssetDisposed = "asset_disposed"
	// codeExpenseAlreadyAnAsset: another asset came from the expense.
	codeExpenseAlreadyAnAsset = "expense_already_an_asset"
)

func errAssetYearEnded() *api.Error {
	return api.New(http.StatusConflict, codeAssetYearEnded,
		"the first schedule year of the asset has ended, record a disposal instead")
}

func errAssetDisposed() *api.Error {
	return api.New(http.StatusConflict, codeAssetDisposed, "the asset is disposed")
}

func errExpenseAlreadyAnAsset() *api.Error {
	return api.New(http.StatusConflict, codeExpenseAlreadyAnAsset, "the expense is already an asset")
}

// assetEditConflict is the 409 a PUT that replaces stored with
// replacement gets at the German calendar year currentYear, or nil while
// the edit is allowed. A disposed asset goes first. Then the first
// schedule year of both must not have ended. The repository's Update
// enforces the same rule; this check only gives each reason its code.
// With replacement equal to stored it is the editable flag.
func assetEditConflict(stored, replacement storage.Asset, currentYear int) *api.Error {
	switch {
	case stored.DisposedOn != nil:
		return errAssetDisposed()
	case afa.FirstScheduleYear(toAFAAsset(stored)) < currentYear,
		afa.FirstScheduleYear(toAFAAsset(replacement)) < currentYear:
		return errAssetYearEnded()
	default:
		return nil
	}
}

// writeAsset answers status with the asset JSON at now.
func (a *app) writeAsset(w http.ResponseWriter, r *http.Request, status int, asset storage.Asset, now time.Time) error {
	return api.Write(w, status, a.toAssetJSON(r.Context(), asset, assetToday(now).Year()))
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

	switch {
	case (r.OpeningBookValueCents == nil) != (r.OpeningBookValueYear == nil):
		p["opening_book_value_year"] = "opening_book_value_cents and opening_book_value_year go together"
	case r.OpeningBookValueYear != nil && int(*r.OpeningBookValueYear) > today.Year():
		// A book value at 1 January of a later year would leave the years
		// in between without depreciation and keep the asset editable
		// until that year ends. internal/afa has no clock, so the bound
		// lives here, in the German year of today.
		p["opening_book_value_year"] = "must not be after the current year"
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
// from inside the scope passes through unchanged. storage.ErrConflict
// becomes conflict, or a plain 409 when conflict is nil.
func assetError(err error, conflict *api.Error) error {
	var apiErr *api.Error
	switch {
	case err == nil:
		return nil
	case errors.As(err, &apiErr):
		return apiErr
	case errors.Is(err, storage.ErrNotFound):
		return api.NotFound("not found")
	case errors.Is(err, storage.ErrConflict) && conflict != nil:
		return conflict
	case errors.Is(err, storage.ErrConflict):
		return api.Conflict("the asset changed meanwhile, load it again")
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
// The table's composite foreign key only proves the expense exists in
// the tenant. Without an expense repository (a deployment that wires
// none) every expense_id is refused, since none of these checks could
// run.
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

	// One expense becomes at most one asset. The unique index
	// assets_expense_once is the guard; this check only gives the 409 its
	// own code, also on PUT, whose repository conflict means a closed year
	// or a disposal.
	others, err := a.repos.Assets.List(ctx)
	if err != nil {
		return err
	}
	for _, other := range others {
		if other.ID != asset.ID && other.ExpenseID != nil && *other.ExpenseID == *asset.ExpenseID {
			return errExpenseAlreadyAnAsset()
		}
	}

	return nil
}

// assetToday is the calendar date of now in Germany, as midnight UTC.
// Every date rule of the register uses it: the year an EÜR is filed for
// is a German calendar year, so at 00:30 on 1 January in Berlin the old
// year has ended, although it is still 31 December in UTC.
func assetToday(now time.Time) time.Time {
	return docissue.IssueDate(now)
}

// disposalDateRule checks a disposal date at now. problem is a 400
// reason for a date in the future. yearEnded is the owner's rule: a
// disposal dated in a calendar year that has ended is refused with 409
// like a PUT, because that year's figures may already be in a filed EÜR.
// A disposal in the current year stays possible.
func disposalDateRule(disposedOn, now time.Time) (problem string, yearEnded bool) {
	today := assetToday(now)
	switch {
	case disposedOn.After(today):
		return "must not be in the future", false
	case disposedOn.Year() < today.Year():
		return "", true
	default:
		return "", false
	}
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
		return assetError(err, nil)
	}

	currentYear := assetToday(a.now()).Year()
	out := make([]assetJSON, len(list))
	for i, item := range list {
		out[i] = a.toAssetJSON(r.Context(), item, currentYear)
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
	now := a.now()
	asset, err := req.asset(assetToday(now))
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
		return assetError(err, errExpenseAlreadyAnAsset())
	}

	return a.writeAsset(w, r, http.StatusCreated, created, now)
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

	return a.writeAsset(w, r, http.StatusOK, found, a.now())
}

func (a *app) findAsset(ctx context.Context, c caller, id uuid.UUID) (storage.Asset, error) {
	var found storage.Asset
	err := a.inTenant(ctx, c.tenantID, func(ctx context.Context) error {
		var err error
		found, err = a.repos.Assets.FindByID(ctx, id)

		return err
	})

	return found, assetError(err, nil)
}

// handleUpdateAsset answers PUT /api/v1/assets/{id}. A disposed asset is
// 409 asset_disposed. Once the first schedule year of the stored asset,
// or of the replacement, has ended in Germany at now, it is 409
// asset_year_ended. Both come before the expense checks, whose 409 is
// expense_already_an_asset. The repository's Update stays the guard of
// the first two; a conflict it finds after these checks passed is a
// plain 409 conflict (a concurrent change).
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
	now := a.now()
	asset, err := req.asset(assetToday(now))
	if err != nil {
		return err
	}
	asset.ID = id
	asset.TenantID = c.tenantID

	var updated storage.Asset
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		stored, err := a.repos.Assets.FindByID(ctx, id)
		if err != nil {
			return err
		}
		if conflict := assetEditConflict(stored, asset, assetToday(now).Year()); conflict != nil {
			return conflict
		}
		if err := a.checkAssetExpense(ctx, asset); err != nil {
			return err
		}
		updated, err = a.repos.Assets.Update(ctx, asset, now)

		return err
	})
	if err != nil {
		return assetError(err, nil)
	}

	return a.writeAsset(w, r, http.StatusOK, updated, now)
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

	now := a.now()
	p := map[string]string{}
	yearEnded := false
	disposedOn, err := time.Parse(assetDateLayout, req.DisposedOn)
	if err != nil {
		p["disposed_on"] = "must be a date such as 2026-12-01"
	} else {
		var problem string
		if problem, yearEnded = disposalDateRule(disposedOn, now); problem != "" {
			p["disposed_on"] = problem
		}
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
			return errAssetDisposed()
		}
		// A date the asset itself refuses (before acquired_on or the
		// opening year) is a 400 first, also when its year has ended: the
		// request is wrong whatever the year.
		current.DisposedOn = &disposedOn
		if invalid := afa.Validate(toAFAAsset(current)); invalid != nil {
			if reason, found := invalid.Problems["disposed_on"]; found {
				return profileInvalid("the disposal is invalid", map[string]string{"disposed_on": reason})
			}

			return fmt.Errorf("stored asset %s is invalid: %w", id, invalid)
		}
		if yearEnded {
			return api.New(http.StatusConflict, codeAssetYearEnded,
				"the disposal date lies in a year that has ended, whose figures may already be filed")
		}

		disposed, err = a.repos.Assets.RecordDisposal(ctx, id, disposedOn, req.ProceedsCents)

		return err
	})
	if err != nil {
		// RecordDisposal's only conflict is an asset disposed already.
		return assetError(err, errAssetDisposed())
	}

	return a.writeAsset(w, r, http.StatusOK, disposed, now)
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
