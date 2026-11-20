package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	Street           *string    `json:"street"`
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

// masterDataErrorBody is the error shape of docs/api.md.
type masterDataErrorBody struct {
	Error struct {
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func masterDataDecode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}

	return v
}

func masterDataWantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()

	if rec.Code != want {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, want, rec.Body.String())
	}
}

// masterDataWantField fails unless rec is a 400 invalid_argument with
// field as a key directly in details, with no wrapper.
func masterDataWantField(t *testing.T, rec *httptest.ResponseRecorder, field string) {
	t.Helper()

	masterDataWantStatus(t, rec, http.StatusBadRequest)
	got := masterDataDecode[masterDataErrorBody](t, rec)
	if got.Error.Code != "invalid_argument" {
		t.Errorf("code = %q, want invalid_argument", got.Error.Code)
	}
	if _, ok := got.Error.Details[field]; !ok {
		t.Errorf("details = %v, want a key %q directly, with no wrapper", got.Error.Details, field)
	}
}

// masterDataTimestamps returns every timestamp string of a JSON object.
func masterDataTimestamps(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()

	raw := masterDataDecode[map[string]any](t, rec)
	out := map[string]string{}
	for _, key := range []string{"created_at", "updated_at", "deleted_at"} {
		if s, ok := raw[key].(string); ok {
			out[key] = s
		}
	}

	return out
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
	masterDataWantStatus(t, rec, http.StatusCreated)
	created := masterDataDecode[customerResponse](t, rec)

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
	if stored := customerRepo(t, app).row(created.ID); stored.TenantID != caller.tenant.ID {
		t.Errorf("stored tenant = %v, want the session tenant %v", stored.TenantID, caller.tenant.ID)
	}

	path := "/api/v1/customers/" + created.ID.String()

	rec = caller.do(t, app, http.MethodPut, path, map[string]any{"name": "Customer GmbH", "document_language": "en"})
	masterDataWantStatus(t, rec, http.StatusOK)
	updated := masterDataDecode[customerResponse](t, rec)
	if updated.Name != "Customer GmbH" || updated.DocumentLanguage != "en" || updated.Number != nil || updated.Email != nil {
		t.Errorf("PUT is not a full replace: %+v", updated)
	}

	rec = caller.do(t, app, http.MethodGet, "/api/v1/customers", nil)
	masterDataWantStatus(t, rec, http.StatusOK)
	if list := masterDataDecode[struct {
		Customers []customerResponse `json:"customers"`
	}](t, rec); len(list.Customers) != 1 {
		t.Fatalf("list has %d customers, want 1", len(list.Customers))
	}

	masterDataWantStatus(t, caller.do(t, app, http.MethodDelete, path, nil), http.StatusNoContent)

	// Soft deleted: gone from the list, still readable by id, not
	// editable, not deletable twice.
	rec = caller.do(t, app, http.MethodGet, "/api/v1/customers", nil)
	if list := masterDataDecode[struct {
		Customers []customerResponse `json:"customers"`
	}](t, rec); len(list.Customers) != 0 {
		t.Errorf("list has %d customers after delete, want 0", len(list.Customers))
	}
	rec = caller.do(t, app, http.MethodGet, path, nil)
	masterDataWantStatus(t, rec, http.StatusOK)
	if got := masterDataDecode[customerResponse](t, rec); got.DeletedAt == nil {
		t.Error("deleted customer has no deleted_at")
	}
	masterDataWantStatus(t, caller.do(t, app, http.MethodPut, path, map[string]any{"name": "Again"}), http.StatusNotFound)
	masterDataWantStatus(t, caller.do(t, app, http.MethodDelete, path, nil), http.StatusNotFound)
}

func TestCustomerValidation(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	caller := newMasterDataCaller(t, app, "acme")

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
			masterDataWantField(t, caller.do(t, app, http.MethodPost, "/api/v1/customers", tc.body), tc.field)
		})
	}

	// The tenant is never taken from the body, and an unknown field is
	// refused.
	for _, field := range []string{"tenant_id", "nonsense"} {
		rec := caller.do(t, app, http.MethodPost, "/api/v1/customers", map[string]any{"name": "X", field: uuid.NewString()})
		masterDataWantStatus(t, rec, http.StatusBadRequest)
	}
}

