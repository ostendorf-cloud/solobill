package httpapi_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/docissue"
	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

const documentsPath = "/api/v1/documents"

// jsonObject is a decoded JSON object with checked accessors.
type jsonObject map[string]any

func (o jsonObject) str(t *testing.T, key string) string {
	t.Helper()

	s, ok := o[key].(string)
	if !ok {
		t.Fatalf("%s = %v, want a string in %v", key, o[key], o)
	}

	return s
}

func (o jsonObject) totals(t *testing.T) jsonObject {
	t.Helper()

	m, ok := o["totals"].(map[string]any)
	if !ok {
		t.Fatalf("totals = %v, want an object", o["totals"])
	}

	return m
}

func (o jsonObject) list(t *testing.T, key string) []jsonObject {
	t.Helper()

	raw, ok := o[key].([]any)
	if !ok {
		t.Fatalf("%s = %v, want a list", key, o[key])
	}
	out := make([]jsonObject, len(raw))
	for i, v := range raw {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("%s[%d] = %v, want an object", key, i, v)
		}
		out[i] = m
	}

	return out
}

func decodeObject(t *testing.T, rec *httptest.ResponseRecorder) jsonObject {
	t.Helper()

	return masterDataDecode[map[string]any](t, rec)
}

// docFixture is one tenant with an owner, a customer, a complete
// profile and a period covering today.
type docFixture struct {
	app      testApp
	tenant   storage.Tenant
	owner    profileSession
	customer string
}

// newDocFixture is a Kleinunternehmer tenant, the owner's own regime.
func newDocFixture(t *testing.T, slug string) docFixture {
	t.Helper()

	return addDocTenant(t, buildTestApp(t, 1000), slug)
}

// addDocTenant adds a Kleinunternehmer tenant with an owner and a
// customer to app, and sets the profile and the period. The profile and
// period fakes hold one tenant's rows; the last call wins.
func addDocTenant(t *testing.T, app testApp, slug string) docFixture {
	t.Helper()

	tenant := app.store.addTenant(storage.Tenant{Slug: slug, Name: slug})
	f := docFixture{
		app:    app,
		tenant: tenant,
		owner:  profileLogin(t, app, tenant, "owner@"+slug+".test", storage.AccountRoleOwner),
	}
	f.addCustomer(t)
	f.setProfile(t, true)
	f.setPeriod(t, "2000-01-01")

	return f
}

func (f *docFixture) addCustomer(t *testing.T) {
	t.Helper()

	rec := f.do(t, f.owner, http.MethodPost, "/api/v1/customers", map[string]any{
		"name": "Customer Ltd", "street": "Side Street 2", "postal_code": "54321",
		"city": "Other City", "country_code": "DE", "document_language": "en",
	})
	masterDataWantStatus(t, rec, http.StatusCreated)
	f.customer = decodeObject(t, rec).str(t, "id")
}

func (f docFixture) setProfile(t *testing.T, complete bool) {
	t.Helper()

	p := storage.SellerProfile{
		TenantID:         f.tenant.ID,
		LegalName:        "Example Trading",
		Street:           "Main Street 1",
		PostalCode:       "12345",
		City:             "Example City",
		CountryCode:      "DE",
		DefaultVATRegime: storage.VATRegimeKleinunternehmer,
		DefaultLanguage:  storage.DocumentLanguageGerman,
	}
	if complete {
		tax := "12/345/67890"
		p.TaxNumber = &tax
	}
	profiles, ok := f.app.repos.SellerProfiles.(*fakeSellerProfiles)
	if !ok {
		t.Fatalf("SellerProfiles = %T, want the fake", f.app.repos.SellerProfiles)
	}
	if _, err := profiles.Put(context.Background(), p); err != nil {
		t.Fatalf("put profile: %v", err)
	}
}

