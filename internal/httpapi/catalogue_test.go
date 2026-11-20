package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
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

	rec := caller.do(t, app, http.MethodPost, "/api/v1/catalogue-items", map[string]any{
		"name": "Consulting hour", "sku": "CONS-H", "unit_price_cents": 9500, "unit": "hour",
	})
	masterDataWantStatus(t, rec, http.StatusCreated)
	created := masterDataDecode[catalogueItemResponse](t, rec)
	if created.UnitPriceCents != 9500 || created.Unit != "hour" || created.SKU == nil || *created.SKU != "CONS-H" {
		t.Errorf("created item = %+v", created)
	}
	if stored := catalogueRepo(t, app).row(created.ID); stored.TenantID != caller.tenant.ID {
		t.Errorf("stored tenant = %v, want the session tenant", stored.TenantID)
	}

	path := "/api/v1/catalogue-items/" + created.ID.String()

	rec = caller.do(t, app, http.MethodPut, path, map[string]any{"name": "Day rate", "unit_price_cents": 70000})
	masterDataWantStatus(t, rec, http.StatusOK)
	updated := masterDataDecode[catalogueItemResponse](t, rec)
	if updated.Name != "Day rate" || updated.Unit != "piece" || updated.SKU != nil {
		t.Errorf("PUT is not a full replace: %+v", updated)
	}

	rec = caller.do(t, app, http.MethodGet, "/api/v1/catalogue-items", nil)
	masterDataWantStatus(t, rec, http.StatusOK)
	if list := masterDataDecode[struct {
		Items []catalogueItemResponse `json:"items"`
	}](t, rec); len(list.Items) != 1 {
		t.Fatalf("list has %d items, want 1", len(list.Items))
	}

	masterDataWantStatus(t, caller.do(t, app, http.MethodDelete, path, nil), http.StatusNoContent)

	rec = caller.do(t, app, http.MethodGet, "/api/v1/catalogue-items", nil)
	if list := masterDataDecode[struct {
		Items []catalogueItemResponse `json:"items"`
	}](t, rec); len(list.Items) != 0 {
		t.Errorf("list has %d items after delete, want 0", len(list.Items))
	}
	rec = caller.do(t, app, http.MethodGet, path, nil)
	masterDataWantStatus(t, rec, http.StatusOK)
	if got := masterDataDecode[catalogueItemResponse](t, rec); got.DeletedAt == nil {
		t.Error("deleted item has no deleted_at")
	}
	masterDataWantStatus(t, caller.do(t, app, http.MethodPut, path, map[string]any{"name": "Again"}), http.StatusNotFound)
	masterDataWantStatus(t, caller.do(t, app, http.MethodDelete, path, nil), http.StatusNotFound)
}

func TestCatalogueValidation(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	caller := newMasterDataCaller(t, app, "acme")

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
			masterDataWantField(t, caller.do(t, app, http.MethodPost, "/api/v1/catalogue-items", tc.body), tc.field)
		})
	}

	// The tenant is never taken from the body, and an unknown field is
	// refused, on create and on update.
	rec := caller.do(t, app, http.MethodPost, "/api/v1/catalogue-items", map[string]any{"name": "Y"})
	masterDataWantStatus(t, rec, http.StatusCreated)
	path := "/api/v1/catalogue-items/" + masterDataDecode[catalogueItemResponse](t, rec).ID.String()
	for _, field := range []string{"tenant_id", "nonsense"} {
		body := map[string]any{"name": "X", field: uuid.NewString()}
		masterDataWantStatus(t, caller.do(t, app, http.MethodPost, "/api/v1/catalogue-items", body), http.StatusBadRequest)
		masterDataWantStatus(t, caller.do(t, app, http.MethodPut, path, body), http.StatusBadRequest)
	}
}

// TestCatalogueBlankOptionalFieldsBecomeNull proves a blank optional
// string never reaches the table's NOT-empty CHECK, which would answer
// 500: it is stored as null, on create and on update.
func TestCatalogueBlankOptionalFieldsBecomeNull(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	caller := newMasterDataCaller(t, app, "acme")

	blank := map[string]any{"name": "Blank", "description": "  ", "sku": ""}

	rec := caller.do(t, app, http.MethodPost, "/api/v1/catalogue-items", blank)
	masterDataWantStatus(t, rec, http.StatusCreated)
	created := masterDataDecode[map[string]any](t, rec)

	rec2 := caller.do(t, app, http.MethodPut, "/api/v1/catalogue-items/"+fmt.Sprint(created["id"]), blank)
	masterDataWantStatus(t, rec2, http.StatusOK)

	for who, got := range map[string]map[string]any{"create": created, "update": masterDataDecode[map[string]any](t, rec2)} {
		for _, key := range []string{"description", "sku"} {
			if got[key] != nil {
				t.Errorf("%s: %s = %v, want null", who, key, got[key])
			}
		}
	}

	// Two items without a sku do not collide.
	masterDataWantStatus(t, caller.do(t, app, http.MethodPost, "/api/v1/catalogue-items", blank), http.StatusCreated)
}

