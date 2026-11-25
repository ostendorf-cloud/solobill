package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// expenseResponse is the expense JSON of docs/api.md.
type expenseResponse struct {
	ID               uuid.UUID  `json:"id"`
	State            string     `json:"state"`
	PaidOn           *string    `json:"paid_on"`
	ReceiptDate      *string    `json:"receipt_date"`
	Supplier         string     `json:"supplier"`
	Description      string     `json:"description"`
	GrossCents       int64      `json:"gross_cents"`
	NetCents         int64      `json:"net_cents"`
	VATRatePermyriad int32      `json:"vat_rate_permyriad"`
	VATCents         int64      `json:"vat_cents"`
	Category         string     `json:"category"`
	FileID           *uuid.UUID `json:"file_id"`
	MissingReceipt   *bool      `json:"missing_receipt"`
	IsAsset          bool       `json:"is_asset"`
	StornoOfID       *uuid.UUID `json:"storno_of_id"`
	Imported         bool       `json:"imported"`
	BookedAt         *time.Time `json:"booked_at"`
}

type expenseListResponse struct {
	Expenses        []expenseResponse `json:"expenses"`
	TotalGrossCents int64             `json:"total_gross_cents"`
	TotalNetCents   int64             `json:"total_net_cents"`
}

// bookingTenant is one tenant with a logged in owner and a logged in
// member.
type bookingTenant struct {
	owner  masterDataCaller
	member masterDataCaller
}

// newBookingTenant creates a tenant, its owner and a member, both logged
// in.
func newBookingTenant(t *testing.T, app testApp, slug string) bookingTenant {
	t.Helper()

	owner := newMasterDataCaller(t, app, slug)

	hash := hashPassword(t, memberPassword)
	secret := totpSecret(t)
	email := slug + "-member@acme.test"
	app.store.addAccount(storage.Account{
		TenantID:     owner.tenant.ID,
		Email:        email,
		PasswordHash: &hash,
		TOTPSecret:   &secret,
		Role:         storage.AccountRoleMember,
		Status:       storage.AccountStatusActive,
	})
	cookie, token := loginAndCSRF(t, app, slug, email, memberPassword, secret)

	return bookingTenant{
		owner:  owner,
		member: masterDataCaller{tenant: owner.tenant, cookie: cookie, csrf: token},
	}
}

// addRegime adds a VAT regime period starting on validFrom.
func addRegime(t *testing.T, app testApp, validFrom string, regime storage.VATRegime) storage.VATRegimePeriod {
	t.Helper()

	from, err := time.Parse(time.DateOnly, validFrom)
	if err != nil {
		t.Fatal(err)
	}
	fake, ok := app.repos.VATRegimePeriods.(*fakeVATRegimes)
	if !ok {
		t.Fatalf("VATRegimePeriods = %T, want the fake", app.repos.VATRegimePeriods)
	}
	p, err := fake.Create(context.Background(), storage.VATRegimePeriod{ValidFrom: from, Regime: regime})
	if err != nil {
		t.Fatalf("add regime: %v", err)
	}

	return p
}

// supersedeRegime replaces period by one of regime on the same day.
func supersedeRegime(t *testing.T, app testApp, period storage.VATRegimePeriod, regime storage.VATRegime) {
	t.Helper()

	fake, ok := app.repos.VATRegimePeriods.(*fakeVATRegimes)
	if !ok {
		t.Fatalf("VATRegimePeriods = %T, want the fake", app.repos.VATRegimePeriods)
	}
	id := period.ID
	if _, err := fake.Create(context.Background(), storage.VATRegimePeriod{ValidFrom: period.ValidFrom, Regime: regime, SupersedesID: &id}); err != nil {
		t.Fatalf("supersede regime: %v", err)
	}
}

// addReceipt stores a files row of tenantID with purpose.
func addReceipt(t *testing.T, app testApp, tenantID uuid.UUID, purpose storage.FilePurpose) uuid.UUID {
	t.Helper()

	fixture, _, _ := filesOf(t, app)
	id := uuid.New()
	fixture.files.add(storage.File{
		ID:          id,
		TenantID:    tenantID,
		Purpose:     purpose,
		ObjectKey:   tenantID.String() + "/" + id.String(),
		SHA256:      "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		SizeBytes:   4,
		ContentType: "application/pdf",
		UploadedAt:  time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC),
	})

	return id
}

func expenseRepo(t *testing.T, app testApp) *fakeExpenseRepository {
	t.Helper()

	repo, ok := app.repos.Expenses.(*fakeExpenseRepository)
	if !ok {
		t.Fatalf("Expenses = %T, want the fake", app.repos.Expenses)
	}

	return repo
}