func (f docFixture) setPeriod(t *testing.T, validFrom string) {
	t.Helper()

	periods, ok := f.app.repos.VATRegimePeriods.(*fakeVATRegimes)
	if !ok {
		t.Fatalf("VATRegimePeriods = %T, want the fake", f.app.repos.VATRegimePeriods)
	}
	from, err := time.Parse(docpayload.DateLayout, validFrom)
	if err != nil {
		t.Fatalf("parse %q: %v", validFrom, err)
	}
	_, err = periods.Create(context.Background(), storage.VATRegimePeriod{
		TenantID: f.tenant.ID, ValidFrom: from, Regime: storage.VATRegimeKleinunternehmer,
	})
	// The fake holds one tenant's periods: a second tenant of the same
	// app reuses the first one's period.
	if err != nil && !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("create period: %v", err)
	}
}

func (f docFixture) member(t *testing.T) profileSession {
	t.Helper()

	return profileLogin(t, f.app, f.tenant, "member@"+f.tenant.Slug+".test", storage.AccountRoleMember)
}

func (f docFixture) do(t *testing.T, s profileSession, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	return doJSON(t, f.app.handler, method, path, body, s.cookies, s.token)
}

// invoiceBody is a valid draft body with one line at rate.
func (f docFixture) invoiceBody(rate int) map[string]any {
	return map[string]any{
		"type":         "invoice",
		"customer_id":  f.customer,
		"service_date": "2026-11-01",
		"lines": []map[string]any{{
			"description": "Consulting", "quantity_thousandths": 2500, "unit": "hour",
			"unit_price_cents": 9500, "vat_rate_permyriad": rate,
		}},
	}
}

// kuBody is invoiceBody under the Kleinunternehmer regime: rate 0.
func (f docFixture) kuBody() map[string]any { return f.invoiceBody(0) }

func (f docFixture) create(t *testing.T, s profileSession, body map[string]any) jsonObject {
	t.Helper()

	rec := f.do(t, s, http.MethodPost, documentsPath, body)
	masterDataWantStatus(t, rec, http.StatusCreated)

	return decodeObject(t, rec)
}

func (f docFixture) createID(t *testing.T) string {
	t.Helper()

	return f.create(t, f.owner, f.kuBody()).str(t, "id")
}

func (f docFixture) issue(t *testing.T, id string) *httptest.ResponseRecorder {
	t.Helper()

	return f.do(t, f.owner, http.MethodPost, documentsPath+"/"+id+"/issue", nil)
}

func (f docFixture) mustIssue(t *testing.T, id string) jsonObject {
	t.Helper()

	rec := f.issue(t, id)
	masterDataWantStatus(t, rec, http.StatusOK)

	return decodeObject(t, rec)
}

func (f docFixture) counter(t *testing.T, docType storage.DocumentType) int64 {
	t.Helper()

	return documentStore(t, f.app).Counter(f.tenant.ID, docType, thisYear())
}

func wantErrorCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) masterDataErrorBody {
	t.Helper()

	masterDataWantStatus(t, rec, status)
	body := masterDataDecode[masterDataErrorBody](t, rec)
	if body.Error.Code != code {
		t.Fatalf("code = %q, want %q, body = %s", body.Error.Code, code, rec.Body.String())
	}

	return body
}

func thisYear() int { return docissue.IssueDate(time.Now()).Year() }

func TestDocumentsRequireASession(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	id := uuid.NewString()
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, documentsPath},
		{http.MethodPost, documentsPath},
		{http.MethodPost, documentsPath + "/preview"},
		{http.MethodGet, documentsPath + "/" + id},
		{http.MethodPut, documentsPath + "/" + id},
		{http.MethodDelete, documentsPath + "/" + id},
		{http.MethodPost, documentsPath + "/" + id + "/issue"},
		{http.MethodPost, documentsPath + "/" + id + "/correction"},
		{http.MethodPost, "/api/v1/offers/" + id + "/to-invoice"},
	} {
		if rec := doJSON(t, app.handler, route.method, route.path, nil, nil, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s status = %d, want 401", route.method, route.path, rec.Code)
		}
	}
}