func TestCatalogueDuplicateSKUConflicts(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	caller := newMasterDataCaller(t, app, "acme")

	masterDataWantStatus(t, caller.do(t, app, http.MethodPost, "/api/v1/catalogue-items", map[string]any{"name": "A", "sku": "S-1"}), http.StatusCreated)
	masterDataWantStatus(t, caller.do(t, app, http.MethodPost, "/api/v1/catalogue-items", map[string]any{"name": "B", "sku": "s-1"}), http.StatusConflict)

	rec := caller.do(t, app, http.MethodPost, "/api/v1/catalogue-items", map[string]any{"name": "C", "sku": "S-2"})
	masterDataWantStatus(t, rec, http.StatusCreated)
	path := "/api/v1/catalogue-items/" + masterDataDecode[catalogueItemResponse](t, rec).ID.String()
	masterDataWantStatus(t, caller.do(t, app, http.MethodPut, path, map[string]any{"name": "C", "sku": "S-1"}), http.StatusConflict)
}

func TestCatalogueTimestampsAreUTC(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	caller := newMasterDataCaller(t, app, "acme")

	rec := caller.do(t, app, http.MethodPost, "/api/v1/catalogue-items", map[string]any{"name": "Zone"})
	masterDataWantStatus(t, rec, http.StatusCreated)
	path := "/api/v1/catalogue-items/" + masterDataDecode[catalogueItemResponse](t, rec).ID.String()
	masterDataWantStatus(t, caller.do(t, app, http.MethodDelete, path, nil), http.StatusNoContent)

	stamps := masterDataTimestamps(t, caller.do(t, app, http.MethodGet, path, nil))
	if len(stamps) != 3 {
		t.Fatalf("timestamps = %v, want created_at, updated_at and deleted_at", stamps)
	}
	for key, s := range stamps {
		if !strings.HasSuffix(s, "Z") {
			t.Errorf("%s = %q, want UTC with a Z suffix", key, s)
		}
	}
}

func TestCatalogueItemsOfAnotherTenantAnswer404(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	a := newMasterDataCaller(t, app, "tenant-a")
	b := newMasterDataCaller(t, app, "tenant-b")

	rec := a.do(t, app, http.MethodPost, "/api/v1/catalogue-items", map[string]any{"name": "Secret item"})
	masterDataWantStatus(t, rec, http.StatusCreated)
	id := masterDataDecode[catalogueItemResponse](t, rec).ID
	path := "/api/v1/catalogue-items/" + id.String()

	masterDataWantStatus(t, b.do(t, app, http.MethodGet, path, nil), http.StatusNotFound)
	masterDataWantStatus(t, b.do(t, app, http.MethodPut, path, map[string]any{"name": "Hijacked"}), http.StatusNotFound)
	masterDataWantStatus(t, b.do(t, app, http.MethodDelete, path, nil), http.StatusNotFound)

	rec = b.do(t, app, http.MethodGet, "/api/v1/catalogue-items", nil)
	if list := masterDataDecode[struct {
		Items []catalogueItemResponse `json:"items"`
	}](t, rec); len(list.Items) != 0 {
		t.Errorf("tenant B lists %d items of tenant A", len(list.Items))
	}

	rec = b.do(t, app, http.MethodPost, "/api/v1/catalogue-items", map[string]any{"name": "B's own"})
	masterDataWantStatus(t, rec, http.StatusCreated)
	if own := catalogueRepo(t, app).row(masterDataDecode[catalogueItemResponse](t, rec).ID); own.TenantID != b.tenant.ID {
		t.Errorf("tenant B's item stored under %v", own.TenantID)
	}

	got := catalogueRepo(t, app).row(id)
	if got.Name != "Secret item" || got.DeletedAt != nil {
		t.Errorf("tenant A's row changed: %+v", got)
	}
}