// kleinExpense is a valid expense under the Kleinunternehmer regime.
func kleinExpense(paidOn string) map[string]any {
	return map[string]any{
		"paid_on":            paidOn,
		"receipt_date":       paidOn,
		"supplier":           "Office Supplies Inc",
		"description":        "Paper and toner",
		"gross_cents":        5950,
		"net_cents":          5950,
		"vat_rate_permyriad": 0,
		"vat_cents":          0,
		"category":           "euer.z51",
		"is_asset":           false,
	}
}

// standardExpense is a valid expense under the standard regime.
func standardExpense(paidOn string) map[string]any {
	e := kleinExpense(paidOn)
	e["gross_cents"] = 11900
	e["net_cents"] = 10000
	e["vat_rate_permyriad"] = 1900
	e["vat_cents"] = 1900

	return e
}

func createExpense(t *testing.T, app testApp, c masterDataCaller, body map[string]any) expenseResponse {
	t.Helper()

	rec := c.do(t, app, http.MethodPost, "/api/v1/expenses", body)
	masterDataWantStatus(t, rec, http.StatusCreated)

	return masterDataDecode[expenseResponse](t, rec)
}

func bookExpense(t *testing.T, app testApp, c masterDataCaller, id uuid.UUID) expenseResponse {
	t.Helper()

	rec := c.do(t, app, http.MethodPost, "/api/v1/expenses/"+id.String()+"/book", nil)
	masterDataWantStatus(t, rec, http.StatusOK)

	return masterDataDecode[expenseResponse](t, rec)
}

func listExpenses(t *testing.T, app testApp, c masterDataCaller, query string) expenseListResponse {
	t.Helper()

	rec := c.do(t, app, http.MethodGet, "/api/v1/expenses"+query, nil)
	masterDataWantStatus(t, rec, http.StatusOK)

	return masterDataDecode[expenseListResponse](t, rec)
}

func withField(body map[string]any, key string, value any) map[string]any {
	out := make(map[string]any, len(body)+1)
	for k, v := range body {
		out[k] = v
	}
	out[key] = value

	return out
}

func TestExpensesRequireASession(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	id := uuid.NewString()
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/expenses"},
		{http.MethodPost, "/api/v1/expenses"},
		{http.MethodGet, "/api/v1/expenses/" + id},
		{http.MethodPut, "/api/v1/expenses/" + id},
		{http.MethodDelete, "/api/v1/expenses/" + id},
		{http.MethodPost, "/api/v1/expenses/" + id + "/book"},
		{http.MethodPost, "/api/v1/expenses/" + id + "/storno"},
	} {
		rec := doJSON(t, app.handler, route.method, route.path, nil, nil, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s status = %d, want 401", route.method, route.path, rec.Code)
		}
	}
}