func TestDocumentsIssueAndCorrectionAreOwnerOnly(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "roles")
	member := f.member(t)

	// A member drafts, edits, previews and lists.
	id := f.create(t, member, f.kuBody()).str(t, "id")
	masterDataWantStatus(t, f.do(t, member, http.MethodPut, documentsPath+"/"+id, f.kuBody()), http.StatusOK)
	masterDataWantStatus(t, f.do(t, member, http.MethodPost, documentsPath+"/preview", f.kuBody()), http.StatusOK)
	masterDataWantStatus(t, f.do(t, member, http.MethodGet, documentsPath, nil), http.StatusOK)

	// A member never issues and never creates a correction.
	wantErrorCode(t, f.do(t, member, http.MethodPost, documentsPath+"/"+id+"/issue", nil), http.StatusForbidden, "permission_denied")
	if got := f.counter(t, storage.DocumentTypeInvoice); got != 0 {
		t.Fatalf("counter = %d after a member's issue, want 0", got)
	}
	if stored, _ := documentStore(t, f.app).Get(uuid.MustParse(id)); stored.State != storage.DocumentStateDraft {
		t.Fatalf("state = %q after a member's issue, want draft", stored.State)
	}

	if issued := f.mustIssue(t, id); issued.str(t, "state") != "issued" {
		t.Errorf("state = %v, want issued", issued["state"])
	}
	wantErrorCode(t, f.do(t, member, http.MethodPost, documentsPath+"/"+id+"/correction", nil), http.StatusForbidden, "permission_denied")
	masterDataWantStatus(t, f.do(t, f.owner, http.MethodPost, documentsPath+"/"+id+"/correction", nil), http.StatusCreated)
}

func TestDocumentsAreScopedToTheSessionTenant(t *testing.T) {
	t.Parallel()

	a := newDocFixture(t, "tenant-a")
	id := a.createID(t)
	issuedID := a.createID(t)
	a.mustIssue(t, issuedID)
	b := addDocTenant(t, a.app, "tenant-b")

	for _, route := range []struct{ method, path string }{
		{http.MethodGet, documentsPath + "/" + id},
		{http.MethodPut, documentsPath + "/" + id},
		{http.MethodDelete, documentsPath + "/" + id},
		{http.MethodPost, documentsPath + "/" + id + "/issue"},
		{http.MethodPost, documentsPath + "/" + issuedID + "/correction"},
		{http.MethodPost, "/api/v1/offers/" + id + "/to-invoice"},
	} {
		var body any
		if route.method == http.MethodPut {
			body = b.kuBody()
		}
		wantErrorCode(t, b.do(t, b.owner, route.method, route.path, body), http.StatusNotFound, "not_found")
	}

	rec := b.do(t, b.owner, http.MethodGet, documentsPath, nil)
	masterDataWantStatus(t, rec, http.StatusOK)
	if docs := masterDataDecode[map[string][]any](t, rec)["documents"]; len(docs) != 0 {
		t.Fatalf("tenant B lists %v, want nothing of tenant A", docs)
	}

	// A draft of B can never name A's customer.
	body := b.kuBody()
	body["customer_id"] = a.customer
	masterDataWantField(t, b.do(t, b.owner, http.MethodPost, documentsPath, body), "customer_id")

	stored, _ := documentStore(t, a.app).Get(uuid.MustParse(id))
	if stored.TenantID != a.tenant.ID || stored.State != storage.DocumentStateDraft || stored.CustomerID.String() != a.customer {
		t.Fatalf("A's draft = %+v, want it untouched", stored)
	}
}

func TestDocumentBodyNeverCarriesTheTenant(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "body")
	body := f.kuBody()
	body["tenant_id"] = uuid.NewString()
	masterDataWantStatus(t, f.do(t, f.owner, http.MethodPost, documentsPath, body), http.StatusBadRequest)

	stored, _ := documentStore(t, f.app).Get(uuid.MustParse(f.createID(t)))
	if stored.TenantID != f.tenant.ID {
		t.Fatalf("tenant = %s, want the session's %s", stored.TenantID, f.tenant.ID)
	}
}