// TestCustomerBlankOptionalFieldsBecomeNull proves a blank optional
// string never reaches the table's NOT-empty CHECK, which would answer
// 500: it is stored as null, on create and on update.
func TestCustomerBlankOptionalFieldsBecomeNull(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	caller := newMasterDataCaller(t, app, "acme")

	blank := map[string]any{
		"name": "Blank Ltd", "email": "  ", "number": "", "vat_id": " ", "street": "\t",
		"address_line2": "", "postal_code": "  ", "city": "", "country_code": " ",
	}

	rec := caller.do(t, app, http.MethodPost, "/api/v1/customers", blank)
	masterDataWantStatus(t, rec, http.StatusCreated)
	created := masterDataDecode[map[string]any](t, rec)

	path := "/api/v1/customers/" + fmt.Sprint(created["id"])
	rec2 := caller.do(t, app, http.MethodPut, path, blank)
	masterDataWantStatus(t, rec2, http.StatusOK)

	for who, got := range map[string]map[string]any{"create": created, "update": masterDataDecode[map[string]any](t, rec2)} {
		for _, key := range []string{"email", "number", "vat_id", "street", "address_line2", "postal_code", "city", "country_code"} {
			if got[key] != nil {
				t.Errorf("%s: %s = %v, want null", who, key, got[key])
			}
		}
	}
}

func TestCustomerNumberIsUnique(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	caller := newMasterDataCaller(t, app, "acme")

	masterDataWantStatus(t, caller.do(t, app, http.MethodPost, "/api/v1/customers", map[string]any{"name": "A", "number": "K-1"}), http.StatusCreated)
	masterDataWantStatus(t, caller.do(t, app, http.MethodPost, "/api/v1/customers", map[string]any{"name": "B", "number": "k-1"}), http.StatusConflict)

	rec := caller.do(t, app, http.MethodPost, "/api/v1/customers", map[string]any{"name": "C", "number": "K-2"})
	masterDataWantStatus(t, rec, http.StatusCreated)
	path := "/api/v1/customers/" + masterDataDecode[customerResponse](t, rec).ID.String()

	masterDataWantStatus(t, caller.do(t, app, http.MethodPut, path, map[string]any{"name": "C", "number": "K-1"}), http.StatusConflict)
	// Keeping its own number is no conflict.
	masterDataWantStatus(t, caller.do(t, app, http.MethodPut, path, map[string]any{"name": "C2", "number": "K-2"}), http.StatusOK)
}

// TestTimestampsAreUTC feeds a repository that answers in another zone
// and expects RFC 3339 in UTC, as docs/api.md requires.
func TestTimestampsAreUTC(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	caller := newMasterDataCaller(t, app, "acme")

	rec := caller.do(t, app, http.MethodPost, "/api/v1/customers", map[string]any{"name": "Zone"})
	masterDataWantStatus(t, rec, http.StatusCreated)
	path := "/api/v1/customers/" + masterDataDecode[customerResponse](t, rec).ID.String()
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

// TestCustomersOfAnotherTenantAnswer404 is the cross tenant test: tenant
// B must neither read nor change tenant A's customer, and A's row must be
// untouched afterwards.
func TestCustomersOfAnotherTenantAnswer404(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 100)
	a := newMasterDataCaller(t, app, "tenant-a")
	b := newMasterDataCaller(t, app, "tenant-b")

	rec := a.do(t, app, http.MethodPost, "/api/v1/customers", map[string]any{"name": "Secret Ltd"})
	masterDataWantStatus(t, rec, http.StatusCreated)
	id := masterDataDecode[customerResponse](t, rec).ID
	path := "/api/v1/customers/" + id.String()

	masterDataWantStatus(t, b.do(t, app, http.MethodGet, path, nil), http.StatusNotFound)
	masterDataWantStatus(t, b.do(t, app, http.MethodPut, path, map[string]any{"name": "Hijacked"}), http.StatusNotFound)
	masterDataWantStatus(t, b.do(t, app, http.MethodDelete, path, nil), http.StatusNotFound)

	rec = b.do(t, app, http.MethodGet, "/api/v1/customers", nil)
	masterDataWantStatus(t, rec, http.StatusOK)
	if list := masterDataDecode[struct {
		Customers []customerResponse `json:"customers"`
	}](t, rec); len(list.Customers) != 0 {
		t.Errorf("tenant B lists %d customers of tenant A", len(list.Customers))
	}

	// Tenant B's own create lands in tenant B.
	rec = b.do(t, app, http.MethodPost, "/api/v1/customers", map[string]any{"name": "B's own"})
	masterDataWantStatus(t, rec, http.StatusCreated)
	if own := customerRepo(t, app).row(masterDataDecode[customerResponse](t, rec).ID); own.TenantID != b.tenant.ID {
		t.Errorf("tenant B's customer stored under %v", own.TenantID)
	}

	got := customerRepo(t, app).row(id)
	if got.Name != "Secret Ltd" || got.DeletedAt != nil {
		t.Errorf("tenant A's row changed: %+v", got)
	}
}