// TestExpenseLifecycle walks a draft through edit, booking and storno
// under the Kleinunternehmer regime, the owner's regime today.
func TestExpenseLifecycle(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "lifecycle")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)
	receipt := addReceipt(t, app, tenant.owner.tenant.ID, storage.FilePurposeReceipt)

	// A member keeps drafts.
	created := createExpense(t, app, tenant.member, withField(kleinExpense("2026-11-18"), "file_id", receipt.String()))
	if created.State != "draft" || created.BookedAt != nil || created.StornoOfID != nil {
		t.Fatalf("created = %+v, want a draft", created)
	}
	if created.FileID == nil || *created.FileID != receipt {
		t.Errorf("file_id = %v, want %v", created.FileID, receipt)
	}
	stored, _ := expenseRepo(t, app).row(created.ID)
	if stored.TenantID != tenant.owner.tenant.ID {
		t.Errorf("stored tenant = %v, want the session tenant", stored.TenantID)
	}

	rec := tenant.member.do(t, app, http.MethodPut, "/api/v1/expenses/"+created.ID.String(),
		withField(kleinExpense("2026-11-19"), "description", "Toner only"))
	masterDataWantStatus(t, rec, http.StatusOK)
	if got := masterDataDecode[expenseResponse](t, rec); got.Description != "Toner only" || *got.PaidOn != "2026-11-19" {
		t.Errorf("updated = %+v", got)
	}

	booked := bookExpense(t, app, tenant.owner, created.ID)
	if booked.State != "booked" || booked.BookedAt == nil {
		t.Fatalf("booked = %+v", booked)
	}

	// Booked is immutable, for the owner too.
	for _, c := range []masterDataCaller{tenant.member, tenant.owner} {
		rec = c.do(t, app, http.MethodPut, "/api/v1/expenses/"+created.ID.String(), kleinExpense("2026-11-20"))
		masterDataWantStatus(t, rec, http.StatusConflict)
		rec = c.do(t, app, http.MethodDelete, "/api/v1/expenses/"+created.ID.String(), nil)
		masterDataWantStatus(t, rec, http.StatusConflict)
	}
	rec = tenant.owner.do(t, app, http.MethodPost, "/api/v1/expenses/"+created.ID.String()+"/book", nil)
	masterDataWantStatus(t, rec, http.StatusConflict)
	if after, ok := expenseRepo(t, app).row(created.ID); !ok || after.Description != "Toner only" || after.State != storage.BookingStateBooked {
		t.Errorf("booked row changed or vanished: %+v", after)
	}

	// The storno reverses it with negated amounts, once.
	rec = tenant.owner.do(t, app, http.MethodPost, "/api/v1/expenses/"+created.ID.String()+"/storno", map[string]any{"paid_on": "2026-11-25"})
	masterDataWantStatus(t, rec, http.StatusCreated)
	storno := masterDataDecode[expenseResponse](t, rec)
	if storno.State != "booked" || storno.StornoOfID == nil || *storno.StornoOfID != created.ID ||
		storno.GrossCents != -5950 || storno.NetCents != -5950 || storno.VATCents != 0 ||
		*storno.PaidOn != "2026-11-25" || storno.Category != "euer.z51" {
		t.Errorf("storno = %+v", storno)
	}

	rec = tenant.owner.do(t, app, http.MethodPost, "/api/v1/expenses/"+created.ID.String()+"/storno", map[string]any{"paid_on": "2026-11-26"})
	masterDataWantStatus(t, rec, http.StatusConflict)
	rec = tenant.owner.do(t, app, http.MethodPost, "/api/v1/expenses/"+storno.ID.String()+"/storno", map[string]any{"paid_on": "2026-11-26"})
	masterDataWantStatus(t, rec, http.StatusConflict)

	// A draft cannot be reversed, a draft can be deleted.
	draft := createExpense(t, app, tenant.member, kleinExpense("2026-11-18"))
	rec = tenant.owner.do(t, app, http.MethodPost, "/api/v1/expenses/"+draft.ID.String()+"/storno", map[string]any{"paid_on": "2026-11-26"})
	masterDataWantStatus(t, rec, http.StatusConflict)
	rec = tenant.member.do(t, app, http.MethodDelete, "/api/v1/expenses/"+draft.ID.String(), nil)
	masterDataWantStatus(t, rec, http.StatusNoContent)
	if _, ok := expenseRepo(t, app).row(draft.ID); ok {
		t.Error("deleted draft is still stored")
	}
}

func TestExpenseBookAndStornoAreOwnerOnly(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "roles")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)

	draft := createExpense(t, app, tenant.member, kleinExpense("2026-11-18"))
	rec := tenant.member.do(t, app, http.MethodPost, "/api/v1/expenses/"+draft.ID.String()+"/book", nil)
	masterDataWantStatus(t, rec, http.StatusForbidden)
	if row, _ := expenseRepo(t, app).row(draft.ID); row.State != storage.BookingStateDraft {
		t.Errorf("a member booked: %+v", row)
	}

	bookExpense(t, app, tenant.owner, draft.ID)
	rec = tenant.member.do(t, app, http.MethodPost, "/api/v1/expenses/"+draft.ID.String()+"/storno", map[string]any{"paid_on": "2026-11-25"})
	masterDataWantStatus(t, rec, http.StatusForbidden)
	if n := expenseRepo(t, app).count(); n != 1 {
		t.Errorf("a member reversed: %d rows", n)
	}
}

func TestExpenseBookingWithoutReceiptIsAllowed(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "noreceipt")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)

	draft := createExpense(t, app, tenant.owner, kleinExpense("2026-11-18"))
	if booked := bookExpense(t, app, tenant.owner, draft.ID); booked.FileID != nil {
		t.Errorf("file_id = %v, want null", booked.FileID)
	}
}

// wantMissingReceipt fails unless the expense JSON carries
// missing_receipt with the value want. A field that is absent fails too.
func wantMissingReceipt(t *testing.T, where string, e expenseResponse, want bool) {
	t.Helper()

	if e.MissingReceipt == nil {
		t.Errorf("%s: missing_receipt is absent, want %v", where, want)

		return
	}
	if *e.MissingReceipt != want {
		t.Errorf("%s: missing_receipt = %v, want %v", where, *e.MissingReceipt, want)
	}
}