func TestDraftIsEditableAndAnIssuedDocumentIsNot(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "lifecycle")
	draft := f.create(t, f.owner, f.kuBody())
	id := draft.str(t, "id")

	// Defaults: the regime of today's period, the customer's language.
	if draft.str(t, "vat_regime") != "kleinunternehmer" || draft.str(t, "language") != "en" ||
		draft.str(t, "state") != "draft" || draft["number"] != nil {
		t.Fatalf("draft = %v", draft)
	}
	if totals := draft.totals(t); totals["grand_total_cents"] != float64(23750) || totals["vat_total_cents"] != float64(0) {
		t.Fatalf("totals = %v, want 23750 and no VAT", totals)
	}

	edit := f.kuBody()
	edit["notes"] = "Thank you"
	edit["lines"] = []map[string]any{{"description": "Workshop", "quantity_thousandths": 1000, "unit": "day", "unit_price_cents": 80000}}
	rec := f.do(t, f.owner, http.MethodPut, documentsPath+"/"+id, edit)
	masterDataWantStatus(t, rec, http.StatusOK)
	edited := decodeObject(t, rec)
	if edited.str(t, "notes") != "Thank you" || edited.totals(t)["grand_total_cents"] != float64(80000) {
		t.Fatalf("edited = %v", edited)
	}

	issued := f.mustIssue(t, id)
	wantNumber := fmt.Sprintf("RE-%d0001", thisYear())
	if issued.str(t, "number") != wantNumber || issued["number_value"] != float64(1) || issued.str(t, "chain_status") != "effective" {
		t.Fatalf("issued = %v, want number %s and chain_status effective", issued, wantNumber)
	}
	before, _ := documentStore(t, f.app).Get(uuid.MustParse(id))

	wantErrorCode(t, f.do(t, f.owner, http.MethodPut, documentsPath+"/"+id, edit), http.StatusConflict, "conflict")
	wantErrorCode(t, f.do(t, f.owner, http.MethodDelete, documentsPath+"/"+id, nil), http.StatusConflict, "conflict")
	wantErrorCode(t, f.issue(t, id), http.StatusConflict, "conflict")

	after, _ := documentStore(t, f.app).Get(uuid.MustParse(id))
	if string(after.FrozenPayload) != string(before.FrozenPayload) || after.UpdatedAt != before.UpdatedAt {
		t.Fatalf("the issued document changed")
	}
	if got := f.counter(t, storage.DocumentTypeInvoice); got != 1 {
		t.Errorf("counter = %d, want 1", got)
	}

	rec = f.do(t, f.owner, http.MethodGet, documentsPath+"/"+id, nil)
	masterDataWantStatus(t, rec, http.StatusOK)
	read := decodeObject(t, rec)
	if read.str(t, "number") != wantNumber || read["frozen_payload_sha256"] == nil || len(read.list(t, "lines")) != 1 {
		t.Fatalf("read = %v", read)
	}

	other := f.createID(t)
	masterDataWantStatus(t, f.do(t, f.owner, http.MethodDelete, documentsPath+"/"+other, nil), http.StatusNoContent)
	wantErrorCode(t, f.do(t, f.owner, http.MethodGet, documentsPath+"/"+other, nil), http.StatusNotFound, "not_found")
}

func TestIssueTakesNoNumberAndNoDateFromTheCaller(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "empty-body")
	id := f.createID(t)

	for _, body := range []map[string]any{{"number": 1}, {"issue_date": "2020-01-01"}, {"number_year": 2020}} {
		masterDataWantStatus(t, f.do(t, f.owner, http.MethodPost, documentsPath+"/"+id+"/issue", body), http.StatusBadRequest)
	}
	if got := f.counter(t, storage.DocumentTypeInvoice); got != 0 {
		t.Fatalf("counter = %d, want 0", got)
	}
	masterDataWantStatus(t, f.do(t, f.owner, http.MethodPost, documentsPath+"/"+id+"/issue", map[string]any{}), http.StatusOK)
}

