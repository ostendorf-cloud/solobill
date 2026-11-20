package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// customerResponse is the customer JSON of docs/api.md.
type customerResponse struct {
	ID               uuid.UUID  `json:"id"`
	Name             string     `json:"name"`
	Number           *string    `json:"number"`
	Email            *string    `json:"email"`
	AddressLine2     *string    `json:"address_line2"`
	CountryCode      *string    `json:"country_code"`
	DocumentLanguage string     `json:"document_language"`
	DeletedAt        *time.Time `json:"deleted_at"`
}

// masterDataCaller is one logged in owner of its own tenant.
type masterDataCaller struct {
	tenant storage.Tenant
	cookie *http.Cookie
	csrf   string
}

// newMasterDataCaller creates a tenant with one owner and logs in.
func newMasterDataCaller(t *testing.T, app testApp, slug string) masterDataCaller {
	t.Helper()

	tenant := app.store.addTenant(storage.Tenant{Slug: slug, Name: slug})
	hash := hashPassword(t, ownerPassword)
	secret := totpSecret(t)
	// One address per tenant: the fake store looks accounts up by email
	// alone.
	email := slug + "@acme.test"
	app.store.addAccount(storage.Account{
		TenantID:     tenant.ID,
		Email:        email,
		PasswordHash: &hash,
		TOTPSecret:   &secret,
		Role:         storage.AccountRoleOwner,
		Status:       storage.AccountStatusActive,
	})
	cookie, token := loginAndCSRF(t, app, slug, email, ownerPassword, secret)

	return masterDataCaller{tenant: tenant, cookie: cookie, csrf: token}
}

func (c masterDataCaller) do(t *testing.T, app testApp, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	token := ""
	if method != http.MethodGet {
		token = c.csrf
	}

	return doJSON(t, app.handler, method, path, body, []*http.Cookie{c.cookie}, token)
}

// errorBody is the error shape of docs/api.md.
type errorBody struct {
	Error struct {
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}

	return v
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()

	if rec.Code != want {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, want, rec.Body.String())
	}
}

func customerRepo(t *testing.T, app testApp) *fakeCustomerRepository {
	t.Helper()

	repo, ok := app.repos.Customers.(*fakeCustomerRepository)
	if !ok {
		t.Fatalf("Customers = %T, want the fake", app.repos.Customers)
	}

	return repo
}

func TestCustomersRequireASession(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/customers"},
		{http.MethodPost, "/api/v1/customers"},
		{http.MethodGet, "/api/v1/customers/" + uuid.NewString()},
		{http.MethodPut, "/api/v1/customers/" + uuid.NewString()},
		{http.MethodDelete, "/api/v1/customers/" + uuid.NewString()},
	} {
		rec := doJSON(t, app.handler, tc.method, tc.path, nil, nil, "")
		if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without a session = %d, want 401 or 403", tc.method, tc.path, rec.Code)
		}
	}
}