// TestExpenseMissingReceiptFlag checks the derived flag on every route
// that answers with an expense JSON: create, book, storno, read and list.
func TestExpenseMissingReceiptFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		withReceipt bool
		book        bool
		want        bool
	}{
		{name: "draft without receipt", withReceipt: false, book: false, want: false},
		{name: "draft with receipt", withReceipt: true, book: false, want: false},
		{name: "booked without receipt", withReceipt: false, book: true, want: true},
		{name: "booked with receipt", withReceipt: true, book: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := buildTestApp(t, 1000)
			tenant := newBookingTenant(t, app, "flag")
			addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)
			body := kleinExpense("2026-11-18")
			if tt.withReceipt {
				body = withField(body, "file_id", addReceipt(t, app, tenant.owner.tenant.ID, storage.FilePurposeReceipt).String())
			}

			created := createExpense(t, app, tenant.owner, body)
			wantMissingReceipt(t, "create", created, false)
			got := created
			if tt.book {
				got = bookExpense(t, app, tenant.owner, created.ID)
				wantMissingReceipt(t, "book", got, tt.want)
			}

			rec := tenant.member.do(t, app, http.MethodGet, "/api/v1/expenses/"+created.ID.String(), nil)
			masterDataWantStatus(t, rec, http.StatusOK)
			wantMissingReceipt(t, "get", masterDataDecode[expenseResponse](t, rec), tt.want)

			list := listExpenses(t, app, tenant.member, "")
			if len(list.Expenses) != 1 {
				t.Fatalf("list has %d expenses, want 1", len(list.Expenses))
			}
			wantMissingReceipt(t, "list", list.Expenses[0], tt.want)

			if !tt.book {
				return
			}
			// The storno copies the file of its original, so it carries
			// the same flag.
			rec = tenant.owner.do(t, app, http.MethodPost, "/api/v1/expenses/"+got.ID.String()+"/storno", map[string]any{"paid_on": "2026-11-25"})
			masterDataWantStatus(t, rec, http.StatusCreated)
			wantMissingReceipt(t, "storno", masterDataDecode[expenseResponse](t, rec), tt.want)
		})
	}
}

func TestExpenseBookingNeedsDateAndCategory(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "incomplete")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)

	body := kleinExpense("")
	delete(body, "paid_on")
	body["category"] = ""
	draft := createExpense(t, app, tenant.member, body)

	rec := tenant.owner.do(t, app, http.MethodPost, "/api/v1/expenses/"+draft.ID.String()+"/book", nil)
	masterDataWantField(t, rec, "paid_on")
	masterDataWantField(t, rec, "category")
}

// TestExpenseKleinunternehmerKeepsVATInsideGross: no Vorsteuer, so net
// equals gross and rate and VAT are 0.
func TestExpenseKleinunternehmerKeepsVATInsideGross(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "klein")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)

	for _, tc := range []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"vorsteuer claimed", standardExpense("2026-11-18"), "vat_cents"},
		{"rate set", withField(kleinExpense("2026-11-18"), "vat_rate_permyriad", 1900), "vat_rate_permyriad"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tenant.member.do(t, app, http.MethodPost, "/api/v1/expenses", tc.body)
			masterDataWantField(t, rec, tc.field)
		})
	}
	if n := expenseRepo(t, app).count(); n != 0 {
		t.Errorf("%d rows stored, want none", n)
	}
}

// TestExpenseStandardRegimeVAT: net, rate and VAT add up, computed from
// the net amount or extracted from the gross amount.
func TestExpenseStandardRegimeVAT(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "standard")
	addRegime(t, app, "2026-01-01", storage.VATRegimeStandard)

	amounts := func(gross, net, rate, vat int64) map[string]any {
		b := kleinExpense("2026-11-18")
		b["gross_cents"], b["net_cents"], b["vat_rate_permyriad"], b["vat_cents"] = gross, net, rate, vat

		return b
	}

	for _, tc := range []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"19 percent from net", amounts(11900, 10000, 1900, 1900), ""},
		{"7 percent from net", amounts(1070, 1000, 700, 70), ""},
		{"rounded from net", amounts(104, 87, 1900, 17), ""},
		// 103 gross at 19 percent contains 16.45 cents VAT; 87 net times
		// 19 percent is 16.53. A receipt may print either.
		{"extracted from gross", amounts(103, 87, 1900, 16), ""},
		// 47 gross at 19 percent contains exactly 7.50 cents VAT. Half away
		// from zero extracts 8 (39 net plus 8; 39 net alone gives 7.41, so
		// only the extraction accepts it). A truncation extracts 7.
		{"extracted from gross, half rounds up", amounts(47, 39, 1900, 8), ""},
		// 40 net gives 7.60, extraction gives 8: 7 matches neither.
		{"half rounded down is refused", amounts(47, 40, 1900, 7), "vat_cents"},
		{"no VAT at 0 percent", amounts(5000, 5000, 0, 0), ""},
		{"VAT does not match the rate", amounts(11800, 10000, 1900, 1800), "vat_cents"},
		{"VAT at 0 percent", amounts(5100, 5000, 0, 100), "vat_cents"},
		{"parts do not add up", amounts(12000, 10000, 1900, 1900), "vat_cents"},
		{"rate above 100 percent", amounts(11900, 10000, 10001, 1900), "vat_rate_permyriad"},
		{"negative rate", amounts(11900, 10000, -1, 1900), "vat_rate_permyriad"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tenant.member.do(t, app, http.MethodPost, "/api/v1/expenses", tc.body)
			if tc.field == "" {
				masterDataWantStatus(t, rec, http.StatusCreated)

				return
			}
			masterDataWantField(t, rec, tc.field)
		})
	}
}