func TestIssueRefusals(t *testing.T) {
	t.Parallel()

	t.Run("no period covers today", func(t *testing.T) {
		t.Parallel()

		app := buildTestApp(t, 1000)
		tenant := app.store.addTenant(storage.Tenant{Slug: "no-period", Name: "no-period"})
		f := docFixture{app: app, tenant: tenant, owner: profileLogin(t, app, tenant, "owner@no-period.test", storage.AccountRoleOwner)}
		f.addCustomer(t)
		f.setProfile(t, true)
		f.setPeriod(t, fmt.Sprintf("%d-01-01", thisYear()+1))
		body := f.kuBody()
		body["vat_regime"] = "kleinunternehmer"
		id := f.create(t, f.owner, body).str(t, "id")

		wantErrorCode(t, f.issue(t, id), http.StatusBadRequest, "vat_regime_missing")
	})

	t.Run("the draft's regime differs from the period", func(t *testing.T) {
		t.Parallel()

		f := newDocFixture(t, "mismatch")
		body := f.invoiceBody(1900)
		body["vat_regime"] = "standard"
		id := f.create(t, f.owner, body).str(t, "id")

		got := wantErrorCode(t, f.issue(t, id), http.StatusConflict, "vat_regime_mismatch")
		if got.Error.Details["vat_regime"] != "kleinunternehmer" {
			t.Errorf("details = %v, want vat_regime kleinunternehmer", got.Error.Details)
		}
	})

	t.Run("an incomplete seller profile", func(t *testing.T) {
		t.Parallel()

		f := newDocFixture(t, "incomplete")
		f.setProfile(t, false)
		id := f.createID(t)

		got := wantErrorCode(t, f.issue(t, id), http.StatusBadRequest, "invalid_argument")
		if _, ok := got.Error.Details["seller.tax_number"]; !ok {
			t.Errorf("details = %v, want seller.tax_number directly in details", got.Error.Details)
		}
		if got := f.counter(t, storage.DocumentTypeInvoice); got != 0 {
			t.Errorf("counter = %d, want 0", got)
		}
	})

	t.Run("an unknown document", func(t *testing.T) {
		t.Parallel()

		f := newDocFixture(t, "unknown")
		wantErrorCode(t, f.issue(t, uuid.NewString()), http.StatusNotFound, "not_found")
	})
}

func TestDraftValidation(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "validation")
	firstLine := func(b map[string]any) map[string]any {
		lines, _ := b["lines"].([]map[string]any)

		return lines[0]
	}
	tests := []struct {
		name   string
		mutate func(b map[string]any)
		field  string
	}{
		{"a correction is never created here", func(b map[string]any) { b["type"] = "correction" }, "type"},
		{"an unknown customer", func(b map[string]any) { b["customer_id"] = uuid.NewString() }, "customer_id"},
		{"an offer has no service period", func(b map[string]any) { b["type"] = "offer" }, "service_date"},
		{"only an offer has a validity date", func(b map[string]any) { b["valid_until"] = "2026-12-31" }, "valid_until"},
		{"a vat rate under the kleinunternehmer regime", func(b map[string]any) {
			firstLine(b)["vat_rate_permyriad"] = 1900
		}, "lines[0].vat_rate_permyriad"},
		{"a description the renderer cannot print", func(b map[string]any) {
			firstLine(b)["description"] = strings.Repeat("a", docpayload.MaxTextChars+1)
		}, "lines[0].description"},
		{"notes with a control character", func(b map[string]any) { b["notes"] = "a\x07b" }, "notes"},
		{"a reference over the line limit", func(b map[string]any) {
			b["customer_reference"] = strings.Repeat("r", docpayload.MaxLineChars+1)
		}, "customer_reference"},
		{"both discounts", func(b map[string]any) {
			b["discount_percent_permyriad"] = 100
			b["discount_amount_cents"] = 100
		}, "discount_amount_cents"},
		{"an unknown unit", func(b map[string]any) { firstLine(b)["unit"] = "litre" }, "lines[0].unit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := f.kuBody()
			tt.mutate(body)
			masterDataWantField(t, f.do(t, f.owner, http.MethodPost, documentsPath, body), tt.field)
		})
	}
}

