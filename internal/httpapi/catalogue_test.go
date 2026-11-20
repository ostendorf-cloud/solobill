package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// catalogueItemResponse is the item JSON of docs/api.md.
type catalogueItemResponse struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	Description    *string    `json:"description"`
	SKU            *string    `json:"sku"`
	UnitPriceCents int64      `json:"unit_price_cents"`
	Unit           string     `json:"unit"`
	DeletedAt      *time.Time `json:"deleted_at"`
}

func catalogueRepo(t *testing.T, app testApp) *fakeCatalogueRepository {
	t.Helper()

	repo, ok := app.repos.CatalogueItems.(*fakeCatalogueRepository)
	if !ok {
		t.Fatalf("CatalogueItems = %T, want the fake", app.repos.CatalogueItems)
	}

	return repo
}

func TestCatalogueRequiresASession(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/catalogue-items"},
		{http.MethodPost, "/api/v1/catalogue-items"},
		{http.MethodGet, "/api/v1/catalogue-items/" + uuid.NewString()},
		{http.MethodPut, "/api/v1/catalogue-items/" + uuid.NewString()},
		{http.MethodDelete, "/api/v1/catalogue-items/" + uuid.NewString()},
	} {
		rec := doJSON(t, app.handler, tc.method, tc.path, nil, nil, "")
		if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without a session = %d, want 401 or 403", tc.method, tc.path, rec.Code)
		}
	}
}

func TestCatalogueItemLifecycle(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	caller := newMasterDataCaller(t, app, "acme")
	catalogueRepo(t, app).actAs(caller.tenant.ID)

	rec := caller.do(t, app, http.MethodPost, "/api/v1/catalogue-items", map[string]any{
		"name": "Consulting hour", "sku": "CONS-H", "unit_price_cents": 9500, "unit": "hour",
	})
	wantStatus(t, rec, http.StatusCreated)
	created := decodeBody[catalogueItemResponse](t, rec)
	if created.UnitPriceCents != 9500 || created.Unit != "hour" || created.SKU == nil || *created.SKU != "CONS-H" {
		t.Errorf("created item = %+v", created)
	}
	stored := catalogueRepo(t, app).row(created.ID)
	if stored.TenantID != caller.tenant.ID {
		t.Errorf("stored tenant = %v, want the session tenant", stored.TenantID)
	}

	path := "/api/v1/catalogue-items/" + created.ID.String()

	rec = caller.do(t, app, http.MethodPut, path, map[string]any{"name": "Day rate", "unit_price_cents": 70000})
	wantStatus(t, rec, http.StatusOK)
	updated := decodeBody[catalogueItemResponse](t, rec)
	if updated.Name != "Day rate" || updated.Unit != "piece" || updated.SKU != nil {
		t.Errorf("PUT is not a full replace: %+v", updated)
	}

	rec = caller.do(t, app, http.MethodGet, "/api/v1/catalogue-items", nil)
	wantStatus(t, rec, http.StatusOK)
	if list := decodeBody[struct {
		Items []catalogueItemResponse `json:"items"`
	}](t, rec); len(list.Items) != 1 {
		t.Fatalf("list has %d items, want 1", len(list.Items))
	}

	wantStatus(t, caller.do(t, app, http.MethodDelete, path, nil), http.StatusNoContent)

	rec = caller.do(t, app, http.MethodGet, "/api/v1/catalogue-items", nil)
	if list := decodeBody[struct {
		Items []catalogueItemResponse `json:"items"`
	}](t, rec); len(list.Items) != 0 {
		t.Errorf("list has %d items after delete, want 0", len(list.Items))
	}
	rec = caller.do(t, app, http.MethodGet, path, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := decodeBody[catalogueItemResponse](t, rec); got.DeletedAt == nil {
		t.Error("deleted item has no deleted_at")
	}
	wantStatus(t, caller.do(t, app, http.MethodPut, path, map[string]any{"name": "Again"}), http.StatusNotFound)
	wantStatus(t, caller.do(t, app, http.MethodDelete, path, nil), http.StatusNotFound)
}

func TestCatalogueValidation(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	caller := newMasterDataCaller(t, app, "acme")
	catalogueRepo(t, app).actAs(caller.tenant.ID)

	for _, tc := range []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"missing name", map[string]any{"unit_price_cents": 1}, "name"},
		{"negative price", map[string]any{"name": "X", "unit_price_cents": -1}, "unit_price_cents"},
		{"bad unit", map[string]any{"name": "X", "unit": "week"}, "unit"},
		{"wrong type", map[string]any{"name": "X", "unit_price_cents": "cheap"}, "unit_price_cents"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := caller.do(t, app, http.MethodPost, "/api/v1/catalogue-items", tc.body)
			wantStatus(t, rec, http.StatusBadRequest)
			got := decodeBody[errorBody](t, rec)
			if got.Error.Code != "invalid_argument" {
				t.Errorf("code = %q, want invalid_argument", got.Error.Code)
			}
			if _, ok := got.Error.Details[tc.field]; !ok {
				t.Errorf("details = %v, want a key %q directly, with no wrapper", got.Error.Details, tc.field)
			}
		})
	}
}

func TestCatalogueDuplicateSKUConflicts(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	caller := newMasterDataCaller(t, app, "acme")
	catalogueRepo(t, app).actAs(caller.tenant.ID)

	wantStatus(t, caller.do(t, app, http.MethodPost, "/api/v1/catalogue-items", map[string]any{"name": "A", "sku": "S-1"}), http.StatusCreated)
	wantStatus(t, caller.do(t, app, http.MethodPost, "/api/v1/catalogue-items", map[string]any{"name": "B", "sku": "s-1"}), http.StatusConflict)

	rec := caller.do(t, app, http.MethodPost, "/api/v1/catalogue-items", map[string]any{"name": "C", "sku": "S-2"})
	wantStatus(t, rec, http.StatusCreated)
	path := "/api/v1/catalogue-items/" + decodeBody[catalogueItemResponse](t, rec).ID.String()
	wantStatus(t, caller.do(t, app, http.MethodPut, path, map[string]any{"name": "C", "sku": "S-1"}), http.StatusConflict)
}

func TestCatalogueItemsOfAnotherTenantAnswer404(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	a := newMasterDataCaller(t, app, "tenant-a")
	b := newMasterDataCaller(t, app, "tenant-b")
	repo := catalogueRepo(t, app)

	repo.actAs(a.tenant.ID)
	rec := a.do(t, app, http.MethodPost, "/api/v1/catalogue-items", map[string]any{"name": "Secret item"})
	wantStatus(t, rec, http.StatusCreated)
	id := decodeBody[catalogueItemResponse](t, rec).ID
	path := "/api/v1/catalogue-items/" + id.String()

	repo.actAs(b.tenant.ID)
	wantStatus(t, b.do(t, app, http.MethodGet, path, nil), http.StatusNotFound)
	wantStatus(t, b.do(t, app, http.MethodPut, path, map[string]any{"name": "Hijacked"}), http.StatusNotFound)
	wantStatus(t, b.do(t, app, http.MethodDelete, path, nil), http.StatusNotFound)

	rec = b.do(t, app, http.MethodGet, "/api/v1/catalogue-items", nil)
	if list := decodeBody[struct {
		Items []catalogueItemResponse `json:"items"`
	}](t, rec); len(list.Items) != 0 {
		t.Errorf("tenant B lists %d items of tenant A", len(list.Items))
	}

	got := repo.row(id)
	if got.Name != "Secret item" || got.DeletedAt != nil {
		t.Errorf("tenant A's row changed: %+v", got)
	}
}