func TestExpenseAmountsMustBePositive(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "amounts")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)

	for _, tc := range []struct {
		name            string
		gross, net, vat int64
		field           string
		rate            int64
	}{
		{name: "zero", gross: 0, net: 0, field: "gross_cents"},
		{name: "negative", gross: -5950, net: -5950, field: "gross_cents"},
		{name: "negative net", gross: 100, net: -100, vat: 200, rate: 1900, field: "net_cents"},
		{name: "negative vat", gross: 100, net: 200, vat: -100, field: "vat_cents"},
		{name: "above the bound", gross: 100_000_000_001, net: 100_000_000_001, field: "gross_cents"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := kleinExpense("2026-11-18")
			b["gross_cents"], b["net_cents"], b["vat_cents"], b["vat_rate_permyriad"] = tc.gross, tc.net, tc.vat, tc.rate
			rec := tenant.member.do(t, app, http.MethodPost, "/api/v1/expenses", b)
			masterDataWantField(t, rec, tc.field)
		})
	}
	if n := expenseRepo(t, app).count(); n != 0 {
		t.Errorf("%d rows stored, want none", n)
	}
}

// TestExpenseCategoryMustBeAnExpenseKey: only keys of kind expense, never
// a form number, an income key, an asset key or a derived key.
func TestExpenseCategoryMustBeAnExpenseKey(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "category")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)

	for _, key := range []string{"euer.z999", "z27", "27", "euer.z27.x", "EUER.Z27", "euer.z18", "euer.z33", "euer.z57"} {
		t.Run(key, func(t *testing.T) {
			rec := tenant.member.do(t, app, http.MethodPost, "/api/v1/expenses", withField(kleinExpense("2026-11-18"), "category", key))
			masterDataWantField(t, rec, "category")
		})
	}
	// USt-Vorauszahlungen are an expense on Zeile 58.
	createExpense(t, app, tenant.member, withField(kleinExpense("2026-11-18"), "category", "euer.z58"))
}

func TestExpenseRefusesUnknownAndServerOwnedFields(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "fields")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)
	other := uuid.NewString()

	for _, field := range []string{"tenant_id", "id", "state", "storno_of_id", "imported", "booked_at", "created_at", "missing_receipt", "nonsense"} {
		t.Run(field, func(t *testing.T) {
			value := any(other)
			if field == "imported" || field == "missing_receipt" {
				value = true
			}
			rec := tenant.member.do(t, app, http.MethodPost, "/api/v1/expenses", withField(kleinExpense("2026-11-18"), field, value))
			masterDataWantStatus(t, rec, http.StatusBadRequest)
		})
	}

	booked := createExpense(t, app, tenant.owner, kleinExpense("2026-11-18"))
	bookExpense(t, app, tenant.owner, booked.ID)
	rec := tenant.owner.do(t, app, http.MethodPost, "/api/v1/expenses/"+booked.ID.String()+"/storno",
		map[string]any{"paid_on": "2026-11-25", "gross_cents": 1})
	masterDataWantStatus(t, rec, http.StatusBadRequest)

	if n := expenseRepo(t, app).count(); n != 1 {
		t.Errorf("%d rows stored, want only the booked one", n)
	}
}

func TestExpenseVATRegimeMissing(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "noregime")
	addRegime(t, app, "2026-07-01", storage.VATRegimeKleinunternehmer)

	rec := tenant.member.do(t, app, http.MethodPost, "/api/v1/expenses", kleinExpense("2026-06-30"))
	masterDataWantStatus(t, rec, http.StatusBadRequest)
	if got := masterDataDecode[masterDataErrorBody](t, rec); got.Error.Code != "vat_regime_missing" {
		t.Errorf("code = %q, want vat_regime_missing", got.Error.Code)
	}

	created := createExpense(t, app, tenant.member, kleinExpense("2026-07-01"))
	bookExpense(t, app, tenant.owner, created.ID)

	// The storno date needs a period as well.
	rec = tenant.owner.do(t, app, http.MethodPost, "/api/v1/expenses/"+created.ID.String()+"/storno", map[string]any{"paid_on": "2026-06-30"})
	masterDataWantStatus(t, rec, http.StatusBadRequest)
}

