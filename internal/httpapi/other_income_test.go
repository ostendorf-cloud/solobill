package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// otherIncomeResponse is the other income JSON of docs/api.md.
type otherIncomeResponse struct {
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
	BookedAt         *time.Time `json:"booked_at"`
}

type otherIncomeListResponse struct {
	OtherIncome     []otherIncomeResponse `json:"other_income"`
	TotalGrossCents int64                 `json:"total_gross_cents"`
}

// vatRefund is a valid refund of the Finanzamt, Zeile 18, under either
// regime: a refund carries no VAT of its own.
func vatRefund(receivedOn string) map[string]any {
	return map[string]any{
		"received_on":        receivedOn,
		"description":        "USt-Erstattung Q3",
		"gross_cents":        12345,
		"net_cents":          12345,
		"vat_rate_permyriad": 0,
		"vat_cents":          0,
		"category":           "euer.z18",
	}
}

func otherIncomeRepo(t *testing.T, app testApp) *fakeOtherIncomeRepository {
	t.Helper()

	repo, ok := app.repos.OtherIncome.(*fakeOtherIncomeRepository)
	if !ok {
		t.Fatalf("OtherIncome = %T, want the fake", app.repos.OtherIncome)
	}

	return repo
}

func createOtherIncome(t *testing.T, app testApp, c masterDataCaller, body map[string]any) otherIncomeResponse {
	t.Helper()

	rec := c.do(t, app, http.MethodPost, "/api/v1/other-income", body)
	masterDataWantStatus(t, rec, http.StatusCreated)

	return masterDataDecode[otherIncomeResponse](t, rec)
}

func bookOtherIncome(t *testing.T, app testApp, c masterDataCaller, id uuid.UUID) {
	t.Helper()

	rec := c.do(t, app, http.MethodPost, "/api/v1/other-income/"+id.String()+"/book", nil)
	masterDataWantStatus(t, rec, http.StatusOK)
}

func TestOtherIncomeRequiresASession(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	id := uuid.NewString()
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/other-income"},
		{http.MethodPost, "/api/v1/other-income"},
		{http.MethodGet, "/api/v1/other-income/" + id},
		{http.MethodPut, "/api/v1/other-income/" + id},
		{http.MethodDelete, "/api/v1/other-income/" + id},
		{http.MethodPost, "/api/v1/other-income/" + id + "/book"},
		{http.MethodPost, "/api/v1/other-income/" + id + "/storno"},
		{http.MethodGet, "/api/v1/categories"},
	} {
		rec := doJSON(t, app.handler, route.method, route.path, nil, nil, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s status = %d, want 401", route.method, route.path, rec.Code)
		}
	}
}

func TestOtherIncomeLifecycle(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "income")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)
	receipt := addReceipt(t, app, tenant.owner.tenant.ID, storage.FilePurposeReceipt)

	created := createOtherIncome(t, app, tenant.member, withField(vatRefund("2026-11-18"), "file_id", receipt.String()))
	if created.State != "draft" || created.FileID == nil || *created.FileID != receipt {
		t.Fatalf("created = %+v", created)
	}

	rec := tenant.member.do(t, app, http.MethodPut, "/api/v1/other-income/"+created.ID.String(), withField(vatRefund("2026-11-19"), "description", "Erstattung"))
	masterDataWantStatus(t, rec, http.StatusOK)

	rec = tenant.member.do(t, app, http.MethodPost, "/api/v1/other-income/"+created.ID.String()+"/book", nil)
	masterDataWantStatus(t, rec, http.StatusForbidden)
	bookOtherIncome(t, app, tenant.owner, created.ID)

	rec = tenant.owner.do(t, app, http.MethodPut, "/api/v1/other-income/"+created.ID.String(), vatRefund("2026-11-20"))
	masterDataWantStatus(t, rec, http.StatusConflict)
	rec = tenant.owner.do(t, app, http.MethodDelete, "/api/v1/other-income/"+created.ID.String(), nil)
	masterDataWantStatus(t, rec, http.StatusConflict)
	if row, ok := otherIncomeRepo(t, app).row(created.ID); !ok || row.Description != "Erstattung" {
		t.Errorf("booked row changed or vanished: %+v", row)
	}

	rec = tenant.member.do(t, app, http.MethodPost, "/api/v1/other-income/"+created.ID.String()+"/storno", map[string]any{"received_on": "2026-11-25"})
	masterDataWantStatus(t, rec, http.StatusForbidden)
	rec = tenant.owner.do(t, app, http.MethodPost, "/api/v1/other-income/"+created.ID.String()+"/storno", map[string]any{"received_on": "2026-11-25"})
	masterDataWantStatus(t, rec, http.StatusCreated)
	storno := masterDataDecode[otherIncomeResponse](t, rec)
	if storno.GrossCents != -12345 || storno.StornoOfID == nil || *storno.StornoOfID != created.ID || *storno.ReceivedOn != "2026-11-25" {
		t.Errorf("storno = %+v", storno)
	}
	rec = tenant.owner.do(t, app, http.MethodPost, "/api/v1/other-income/"+created.ID.String()+"/storno", map[string]any{"received_on": "2026-11-26"})
	masterDataWantStatus(t, rec, http.StatusConflict)

	draft := createOtherIncome(t, app, tenant.member, vatRefund("2026-11-18"))
	rec = tenant.member.do(t, app, http.MethodDelete, "/api/v1/other-income/"+draft.ID.String(), nil)
	masterDataWantStatus(t, rec, http.StatusNoContent)
}