func TestPreviewComputesBothRegimes(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "preview")

	ku := f.kuBody()
	ku["vat_regime"] = "kleinunternehmer"
	rec := f.do(t, f.owner, http.MethodPost, documentsPath+"/preview", ku)
	masterDataWantStatus(t, rec, http.StatusOK)
	if totals := decodeObject(t, rec).totals(t); totals["grand_total_cents"] != float64(23750) || totals["vat_total_cents"] != float64(0) {
		t.Errorf("kleinunternehmer totals = %v", totals)
	}

	standard := map[string]any{
		"type": "invoice", "vat_regime": "standard", "discount_percent_permyriad": 1500,
		"lines": []map[string]any{
			{"description": "Consulting", "quantity_thousandths": 1000, "unit": "piece", "unit_price_cents": 12004, "vat_rate_permyriad": 1900},
			{"description": "Support", "quantity_thousandths": 1000, "unit": "piece", "unit_price_cents": 7004, "vat_rate_permyriad": 1900},
		},
	}
	rec = f.do(t, f.owner, http.MethodPost, documentsPath+"/preview", standard)
	masterDataWantStatus(t, rec, http.StatusOK)
	got := decodeObject(t, rec)
	totals := got.totals(t)
	if totals["net_after_document_discount_cents"] != float64(16157) || totals["vat_total_cents"] != float64(3070) || totals["grand_total_cents"] != float64(19227) {
		t.Errorf("standard totals = %v, want the worked example 161.57, 30.70, 192.27", totals)
	}
	if lines := got.list(t, "lines"); lines[0]["document_discount_share_cents"] != float64(1801) {
		t.Errorf("lines = %v, want a share of 1801 on the first line", lines)
	}

	bad := f.invoiceBody(1900)
	bad["vat_regime"] = "kleinunternehmer"
	masterDataWantField(t, f.do(t, f.owner, http.MethodPost, documentsPath+"/preview", bad), "lines[0].vat_rate_permyriad")

	rec = f.do(t, f.owner, http.MethodGet, documentsPath, nil)
	if docs := masterDataDecode[map[string][]any](t, rec)["documents"]; len(docs) != 0 {
		t.Errorf("a preview stored %v", docs)
	}
}

func TestCorrectionsThroughTheAPI(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "corrections")
	original := f.createID(t)
	wantErrorCode(t, f.do(t, f.owner, http.MethodPost, documentsPath+"/"+original+"/correction", nil), http.StatusConflict, "conflict")
	f.mustIssue(t, original)
	before, _ := documentStore(t, f.app).Get(uuid.MustParse(original))

	newCorrection := func(from string) jsonObject {
		rec := f.do(t, f.owner, http.MethodPost, documentsPath+"/"+from+"/correction", nil)
		masterDataWantStatus(t, rec, http.StatusCreated)

		return decodeObject(t, rec)
	}

	first := newCorrection(original)
	if first.str(t, "type") != "correction" || first.str(t, "corrects_document_id") != original ||
		first.str(t, "correction_base_id") != original || first["correction_base_seq"] != float64(0) ||
		first["number"] != nil || first["correction_seq"] != nil {
		t.Fatalf("correction draft = %v", first)
	}
	if lines := first.list(t, "lines"); len(lines) != 1 || lines[0]["description"] != "Consulting" {
		t.Fatalf("lines = %v, want the original's lines", lines)
	}
	stale := newCorrection(original)

	issued := f.mustIssue(t, first.str(t, "id"))
	wantNumber := fmt.Sprintf("RE-%d0001-1", thisYear())
	if issued.str(t, "number") != wantNumber || issued["correction_seq"] != float64(1) || issued.str(t, "chain_status") != "effective" {
		t.Fatalf("issued correction = %v, want %s", issued, wantNumber)
	}

	// The stale draft copied the original, correction 1 was issued since.
	got := wantErrorCode(t, f.issue(t, stale.str(t, "id")), http.StatusConflict, "correction_base_changed")
	if got.Error.Details["correction_base_seq"] != "1" {
		t.Errorf("details = %v, want correction_base_seq \"1\"", got.Error.Details)
	}

	// A correction of the correction still names the root.
	second := newCorrection(first.str(t, "id"))
	if second.str(t, "corrects_document_id") != original || second.str(t, "correction_base_id") != first.str(t, "id") ||
		second["correction_base_seq"] != float64(1) {
		t.Fatalf("second draft = %v", second)
	}

	after, _ := documentStore(t, f.app).Get(uuid.MustParse(original))
	if string(after.FrozenPayload) != string(before.FrozenPayload) || after.UpdatedAt != before.UpdatedAt {
		t.Errorf("the original changed")
	}

	rec := f.do(t, f.owner, http.MethodGet, documentsPath+"/"+original, nil)
	if status := decodeObject(t, rec).str(t, "chain_status"); status != "superseded" {
		t.Errorf("original chain_status = %v, want superseded", status)
	}
}