// TestExpenseBookingChecksTheRegimeAgain: a period added after the draft
// was saved decides the booking.
func TestExpenseBookingChecksTheRegimeAgain(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "recheck")
	period := addRegime(t, app, "2026-01-01", storage.VATRegimeStandard)

	draft := createExpense(t, app, tenant.member, standardExpense("2026-11-18"))
	supersedeRegime(t, app, period, storage.VATRegimeKleinunternehmer)

	rec := tenant.owner.do(t, app, http.MethodPost, "/api/v1/expenses/"+draft.ID.String()+"/book", nil)
	masterDataWantField(t, rec, "vat_cents")
	if row, _ := expenseRepo(t, app).row(draft.ID); row.State != storage.BookingStateDraft {
		t.Errorf("state = %s, want draft", row.State)
	}
}

// TestExpenseReceiptMustBeOwnReceipt: a file of another tenant answers
// exactly like an unknown id, and a file that is not a receipt is
// refused.
func TestExpenseReceiptMustBeOwnReceipt(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	a := newBookingTenant(t, app, "receipt-a")
	b := newBookingTenant(t, app, "receipt-b")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)

	foreign := addReceipt(t, app, b.owner.tenant.ID, storage.FilePurposeReceipt)
	logo := addReceipt(t, app, a.owner.tenant.ID, storage.FilePurposeLogo)
	own := addReceipt(t, app, a.owner.tenant.ID, storage.FilePurposeReceipt)

	unknownRec := a.member.do(t, app, http.MethodPost, "/api/v1/expenses", withField(kleinExpense("2026-11-18"), "file_id", uuid.NewString()))
	masterDataWantField(t, unknownRec, "file_id")
	foreignRec := a.member.do(t, app, http.MethodPost, "/api/v1/expenses", withField(kleinExpense("2026-11-18"), "file_id", foreign.String()))
	masterDataWantField(t, foreignRec, "file_id")
	unknown := masterDataDecode[masterDataErrorBody](t, unknownRec)
	foreignBody := masterDataDecode[masterDataErrorBody](t, foreignRec)
	if fmt.Sprint(unknown.Error.Details) != fmt.Sprint(foreignBody.Error.Details) {
		t.Errorf("a foreign file answers %v, an unknown one %v; want the same", foreignBody.Error.Details, unknown.Error.Details)
	}

	rec := a.member.do(t, app, http.MethodPost, "/api/v1/expenses", withField(kleinExpense("2026-11-18"), "file_id", logo.String()))
	masterDataWantField(t, rec, "file_id")
	rec = a.member.do(t, app, http.MethodPost, "/api/v1/expenses", withField(kleinExpense("2026-11-18"), "file_id", "not-a-uuid"))
	masterDataWantField(t, rec, "file_id")

	// The update path checks the same.
	draft := createExpense(t, app, a.member, withField(kleinExpense("2026-11-18"), "file_id", own.String()))
	rec = a.member.do(t, app, http.MethodPut, "/api/v1/expenses/"+draft.ID.String(), withField(kleinExpense("2026-11-18"), "file_id", foreign.String()))
	masterDataWantField(t, rec, "file_id")
	if row, _ := expenseRepo(t, app).row(draft.ID); row.FileID == nil || *row.FileID != own {
		t.Errorf("file_id = %v, want the own receipt", row.FileID)
	}
}