func TestOtherIncomeValidation(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "income-valid")
	addRegime(t, app, "2026-01-01", storage.VATRegimeStandard)
	foreign := addReceipt(t, app, uuid.New(), storage.FilePurposeReceipt)

	// The sale of an asset under the standard regime carries VAT.
	sale := map[string]any{
		"received_on": "2026-11-18", "description": "Laptop sold", "gross_cents": 11900, "net_cents": 10000,
		"vat_rate_permyriad": 1900, "vat_cents": 1900, "category": "euer.z19",
	}
	createOtherIncome(t, app, tenant.member, sale)

	for _, tc := range []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"expense key", withField(sale, "category", "euer.z27"), "category"},
		{"derived key", withField(sale, "category", "euer.z17"), "category"},
		{"unknown key", withField(sale, "category", "euer.z99"), "category"},
		{"VAT mismatch", withField(sale, "vat_cents", 1800), "vat_cents"},
		{"zero amount", withField(withField(withField(sale, "gross_cents", 0), "net_cents", 0), "vat_cents", 0), "gross_cents"},
		{"foreign receipt", withField(sale, "file_id", foreign.String()), "file_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tenant.member.do(t, app, http.MethodPost, "/api/v1/other-income", tc.body)
			masterDataWantField(t, rec, tc.field)
		})
	}

	// Fields of an expense and server owned fields are unknown here.
	for _, field := range []string{"supplier", "receipt_date", "is_asset", "paid_on", "tenant_id", "state"} {
		rec := tenant.member.do(t, app, http.MethodPost, "/api/v1/other-income", withField(sale, field, "x"))
		masterDataWantStatus(t, rec, http.StatusBadRequest)
	}
}

func TestOtherIncomeKleinunternehmer(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "income-klein")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)

	rec := tenant.member.do(t, app, http.MethodPost, "/api/v1/other-income", map[string]any{
		"received_on": "2026-11-18", "gross_cents": 11900, "net_cents": 10000,
		"vat_rate_permyriad": 1900, "vat_cents": 1900, "category": "euer.z19",
	})
	masterDataWantField(t, rec, "vat_cents")

	rec = tenant.member.do(t, app, http.MethodPost, "/api/v1/other-income", vatRefund("2025-12-31"))
	masterDataWantStatus(t, rec, http.StatusBadRequest)
	if got := masterDataDecode[masterDataErrorBody](t, rec); got.Error.Code != "vat_regime_missing" {
		t.Errorf("code = %q, want vat_regime_missing", got.Error.Code)
	}
}