func TestOfferToInvoice(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "offers")
	body := f.kuBody()
	body["type"] = "offer"
	delete(body, "service_date")
	body["valid_until"] = "2026-12-31"
	offerID := f.create(t, f.owner, body).str(t, "id")

	rec := f.do(t, f.member(t), http.MethodPost, "/api/v1/offers/"+offerID+"/to-invoice", nil)
	masterDataWantStatus(t, rec, http.StatusCreated)
	invoice := decodeObject(t, rec)
	if invoice.str(t, "type") != "invoice" || invoice.str(t, "source_offer_id") != offerID || invoice["valid_until"] != nil ||
		len(invoice.list(t, "lines")) != 1 {
		t.Fatalf("invoice = %v", invoice)
	}

	wantErrorCode(t, f.do(t, f.owner, http.MethodPost, "/api/v1/offers/"+invoice.str(t, "id")+"/to-invoice", nil), http.StatusConflict, "conflict")
	// The offer is referenced now and stays.
	wantErrorCode(t, f.do(t, f.owner, http.MethodDelete, documentsPath+"/"+offerID, nil), http.StatusConflict, "conflict")

	issuedOffer := f.mustIssue(t, offerID)
	if issuedOffer.str(t, "number") != fmt.Sprintf("AN-%d0001", thisYear()) || issuedOffer["chain_status"] != nil {
		t.Errorf("issued offer = %v", issuedOffer)
	}
	if got := f.counter(t, storage.DocumentTypeOffer); got != 1 {
		t.Errorf("offer counter = %d, want 1", got)
	}
}

func TestListFiltersAndDerivesTheChainStatus(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "list")
	original := f.createID(t)
	f.mustIssue(t, original)
	correctionRec := f.do(t, f.owner, http.MethodPost, documentsPath+"/"+original+"/correction", nil)
	correction := decodeObject(t, correctionRec).str(t, "id")
	f.mustIssue(t, correction)
	draft := f.createID(t)

	list := func(query string) map[string]jsonObject {
		t.Helper()

		rec := f.do(t, f.owner, http.MethodGet, documentsPath+query, nil)
		masterDataWantStatus(t, rec, http.StatusOK)
		out := map[string]jsonObject{}
		for _, d := range decodeObject(t, rec).list(t, "documents") {
			if _, ok := d["lines"]; ok {
				t.Errorf("a list entry carries lines: %v", d)
			}
			out[d.str(t, "id")] = d
		}

		return out
	}

	all := list("")
	if len(all) != 3 || all[original]["chain_status"] != "superseded" || all[correction]["chain_status"] != "effective" ||
		all[draft]["chain_status"] != nil {
		t.Fatalf("list = %v", all)
	}
	if all[original]["totals"] == nil || all[draft]["totals"] != nil {
		t.Errorf("totals: issued %v, draft %v, want the frozen totals and none", all[original]["totals"], all[draft]["totals"])
	}
	if got := list("?type=correction"); len(got) != 1 || got[correction] == nil {
		t.Errorf("type filter = %v", got)
	}
	if got := list("?state=draft"); len(got) != 1 || got[draft] == nil {
		t.Errorf("state filter = %v", got)
	}
	if got := list(fmt.Sprintf("?year=%d", thisYear())); len(got) != 3 {
		t.Errorf("year filter = %v", got)
	}
	if got := list(fmt.Sprintf("?year=%d", thisYear()-5)); len(got) != 0 {
		t.Errorf("year filter of another year = %v", got)
	}
	masterDataWantField(t, f.do(t, f.owner, http.MethodGet, documentsPath+"?state=cancelled", nil), "state")
}