// TestExpensesOfAnotherTenantAnswer404 is the cross tenant test: tenant B
// neither reads nor changes tenant A's expense, and A's row is unchanged.
func TestExpensesOfAnotherTenantAnswer404(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	a := newBookingTenant(t, app, "tenant-a")
	b := newBookingTenant(t, app, "tenant-b")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)

	draftA := createExpense(t, app, a.member, kleinExpense("2026-11-18"))
	bookedA := createExpense(t, app, a.member, kleinExpense("2026-11-18"))
	bookExpense(t, app, a.owner, bookedA.ID)

	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/expenses/" + draftA.ID.String(), nil},
		{http.MethodPut, "/api/v1/expenses/" + draftA.ID.String(), kleinExpense("2026-11-19")},
		{http.MethodDelete, "/api/v1/expenses/" + draftA.ID.String(), nil},
		{http.MethodPost, "/api/v1/expenses/" + draftA.ID.String() + "/book", nil},
		{http.MethodPost, "/api/v1/expenses/" + bookedA.ID.String() + "/storno", map[string]any{"paid_on": "2026-11-25"}},
	} {
		rec := b.owner.do(t, app, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s by tenant B = %d, want 404, body = %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	if list := listExpenses(t, app, b.owner, ""); len(list.Expenses) != 0 || list.TotalGrossCents != 0 {
		t.Errorf("tenant B lists %+v", list)
	}

	row, ok := expenseRepo(t, app).row(draftA.ID)
	if !ok || row.State != storage.BookingStateDraft || row.PaidOn.Format(time.DateOnly) != "2026-11-18" || row.TenantID != a.owner.tenant.ID {
		t.Errorf("tenant A's draft changed: %+v", row)
	}
	if n := expenseRepo(t, app).count(); n != 2 {
		t.Errorf("%d rows, want tenant A's two and no storno", n)
	}

	// Tenant B's own create lands in tenant B.
	own := createExpense(t, app, b.member, kleinExpense("2026-11-18"))
	if row, _ := expenseRepo(t, app).row(own.ID); row.TenantID != b.owner.tenant.ID {
		t.Errorf("tenant B's expense stored under %v", row.TenantID)
	}
	if list := listExpenses(t, app, a.owner, ""); len(list.Expenses) != 2 {
		t.Errorf("tenant A lists %d expenses, want its own two", len(list.Expenses))
	}
}

// TestExpenseHandlerChecksTheTenantItself: with a repository that lost
// its tenant filter, the handler still answers 404 and lists nothing of
// the other tenant. Row level security is the guarantee; this is the
// layer above it.
func TestExpenseHandlerChecksTheTenantItself(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	a := newBookingTenant(t, app, "leak-a")
	b := newBookingTenant(t, app, "leak-b")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)

	draftA := createExpense(t, app, a.member, kleinExpense("2026-11-18"))
	bookedA := createExpense(t, app, a.member, kleinExpense("2026-11-18"))
	bookExpense(t, app, a.owner, bookedA.ID)
	expenseRepo(t, app).leak()

	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/expenses/" + draftA.ID.String(), nil},
		{http.MethodPut, "/api/v1/expenses/" + draftA.ID.String(), kleinExpense("2026-11-19")},
		{http.MethodDelete, "/api/v1/expenses/" + draftA.ID.String(), nil},
		{http.MethodPost, "/api/v1/expenses/" + draftA.ID.String() + "/book", nil},
		{http.MethodPost, "/api/v1/expenses/" + bookedA.ID.String() + "/storno", map[string]any{"paid_on": "2026-11-25"}},
	} {
		rec := b.owner.do(t, app, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s by tenant B = %d, want 404", tc.method, tc.path, rec.Code)
		}
	}
	if list := listExpenses(t, app, b.owner, ""); len(list.Expenses) != 0 || list.TotalGrossCents != 0 {
		t.Errorf("tenant B lists %+v", list)
	}
}

// TestExpenseHandlerRefusesChangesToBookedRowsItself: with a repository
// that lost its draft guard and trigger, the handler still refuses to
// change or delete a booked expense. The trigger is the guarantee; this
// is the layer above it.
func TestExpenseHandlerRefusesChangesToBookedRowsItself(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "unguarded")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)

	booked := createExpense(t, app, tenant.member, kleinExpense("2026-11-18"))
	bookExpense(t, app, tenant.owner, booked.ID)
	expenseRepo(t, app).unguard()

	rec := tenant.owner.do(t, app, http.MethodPut, "/api/v1/expenses/"+booked.ID.String(), withField(kleinExpense("2026-11-18"), "description", "tampered"))
	masterDataWantStatus(t, rec, http.StatusConflict)
	rec = tenant.owner.do(t, app, http.MethodDelete, "/api/v1/expenses/"+booked.ID.String(), nil)
	masterDataWantStatus(t, rec, http.StatusConflict)

	row, ok := expenseRepo(t, app).row(booked.ID)
	if !ok || row.State != storage.BookingStateBooked || row.Description != "Paper and toner" {
		t.Errorf("booked row = %+v, %v, want it unchanged", row, ok)
	}
}

// TestExpenseBookingRechecksTheBookedRow: a change that slips in between
// the handler's read and the booking answers 409 instead of booking a row
// that was not checked.
func TestExpenseBookingRechecksTheBookedRow(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "rebook")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)

	draft := createExpense(t, app, tenant.member, kleinExpense("2026-11-18"))
	expenseRepo(t, app).changeOnBook(func(e storage.Expense) storage.Expense {
		e.GrossCents, e.NetCents = e.GrossCents+100, e.NetCents+100

		return e
	})

	rec := tenant.owner.do(t, app, http.MethodPost, "/api/v1/expenses/"+draft.ID.String()+"/book", nil)
	masterDataWantStatus(t, rec, http.StatusConflict)
}

