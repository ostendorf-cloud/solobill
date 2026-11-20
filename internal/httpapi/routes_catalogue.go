package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/api"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// registerCatalogue mounts the catalogue items API. Owned by issue #69
// (I3). See docs/api.md, "Catalogue items".
func registerCatalogue(mux *http.ServeMux, route routeFunc, a *app) {
	mux.Handle("GET /api/v1/catalogue-items", route(api.Handle(a.handleListCatalogueItems)))
	mux.Handle("POST /api/v1/catalogue-items", route(api.Handle(a.handleCreateCatalogueItem)))
	mux.Handle("GET /api/v1/catalogue-items/{id}", route(api.Handle(a.handleGetCatalogueItem)))
	mux.Handle("PUT /api/v1/catalogue-items/{id}", route(api.Handle(a.handleUpdateCatalogueItem)))
	mux.Handle("DELETE /api/v1/catalogue-items/{id}", route(api.Handle(a.handleDeleteCatalogueItem)))
}

// catalogueItemJSON is the item of docs/api.md.
type catalogueItemJSON struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	Description    *string    `json:"description"`
	SKU            *string    `json:"sku"`
	UnitPriceCents int64      `json:"unit_price_cents"`
	Unit           string     `json:"unit"`
	DeletedAt      *time.Time `json:"deleted_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// catalogueItemRequest is the body of POST and PUT. The tenant is never
// part of it: it comes from the session only.
type catalogueItemRequest struct {
	Name           string  `json:"name"`
	Description    *string `json:"description"`
	SKU            *string `json:"sku"`
	UnitPriceCents int64   `json:"unit_price_cents"`
	Unit           string  `json:"unit"`
}

func toCatalogueItemJSON(i storage.CatalogueItem) catalogueItemJSON {
	return catalogueItemJSON{
		ID:             i.ID,
		Name:           i.Name,
		Description:    i.Description,
		SKU:            i.SKU,
		UnitPriceCents: i.UnitPriceCents,
		Unit:           i.Unit,
		DeletedAt:      i.DeletedAt,
		CreatedAt:      i.CreatedAt,
		UpdatedAt:      i.UpdatedAt,
	}
}

// item validates the request and builds the domain value, without id or
// tenant. An omitted unit is "piece", the column's own default, for
// Update too: the body is a full replace.
func (r catalogueItemRequest) item() (storage.CatalogueItem, error) {
	details := map[string]any{}

	name := strings.TrimSpace(r.Name)
	if name == "" {
		details["name"] = "required"
	}
	if r.UnitPriceCents < 0 {
		details["unit_price_cents"] = "must not be negative"
	}

	unit := r.Unit
	switch unit {
	case "":
		unit = "piece"
	case "piece", "hour", "day":
	default:
		details["unit"] = "must be piece, hour or day"
	}

	if len(details) > 0 {
		return storage.CatalogueItem{}, masterDataInvalid(details)
	}

	return storage.CatalogueItem{
		Name:           name,
		Description:    masterDataText(r.Description),
		SKU:            masterDataText(r.SKU),
		UnitPriceCents: r.UnitPriceCents,
		Unit:           unit,
	}, nil
}

const catalogueSKUConflict = "an item with this sku exists already"

func (a *app) handleListCatalogueItems(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if a.repos.CatalogueItems == nil || a.repos.Scope == nil {
		return a.notImplemented(w, r)
	}

	var list []storage.CatalogueItem
	err = a.repos.Scope.WithTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		var err error
		list, err = a.repos.CatalogueItems.List(ctx)

		return err
	})
	if err != nil {
		return masterDataError(err, "")
	}

	out := make([]catalogueItemJSON, len(list))
	for i, item := range list {
		out[i] = toCatalogueItemJSON(item)
	}

	return api.Write(w, http.StatusOK, map[string]any{"items": out})
}

func (a *app) handleCreateCatalogueItem(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if a.repos.CatalogueItems == nil || a.repos.Scope == nil {
		return a.notImplemented(w, r)
	}

	var req catalogueItemRequest
	if err := api.Decode(r, &req); err != nil {
		return err
	}
	item, err := req.item()
	if err != nil {
		return err
	}
	// The tenant comes from the session, never from the body.
	item.TenantID = c.tenantID

	var created storage.CatalogueItem
	err = a.repos.Scope.WithTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		var err error
		created, err = a.repos.CatalogueItems.Create(ctx, item)

		return err
	})
	if err != nil {
		return masterDataError(err, catalogueSKUConflict)
	}

	return api.Write(w, http.StatusCreated, toCatalogueItemJSON(created))
}

func (a *app) handleGetCatalogueItem(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if a.repos.CatalogueItems == nil || a.repos.Scope == nil {
		return a.notImplemented(w, r)
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	var found storage.CatalogueItem
	err = a.repos.Scope.WithTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		var err error
		found, err = a.repos.CatalogueItems.FindByID(ctx, id)

		return err
	})
	if err != nil {
		return masterDataError(err, "")
	}

	return api.Write(w, http.StatusOK, toCatalogueItemJSON(found))
}

func (a *app) handleUpdateCatalogueItem(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if a.repos.CatalogueItems == nil || a.repos.Scope == nil {
		return a.notImplemented(w, r)
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	var req catalogueItemRequest
	if err := api.Decode(r, &req); err != nil {
		return err
	}
	item, err := req.item()
	if err != nil {
		return err
	}
	item.ID = id
	item.TenantID = c.tenantID

	var updated storage.CatalogueItem
	err = a.repos.Scope.WithTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		var err error
		updated, err = a.repos.CatalogueItems.Update(ctx, item)

		return err
	})
	if err != nil {
		return masterDataError(err, catalogueSKUConflict)
	}

	return api.Write(w, http.StatusOK, toCatalogueItemJSON(updated))
}

func (a *app) handleDeleteCatalogueItem(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if a.repos.CatalogueItems == nil || a.repos.Scope == nil {
		return a.notImplemented(w, r)
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	err = a.repos.Scope.WithTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		return a.repos.CatalogueItems.Delete(ctx, id)
	})
	if err != nil {
		return masterDataError(err, "")
	}

	api.NoContent(w)

	return nil
}