func TestCustomerLifecycle(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	caller := newMasterDataCaller(t, app, "acme")
	customerRepo(t, app).actAs(caller.tenant.ID)

	rec := caller.do(t, app, http.MethodPost, "/api/v1/customers", map[string]any{
		"name":          " Customer Ltd ",
		"number":        "K-0042",
		"email":         "billing@customer.test",
		"street":        "Side Street 2",
		"address_line2": "Floor 3",
		"postal_code":   "54321",
		"city":          "Other City",
		"country_code":  "de",
	})
	wantStatus(t, rec, http.StatusCreated)
	created := decodeBody[customerResponse](t, rec)

	if created.Name != "Customer Ltd" {
		t.Errorf("name = %q, want it trimmed", created.Name)
	}
	if created.DocumentLanguage != "de" {
		t.Errorf("document_language = %q, want the default de", created.DocumentLanguage)
	}
	if created.CountryCode == nil || *created.CountryCode != "DE" {
		t.Errorf("country_code = %v, want DE", created.CountryCode)
	}
	if created.AddressLine2 == nil || *created.AddressLine2 != "Floor 3" || created.Number == nil || *created.Number != "K-0042" {
		t.Errorf("address_line2 and number not stored: %+v", created)
	}
	stored := customerRepo(t, app).row(created.ID)
	if stored.TenantID != caller.tenant.ID {
		t.Errorf("stored tenant = %v, want the session tenant %v", stored.TenantID, caller.tenant.ID)
	}

	path := "/api/v1/customers/" + created.ID.String()

	rec = caller.do(t, app, http.MethodPut, path, map[string]any{"name": "Customer GmbH", "document_language": "en"})
	wantStatus(t, rec, http.StatusOK)
	updated := decodeBody[customerResponse](t, rec)
	if updated.Name != "Customer GmbH" || updated.DocumentLanguage != "en" || updated.Number != nil || updated.Email != nil {
		t.Errorf("PUT is not a full replace: %+v", updated)
	}

	rec = caller.do(t, app, http.MethodGet, "/api/v1/customers", nil)
	wantStatus(t, rec, http.StatusOK)
	if list := decodeBody[struct {
		Customers []customerResponse `json:"customers"`
	}](t, rec); len(list.Customers) != 1 {
		t.Fatalf("list has %d customers, want 1", len(list.Customers))
	}

	wantStatus(t, caller.do(t, app, http.MethodDelete, path, nil), http.StatusNoContent)

	// Soft deleted: gone from the list, still readable by id, not
	// editable, not deletable twice.
	rec = caller.do(t, app, http.MethodGet, "/api/v1/customers", nil)
	if list := decodeBody[struct {
		Customers []customerResponse `json:"customers"`
	}](t, rec); len(list.Customers) != 0 {
		t.Errorf("list has %d customers after delete, want 0", len(list.Customers))
	}
	rec = caller.do(t, app, http.MethodGet, path, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := decodeBody[customerResponse](t, rec); got.DeletedAt == nil {
		t.Error("deleted customer has no deleted_at")
	}
	wantStatus(t, caller.do(t, app, http.MethodPut, path, map[string]any{"name": "Again"}), http.StatusNotFound)
	wantStatus(t, caller.do(t, app, http.MethodDelete, path, nil), http.StatusNotFound)
}

func TestCustomerValidation(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	caller := newMasterDataCaller(t, app, "acme")
	customerRepo(t, app).actAs(caller.tenant.ID)

	for _, tc := range []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"missing name", map[string]any{"email": "a@b.test"}, "name"},
		{"blank name", map[string]any{"name": "  "}, "name"},
		{"bad language", map[string]any{"name": "X", "document_language": "fr"}, "document_language"},
		{"bad country", map[string]any{"name": "X", "country_code": "DEU"}, "country_code"},
		{"wrong type", map[string]any{"name": 5}, "name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := caller.do(t, app, http.MethodPost, "/api/v1/customers", tc.body)
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

	// The tenant is never taken from the body.
	rec := caller.do(t, app, http.MethodPost, "/api/v1/customers", map[string]any{"name": "X", "tenant_id": uuid.NewString()})
	wantStatus(t, rec, http.StatusBadRequest)
}

func TestCustomerNumberIsUnique(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	caller := newMasterDataCaller(t, app, "acme")
	customerRepo(t, app).actAs(caller.tenant.ID)

	wantStatus(t, caller.do(t, app, http.MethodPost, "/api/v1/customers", map[string]any{"name": "A", "number": "K-1"}), http.StatusCreated)
	wantStatus(t, caller.do(t, app, http.MethodPost, "/api/v1/customers", map[string]any{"name": "B", "number": "k-1"}), http.StatusConflict)
}

// TestCustomersOfAnotherTenantAnswer404 is the cross tenant test: tenant
// B must neither read nor change tenant A's customer, and A's row must be
// untouched afterwards.
func TestCustomersOfAnotherTenantAnswer404(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	a := newMasterDataCaller(t, app, "tenant-a")
	b := newMasterDataCaller(t, app, "tenant-b")
	repo := customerRepo(t, app)

	repo.actAs(a.tenant.ID)
	rec := a.do(t, app, http.MethodPost, "/api/v1/customers", map[string]any{"name": "Secret Ltd"})
	wantStatus(t, rec, http.StatusCreated)
	id := decodeBody[customerResponse](t, rec).ID
	path := "/api/v1/customers/" + id.String()

	repo.actAs(b.tenant.ID)
	wantStatus(t, b.do(t, app, http.MethodGet, path, nil), http.StatusNotFound)
	wantStatus(t, b.do(t, app, http.MethodPut, path, map[string]any{"name": "Hijacked"}), http.StatusNotFound)
	wantStatus(t, b.do(t, app, http.MethodDelete, path, nil), http.StatusNotFound)

	rec = b.do(t, app, http.MethodGet, "/api/v1/customers", nil)
	wantStatus(t, rec, http.StatusOK)
	if list := decodeBody[struct {
		Customers []customerResponse `json:"customers"`
	}](t, rec); len(list.Customers) != 0 {
		t.Errorf("tenant B lists %d customers of tenant A", len(list.Customers))
	}

	got := repo.row(id)
	if got.Name != "Secret Ltd" || got.DeletedAt != nil {
		t.Errorf("tenant A's row changed: %+v", got)
	}
}