// TestExpenseListYearBoundaryAndFilters: the year comes from paid_on, so
// 31 December and 1 January fall into different years, and a storno
// counts in the year the money flowed back.
func TestExpenseListYearBoundaryAndFilters(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "years")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)

	dec31 := createExpense(t, app, tenant.member, withField(kleinExpense("2026-12-31"), "receipt_date", "2027-01-02"))
	jan1 := createExpense(t, app, tenant.member, withField(kleinExpense("2027-01-01"), "receipt_date", "2026-12-30"))
	june := createExpense(t, app, tenant.member, withField(kleinExpense("2026-06-01"), "category", "euer.z43"))
	// Same day as dec31, created later: ordered after it.
	dec31b := createExpense(t, app, tenant.member, kleinExpense("2026-12-31"))
	noDate := kleinExpense("")
	delete(noDate, "paid_on")
	undated := createExpense(t, app, tenant.member, noDate)

	bookExpense(t, app, tenant.owner, dec31.ID)
	rec := tenant.owner.do(t, app, http.MethodPost, "/api/v1/expenses/"+dec31.ID.String()+"/storno", map[string]any{"paid_on": "2027-01-05"})
	masterDataWantStatus(t, rec, http.StatusCreated)
	storno := masterDataDecode[expenseResponse](t, rec)

	ids := func(list expenseListResponse) []uuid.UUID {
		out := make([]uuid.UUID, len(list.Expenses))
		for i, e := range list.Expenses {
			out[i] = e.ID
		}

		return out
	}
	wantIDs := func(name string, got []uuid.UUID, want ...uuid.UUID) {
		t.Helper()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}

	y2026 := listExpenses(t, app, tenant.member, "?year=2026")
	wantIDs("year 2026", ids(y2026), june.ID, dec31.ID, dec31b.ID)
	if y2026.TotalGrossCents != 3*5950 || y2026.TotalNetCents != 3*5950 {
		t.Errorf("totals 2026 = %d, %d", y2026.TotalGrossCents, y2026.TotalNetCents)
	}
	y2027 := listExpenses(t, app, tenant.member, "?year=2027")
	wantIDs("year 2027", ids(y2027), jan1.ID, storno.ID)
	if y2027.TotalGrossCents != 0 {
		t.Errorf("total 2027 = %d, want 5950 minus the storno", y2027.TotalGrossCents)
	}

	wantIDs("all", ids(listExpenses(t, app, tenant.member, "")), june.ID, dec31.ID, dec31b.ID, jan1.ID, storno.ID, undated.ID)
	wantIDs("category", ids(listExpenses(t, app, tenant.member, "?category=euer.z43")), june.ID)
	wantIDs("booked 2026", ids(listExpenses(t, app, tenant.member, "?year=2026&state=booked")), dec31.ID)
	wantIDs("drafts 2027", ids(listExpenses(t, app, tenant.member, "?year=2027&state=draft")), jan1.ID)

	for _, query := range []struct{ q, field string }{
		{"?year=26", "year"},
		{"?year=abc", "year"},
		{"?state=open", "state"},
		{"?category=euer.z18", "category"},
		{"?category=z27", "category"},
	} {
		rec := tenant.member.do(t, app, http.MethodGet, "/api/v1/expenses"+query.q, nil)
		masterDataWantField(t, rec, query.field)
	}
}

func TestExpenseStornoDate(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "stornodate")
	addRegime(t, app, "2026-01-01", storage.VATRegimeKleinunternehmer)

	booked := createExpense(t, app, tenant.owner, kleinExpense("2026-11-18"))
	bookExpense(t, app, tenant.owner, booked.ID)

	for _, body := range []map[string]any{
		{},
		{"paid_on": ""},
		{"paid_on": "18.11.2026"},
		{"paid_on": "2026-11-17"},
	} {
		rec := tenant.owner.do(t, app, http.MethodPost, "/api/v1/expenses/"+booked.ID.String()+"/storno", body)
		masterDataWantField(t, rec, "paid_on")
	}
}

func TestExpenseDatesAreValidated(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	tenant := newBookingTenant(t, app, "dates")
	addRegime(t, app, "2000-01-01", storage.VATRegimeKleinunternehmer)

	for _, tc := range []struct{ field, value string }{
		{"paid_on", "2026-13-01"},
		{"paid_on", "1999-12-31"},
		{"paid_on", time.Now().UTC().AddDate(1, 0, 2).Format(time.DateOnly)},
		{"receipt_date", "yesterday"},
	} {
		rec := tenant.member.do(t, app, http.MethodPost, "/api/v1/expenses", withField(kleinExpense("2026-11-18"), tc.field, tc.value))
		masterDataWantField(t, rec, tc.field)
	}
}