func TestOtherIncomeOfAnotherTenantAnswers404(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	a := newBookingTenant(t, app, "income-a")
	b := newBookingTenant(t, app, "income-b")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)

	draftA := createOtherIncome(t, app, a.member, vatRefund("2026-11-18"))
	bookedA := createOtherIncome(t, app, a.member, vatRefund("2026-11-18"))
	bookOtherIncome(t, app, a.owner, bookedA.ID)

	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/other-income/" + draftA.ID.String(), nil},
		{http.MethodPut, "/api/v1/other-income/" + draftA.ID.String(), vatRefund("2026-11-19")},
		{http.MethodDelete, "/api/v1/other-income/" + draftA.ID.String(), nil},
		{http.MethodPost, "/api/v1/other-income/" + draftA.ID.String() + "/book", nil},
		{http.MethodPost, "/api/v1/other-income/" + bookedA.ID.String() + "/storno", map[string]any{"received_on": "2026-11-25"}},
	} {
		rec := b.owner.do(t, app, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s by tenant B = %d, want 404", tc.method, tc.path, rec.Code)
		}
	}

	rec := b.owner.do(t, app, http.MethodGet, "/api/v1/other-income", nil)
	masterDataWantStatus(t, rec, http.StatusOK)
	if list := masterDataDecode[otherIncomeListResponse](t, rec); len(list.OtherIncome) != 0 {
		t.Errorf("tenant B lists %+v", list)
	}
	if row, _ := otherIncomeRepo(t, app).row(draftA.ID); row.State != storage.BookingStateDraft || row.ReceivedOn.Format(time.DateOnly) != "2026-11-18" {
		t.Errorf("tenant A's draft changed: %+v", row)
	}
}

func TestOtherIncomeListYearBoundary(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "income-years")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)

	dec31 := createOtherIncome(t, app, tenant.member, vatRefund("2026-12-31"))
	jan1 := createOtherIncome(t, app, tenant.member, vatRefund("2027-01-01"))
	private := createOtherIncome(t, app, tenant.member, withField(vatRefund("2026-03-01"), "category", "euer.z20"))

	list := func(query string) []uuid.UUID {
		rec := tenant.member.do(t, app, http.MethodGet, "/api/v1/other-income"+query, nil)
		masterDataWantStatus(t, rec, http.StatusOK)
		var out []uuid.UUID
		for _, o := range masterDataDecode[otherIncomeListResponse](t, rec).OtherIncome {
			out = append(out, o.ID)
		}

		return out
	}

	for _, tc := range []struct {
		query string
		want  []uuid.UUID
	}{
		{"?year=2026", []uuid.UUID{private.ID, dec31.ID}},
		{"?year=2027", []uuid.UUID{jan1.ID}},
		{"?category=euer.z20", []uuid.UUID{private.ID}},
		{"?state=booked", nil},
	} {
		if got := list(tc.query); fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("%s = %v, want %v", tc.query, got, tc.want)
		}
	}

	rec := tenant.member.do(t, app, http.MethodGet, "/api/v1/other-income?category=euer.z27", nil)
	masterDataWantField(t, rec, "category")
}

func TestCategoriesList(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "categories")

	rec := tenant.member.do(t, app, http.MethodGet, "/api/v1/categories", nil)
	masterDataWantStatus(t, rec, http.StatusOK)
	got := masterDataDecode[struct {
		Categories []struct {
			Key     string `json:"key"`
			Kind    string `json:"kind"`
			LabelDE string `json:"label_de"`
			LabelEN string `json:"label_en"`
		} `json:"categories"`
	}](t, rec)

	if len(got.Categories) == 0 {
		t.Fatal("no categories")
	}
	seen := map[string]string{}
	for _, c := range got.Categories {
		if c.LabelDE == "" || c.LabelEN == "" {
			t.Errorf("%s has an empty label", c.Key)
		}
		seen[c.Key] = c.Kind
	}
	for key, kind := range map[string]string{"euer.z27": "expense", "euer.z58": "expense", "euer.z18": "income", "euer.z33": "asset"} {
		if seen[key] != kind {
			t.Errorf("%s kind = %q, want %q", key, seen[key], kind)
		}
	}
	if got.Categories[0].Key != "euer.z12" {
		t.Errorf("first key = %s, want euer.z12 in form order", got.Categories[0].Key)
	}
}
