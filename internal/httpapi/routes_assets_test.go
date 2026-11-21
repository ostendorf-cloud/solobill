package httpapi_test

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

const assetsPath = "/api/v1/assets"

// assetExtras are repositories this feature does not own. Only a test
// that needs one hands it to newAssetFixtureWith, and the hook below sets
// it for that test's app alone.
type assetExtras struct {
	expenses *assetExpenses
}

// assetExtrasByTest maps the *testing.T of a test to its extras.
var assetExtrasByTest sync.Map

// The extras hook lives in this file, not in fake_assets_test.go: Go runs
// the init functions of a package's files in file name order, so this
// hook runs after fake_expenses_test.go's (issue #76), which sets
// Repositories.Expenses for every test.
func init() {
	repositoryHooks = append(repositoryHooks, func(t *testing.T, repos *httpapi.Repositories) {
		if v, ok := assetExtrasByTest.Load(t); ok {
			extras, _ := v.(assetExtras)
			if extras.expenses != nil {
				repos.Expenses = extras.expenses
			}
		}
	})
}

func newAssetFixture(t *testing.T) profileFixture {
	t.Helper()

	return newAssetFixtureWith(t, assetExtras{})
}

func newAssetFixtureWith(t *testing.T, extras assetExtras) profileFixture {
	t.Helper()

	assetExtrasByTest.Store(t, extras)
	t.Cleanup(func() { assetExtrasByTest.Delete(t) })

	return newProfileFixture(t)
}

func fakeAssetsOf(t *testing.T, f profileFixture) *fakeAssets {
	t.Helper()

	assets, ok := f.app.repos.Assets.(*fakeAssets)
	if !ok {
		t.Fatalf("Assets is %T, want the asset fake", f.app.repos.Assets)
	}

	return assets
}

// thisYear is the first day of the current year: an acquisition date
// whose first schedule year is open, and never in the future.
func thisYear() string {
	return fmt.Sprintf("%d-01-01", time.Now().UTC().Year())
}

func lastYear() string {
	return fmt.Sprintf("%d-06-01", time.Now().UTC().Year()-1)
}

func laptopBody() map[string]any {
	return map[string]any{
		"name":               "Laptop",
		"acquired_on":        "2026-03-10",
		"cost_basis_cents":   149900,
		"method":             "linear",
		"group":              "office_equipment",
		"useful_life_months": 36,
	}
}

func createAsset(t *testing.T, f profileFixture, body map[string]any) map[string]any {
	t.Helper()

	rec := f.do(t, f.owner, http.MethodPost, assetsPath, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, body = %s", rec.Code, rec.Body.String())
	}

	return profileDecodeMap(t, rec)
}

func TestAssetsRequireASession(t *testing.T) {
	t.Parallel()

	f := newAssetFixture(t)
	id := uuid.NewString()
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, assetsPath},
		{http.MethodPost, assetsPath},
		{http.MethodGet, assetsPath + "/" + id},
		{http.MethodPut, assetsPath + "/" + id},
		{http.MethodPost, assetsPath + "/" + id + "/disposal"},
		{http.MethodGet, assetsPath + "/" + id + "/schedule"},
	} {
		if rec := doJSON(t, f.app.handler, tc.method, tc.path, nil, nil, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s status = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

func TestAssetsListIsEmptyArrayAndOpenToMembers(t *testing.T) {
	t.Parallel()

	f := newAssetFixture(t)
	rec := f.do(t, f.memberSession(t), http.MethodGet, assetsPath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got, ok := profileDecodeMap(t, rec)["assets"].([]any); !ok || len(got) != 0 {
		t.Errorf("assets = %v, want an empty list", profileDecodeMap(t, rec)["assets"])
	}
}

func TestAssetsWritesAreOwnerOnly(t *testing.T) {
	t.Parallel()

	f := newAssetFixture(t)
	member := f.memberSession(t)

	if rec := f.do(t, member, http.MethodPost, assetsPath, laptopBody()); rec.Code != http.StatusForbidden {
		t.Errorf("member POST status = %d, want 403", rec.Code)
	}
	if n := len(fakeAssetsOf(t, f).assets); n != 0 {
		t.Fatalf("a refused POST stored %d assets", n)
	}

	created := createAsset(t, f, laptopBody())
	id, _ := created["id"].(string)

	if rec := f.do(t, member, http.MethodPut, assetsPath+"/"+id, laptopBody()); rec.Code != http.StatusForbidden {
		t.Errorf("member PUT status = %d, want 403", rec.Code)
	}
	if rec := f.do(t, member, http.MethodPost, assetsPath+"/"+id+"/disposal", map[string]any{"disposed_on": "2026-04-01"}); rec.Code != http.StatusForbidden {
		t.Errorf("member disposal status = %d, want 403", rec.Code)
	}
	// Reading stays open to members.
	if rec := f.do(t, member, http.MethodGet, assetsPath+"/"+id, nil); rec.Code != http.StatusOK {
		t.Errorf("member GET status = %d, want 200", rec.Code)
	}
	if rec := f.do(t, member, http.MethodGet, assetsPath+"/"+id+"/schedule", nil); rec.Code != http.StatusOK {
		t.Errorf("member schedule status = %d, want 200", rec.Code)
	}
}

func TestAssetsCreateReadAndSchedule(t *testing.T) {
	t.Parallel()

	f := newAssetFixture(t)
	created := createAsset(t, f, laptopBody())

	want := map[string]any{
		"name": "Laptop", "acquired_on": "2026-03-10", "cost_basis_cents": float64(149900), "method": "linear",
		"group": "office_equipment", "useful_life_months": float64(36), "expense_id": nil,
		"opening_book_value_cents": nil, "opening_book_value_year": nil, "disposed_on": nil,
		"disposal_proceeds_cents": nil, "imported": false,
	}
	for k, v := range want {
		if created[k] != v {
			t.Errorf("created[%s] = %v, want %v", k, created[k], v)
		}
	}
	id, _ := created["id"].(string)

	rec := f.do(t, f.owner, http.MethodGet, assetsPath+"/"+id, nil)
	if rec.Code != http.StatusOK || profileDecodeMap(t, rec)["id"] != id {
		t.Errorf("GET status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = f.do(t, f.owner, http.MethodGet, assetsPath+"/"+id+"/schedule", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("schedule status = %d, body = %s", rec.Code, rec.Body.String())
	}
	schedule := profileDecodeMap(t, rec)
	if schedule["asset_id"] != id || schedule["method"] != "linear" {
		t.Errorf("schedule = %v", schedule)
	}
	years, _ := schedule["years"].([]any)
	// docs/api.md's example, worked out in internal/afa's tests.
	wantYears := [][4]float64{
		{2026, 149900, 41639, 108261},
		{2027, 108261, 49967, 58294},
		{2028, 58294, 49967, 8327},
		{2029, 8327, 8327, 0},
	}
	if len(years) != len(wantYears) {
		t.Fatalf("years = %v, want %d", years, len(wantYears))
	}
	for i, w := range wantYears {
		row, _ := years[i].(map[string]any)
		got := [4]float64{}
		for j, k := range []string{"year", "opening_book_value_cents", "depreciation_cents", "closing_book_value_cents"} {
			got[j], _ = row[k].(float64)
		}
		if got != w || row["euer_key"] != "euer.z33" {
			t.Errorf("year %d = %v, want %v on euer.z33", i, row, w)
		}
	}

	list := f.do(t, f.owner, http.MethodGet, assetsPath, nil)
	if got, _ := profileDecodeMap(t, list)["assets"].([]any); len(got) != 1 {
		t.Errorf("list = %s, want one asset", list.Body.String())
	}
}

func TestAssetsUnknownIDIs404(t *testing.T) {
	t.Parallel()

	f := newAssetFixture(t)
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, assetsPath + "/" + uuid.NewString(), nil},
		{http.MethodGet, assetsPath + "/not-a-uuid", nil},
		{http.MethodGet, assetsPath + "/" + uuid.NewString() + "/schedule", nil},
		{http.MethodPut, assetsPath + "/" + uuid.NewString(), laptopBody()},
		{http.MethodPost, assetsPath + "/" + uuid.NewString() + "/disposal", map[string]any{"disposed_on": "2026-04-01"}},
	} {
		if rec := f.do(t, f.owner, tc.method, tc.path, tc.body); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s status = %d, want 404, body = %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestAssetsRejectInvalidBodies(t *testing.T) {
	t.Parallel()

	with := func(edit func(b map[string]any)) map[string]any {
		b := laptopBody()
		edit(b)

		return b
	}

	tests := []struct {
		name    string
		body    map[string]any
		wantKey string
	}{
		{"gwg at 800.01", with(func(b map[string]any) {
			b["method"], b["cost_basis_cents"] = "gwg", 80001
			delete(b, "useful_life_months")
		}), "cost_basis_cents"},
		{"sammelposten at 250.00", with(func(b map[string]any) {
			b["method"], b["cost_basis_cents"] = "sammelposten", 25000
			delete(b, "useful_life_months")
		}), "cost_basis_cents"},
		{"sammelposten at 1000.01", with(func(b map[string]any) {
			b["method"], b["cost_basis_cents"] = "sammelposten", 100001
			delete(b, "useful_life_months")
		}), "cost_basis_cents"},
		{"immediate at 250.01", with(func(b map[string]any) {
			b["method"], b["cost_basis_cents"] = "immediate", 25001
			delete(b, "useful_life_months")
		}), "cost_basis_cents"},
		{"degressive", with(func(b map[string]any) { b["method"] = "degressive" }), "method"},
		{"unknown group", with(func(b map[string]any) { b["group"] = "buildings" }), "group"},
		{"linear without useful life", with(func(b map[string]any) { delete(b, "useful_life_months") }), "useful_life_months"},
		{"linear with useful life 0", with(func(b map[string]any) { b["useful_life_months"] = 0 }), "useful_life_months"},
		{"useful life on a gwg", with(func(b map[string]any) { b["method"], b["cost_basis_cents"] = "gwg", 50000 }), "useful_life_months"},
		{"zero cost", with(func(b map[string]any) { b["cost_basis_cents"] = 0 }), "cost_basis_cents"},
		{"blank name", with(func(b map[string]any) { b["name"] = "  " }), "name"},
		{"bad date", with(func(b map[string]any) { b["acquired_on"] = "10.03.2026" }), "acquired_on"},
		{"future date", with(func(b map[string]any) {
			b["acquired_on"] = time.Now().UTC().AddDate(0, 0, 2).Format("2006-01-02")
		}), "acquired_on"},
		{"bad expense id", with(func(b map[string]any) { b["expense_id"] = "x" }), "expense_id"},
		{"opening value without year", with(func(b map[string]any) { b["opening_book_value_cents"] = 1000 }), "opening_book_value_year"},
		{"opening year not after acquisition", with(func(b map[string]any) {
			b["opening_book_value_cents"], b["opening_book_value_year"] = 1000, 2026
		}), "opening_book_value_year"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAssetFixture(t)
			rec := f.do(t, f.owner, http.MethodPost, assetsPath, tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
			}
			if _, ok := profileErrorDetails(t, rec)[tt.wantKey]; !ok {
				t.Errorf("details = %v, want %q", profileErrorDetails(t, rec), tt.wantKey)
			}
			if n := len(fakeAssetsOf(t, f).assets); n != 0 {
				t.Errorf("a refused POST stored %d assets", n)
			}
		})
	}

	f := newAssetFixture(t)
	for _, field := range []string{"imported", "disposed_on", "id", "tenant_id"} {
		body := laptopBody()
		body[field] = "2026-04-01"
		if rec := f.do(t, f.owner, http.MethodPost, assetsPath, body); rec.Code != http.StatusBadRequest {
			t.Errorf("read only field %s: status = %d, want 400", field, rec.Code)
		}
	}
}

func TestAssetsThresholdsAccepted(t *testing.T) {
	t.Parallel()

	f := newAssetFixture(t)
	for _, tc := range []struct {
		method string
		cost   int
		key    string
	}{
		{"gwg", 80000, "euer.z36"},
		{"immediate", 25000, "euer.z36"},
		{"sammelposten", 25001, "euer.z37"},
		{"sammelposten", 100000, "euer.z37"},
	} {
		body := laptopBody()
		body["method"], body["cost_basis_cents"] = tc.method, tc.cost
		delete(body, "useful_life_months")
		created := createAsset(t, f, body)
		id, _ := created["id"].(string)

		rec := f.do(t, f.owner, http.MethodGet, assetsPath+"/"+id+"/schedule", nil)
		years, _ := profileDecodeMap(t, rec)["years"].([]any)
		first, _ := years[0].(map[string]any)
		if first["euer_key"] != tc.key {
			t.Errorf("%s %d: first year %v, want %s", tc.method, tc.cost, first, tc.key)
		}
	}
}

func TestAssetsUpdateWhileTheFirstYearIsOpen(t *testing.T) {
	t.Parallel()

	f := newAssetFixture(t)
	body := laptopBody()
	body["acquired_on"] = thisYear()
	created := createAsset(t, f, body)
	id, _ := created["id"].(string)

	body["name"] = "Laptop 14"
	body["useful_life_months"] = 12
	rec := f.do(t, f.owner, http.MethodPut, assetsPath+"/"+id, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", rec.Code, rec.Body.String())
	}
	got := profileDecodeMap(t, rec)
	if got["name"] != "Laptop 14" || got["useful_life_months"] != float64(12) || got["id"] != id {
		t.Errorf("updated = %v", got)
	}

	// Moving the asset into a year that has ended is refused too.
	body["acquired_on"] = lastYear()
	if rec := f.do(t, f.owner, http.MethodPut, assetsPath+"/"+id, body); rec.Code != http.StatusConflict {
		t.Errorf("PUT into a closed year status = %d, want 409, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAssetsUpdateAfterTheFirstYearIs409(t *testing.T) {
	t.Parallel()

	f := newAssetFixture(t)
	life := int32(36)
	acquired, _ := time.Parse("2006-01-02", lastYear())
	stored := fakeAssetsOf(t, f).put(storage.Asset{
		TenantID: f.tenant.ID, Name: "Desk", AcquiredOn: acquired, CostBasisCents: 120000,
		Method: storage.DepreciationLinear, Group: storage.AssetGroupOfficeEquipment, UsefulLifeMonths: &life,
	})

	body := laptopBody()
	body["acquired_on"] = thisYear()
	rec := f.do(t, f.owner, http.MethodPut, assetsPath+"/"+stored.ID.String(), body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body = %s", rec.Code, rec.Body.String())
	}
	if got, _ := fakeAssetsOf(t, f).FindByID(t.Context(), stored.ID); got.Name != "Desk" {
		t.Errorf("a refused PUT changed the asset: %+v", got)
	}

	// An imported asset's first schedule year is its opening year.
	opening, year := int64(60000), int32(time.Now().UTC().Year())
	imported := fakeAssetsOf(t, f).put(storage.Asset{
		TenantID: f.tenant.ID, Name: "Old desk", AcquiredOn: acquired.AddDate(-2, 0, 0), CostBasisCents: 120000,
		Method: storage.DepreciationLinear, Group: storage.AssetGroupOfficeEquipment, UsefulLifeMonths: &life,
		OpeningBookValueCents: &opening, OpeningBookValueYear: &year, Imported: true,
	})
	body = laptopBody()
	body["acquired_on"] = acquired.AddDate(-2, 0, 0).Format("2006-01-02")
	body["cost_basis_cents"] = 120000
	body["opening_book_value_cents"] = 50000
	body["opening_book_value_year"] = year
	rec = f.do(t, f.owner, http.MethodPut, assetsPath+"/"+imported.ID.String(), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT in the open opening year status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := profileDecodeMap(t, rec); got["imported"] != true || got["opening_book_value_cents"] != float64(50000) {
		t.Errorf("updated imported asset = %v", got)
	}
}

func TestAssetsDisposal(t *testing.T) {
	t.Parallel()

	f := newAssetFixture(t)
	body := laptopBody()
	body["acquired_on"] = "2025-01-15"
	body["cost_basis_cents"] = 120000
	created := createAsset(t, f, body)
	id, _ := created["id"].(string)
	path := assetsPath + "/" + id + "/disposal"

	for _, tc := range []struct {
		name    string
		body    map[string]any
		wantKey string
	}{
		{"before acquisition", map[string]any{"disposed_on": "2025-01-14"}, "disposed_on"},
		{"future", map[string]any{"disposed_on": time.Now().UTC().AddDate(0, 0, 2).Format("2006-01-02")}, "disposed_on"},
		{"bad date", map[string]any{"disposed_on": "soon"}, "disposed_on"},
		{"negative proceeds", map[string]any{"disposed_on": "2026-06-30", "proceeds_cents": -1}, "proceeds_cents"},
	} {
		rec := f.do(t, f.owner, http.MethodPost, path, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400, body = %s", tc.name, rec.Code, rec.Body.String())

			continue
		}
		if _, ok := profileErrorDetails(t, rec)[tc.wantKey]; !ok {
			t.Errorf("%s: details = %v, want %s", tc.name, profileErrorDetails(t, rec), tc.wantKey)
		}
	}

	rec := f.do(t, f.owner, http.MethodPost, path, map[string]any{"disposed_on": "2026-06-30", "proceeds_cents": 20000})
	if rec.Code != http.StatusOK {
		t.Fatalf("disposal status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := profileDecodeMap(t, rec); got["disposed_on"] != "2026-06-30" || got["disposal_proceeds_cents"] != float64(20000) {
		t.Errorf("disposed = %v", got)
	}

	if rec := f.do(t, f.owner, http.MethodPost, path, map[string]any{"disposed_on": "2026-07-01"}); rec.Code != http.StatusConflict {
		t.Errorf("second disposal status = %d, want 409", rec.Code)
	}

	// The schedule stops in June 2026: 40000 for 2025, 6/36 of 120000 =
	// 20000 for 2026.
	sched := profileDecodeMap(t, f.do(t, f.owner, http.MethodGet, assetsPath+"/"+id+"/schedule", nil))
	years, _ := sched["years"].([]any)
	if len(years) != 2 {
		t.Fatalf("years = %v, want 2", years)
	}
	last, _ := years[1].(map[string]any)
	if last["depreciation_cents"] != float64(20000) || last["closing_book_value_cents"] != float64(60000) {
		t.Errorf("disposal year = %v", last)
	}

	// A disposed asset is no longer edited.
	if rec := f.do(t, f.owner, http.MethodPut, assetsPath+"/"+id, laptopBody()); rec.Code != http.StatusConflict {
		t.Errorf("PUT after disposal status = %d, want 409", rec.Code)
	}
}

// TestAssetsSeeTheirOwnExpenseFake proves the hook order this file relies
// on: fake_expenses_test.go's hook sets Repositories.Expenses for every
// test, and the extras hook here runs after it. With extras, the asset
// routes must read the asset test's fake: an expense that exists only
// there is accepted. Without extras, they read the expense feature's
// own fake, which does not know it.
func TestAssetsSeeTheirOwnExpenseFake(t *testing.T) {
	t.Parallel()

	paid := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	only := storage.Expense{
		ID: uuid.New(), State: storage.BookingStateBooked, PaidOn: &paid,
		GrossCents: 80000, NetCents: 80000, IsAsset: true,
	}
	body := laptopBody()
	body["expense_id"] = only.ID.String()
	body["method"], body["cost_basis_cents"] = "gwg", 80000
	delete(body, "useful_life_months")

	seedRegime := func(t *testing.T, f profileFixture) {
		t.Helper()

		periods, ok := f.app.repos.VATRegimePeriods.(*fakeVATRegimes)
		if !ok {
			t.Fatalf("VATRegimePeriods is %T", f.app.repos.VATRegimePeriods)
		}
		if _, err := periods.Create(t.Context(), storage.VATRegimePeriod{
			TenantID: f.tenant.ID, ValidFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Regime: storage.VATRegimeKleinunternehmer,
		}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("with extras", func(t *testing.T) {
		t.Parallel()

		mine := &assetExpenses{expenses: map[uuid.UUID]storage.Expense{only.ID: only}}
		f := newAssetFixtureWith(t, assetExtras{expenses: mine})
		if got, ok := f.app.repos.Expenses.(*assetExpenses); !ok || got != mine {
			t.Fatalf("Repositories.Expenses is %T, want this test's assetExpenses", f.app.repos.Expenses)
		}
		seedRegime(t, f)
		if rec := f.do(t, f.owner, http.MethodPost, assetsPath, body); rec.Code != http.StatusCreated {
			t.Errorf("status = %d, want 201, body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("without extras", func(t *testing.T) {
		t.Parallel()

		f := newAssetFixture(t)
		if f.app.repos.Expenses == nil {
			t.Fatal("no expense repository: the expense feature's hook did not run")
		}
		if _, mine := f.app.repos.Expenses.(*assetExpenses); mine {
			t.Fatal("the asset fake leaked into a test that did not ask for it")
		}
		seedRegime(t, f)
		rec := f.do(t, f.owner, http.MethodPost, assetsPath, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
		}
		if got := profileErrorDetails(t, rec)["expense_id"]; got != "no such expense" {
			t.Errorf("details expense_id = %v, want no such expense", got)
		}
	})
}

func TestAssetsExpenseLink(t *testing.T) {
	t.Parallel()

	paid := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	expense := func(edit func(e *storage.Expense)) storage.Expense {
		e := storage.Expense{
			ID: uuid.New(), State: storage.BookingStateBooked, PaidOn: &paid,
			GrossCents: 95200, NetCents: 80000, VATRatePermyriad: 1900, VATCents: 15200, IsAsset: true,
		}
		if edit != nil {
			edit(&e)
		}

		return e
	}

	booked := expense(nil)
	draft := expense(func(e *storage.Expense) { e.State = storage.BookingStateDraft })
	plain := expense(func(e *storage.Expense) { e.IsAsset = false })
	storno := expense(func(e *storage.Expense) { id := uuid.New(); e.StornoOfID = &id })

	tests := []struct {
		name     string
		regime   storage.VATRegime
		expense  uuid.UUID
		cost     int
		method   string
		wantCode int
		wantKey  string
	}{
		{"Regelbesteuerung, net basis as a GWG", storage.VATRegimeStandard, booked.ID, 80000, "gwg", http.StatusCreated, ""},
		{"Regelbesteuerung refuses the gross price", storage.VATRegimeStandard, booked.ID, 95200, "sammelposten", http.StatusBadRequest, "cost_basis_cents"},
		{"Kleinunternehmer, gross basis as a Sammelposten", storage.VATRegimeKleinunternehmer, booked.ID, 95200, "sammelposten", http.StatusCreated, ""},
		{"Kleinunternehmer refuses the net price", storage.VATRegimeKleinunternehmer, booked.ID, 80000, "gwg", http.StatusBadRequest, "cost_basis_cents"},
		{"unknown expense", storage.VATRegimeStandard, uuid.New(), 80000, "gwg", http.StatusBadRequest, "expense_id"},
		{"draft expense", storage.VATRegimeStandard, draft.ID, 80000, "gwg", http.StatusBadRequest, "expense_id"},
		{"expense without is_asset", storage.VATRegimeStandard, plain.ID, 80000, "gwg", http.StatusBadRequest, "expense_id"},
		{"storno row", storage.VATRegimeStandard, storno.ID, 80000, "gwg", http.StatusBadRequest, "expense_id"},
		{"no VAT regime period", "", booked.ID, 80000, "gwg", http.StatusBadRequest, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			extras := assetExtras{expenses: &assetExpenses{expenses: map[uuid.UUID]storage.Expense{
				booked.ID: booked, draft.ID: draft, plain.ID: plain, storno.ID: storno,
			}}}
			f := newAssetFixtureWith(t, extras)
			if tt.regime != "" {
				periods, ok := f.app.repos.VATRegimePeriods.(*fakeVATRegimes)
				if !ok {
					t.Fatalf("VATRegimePeriods is %T", f.app.repos.VATRegimePeriods)
				}
				if _, err := periods.Create(t.Context(), storage.VATRegimePeriod{
					TenantID: f.tenant.ID, ValidFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Regime: tt.regime,
				}); err != nil {
					t.Fatal(err)
				}
			}

			body := laptopBody()
			body["expense_id"] = tt.expense.String()
			body["cost_basis_cents"] = tt.cost
			body["method"] = tt.method
			delete(body, "useful_life_months")

			rec := f.do(t, f.owner, http.MethodPost, assetsPath, body)
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.wantCode, rec.Body.String())
			}
			if tt.wantKey != "" {
				if _, ok := profileErrorDetails(t, rec)[tt.wantKey]; !ok {
					t.Errorf("details = %v, want %s", profileErrorDetails(t, rec), tt.wantKey)
				}
			}
			if tt.name == "no VAT regime period" {
				e, _ := profileDecodeMap(t, rec)["error"].(map[string]any)
				if e["code"] != "vat_regime_missing" {
					t.Errorf("error = %v, want vat_regime_missing", e)
				}
			}
			if tt.wantCode != http.StatusCreated {
				return
			}

			if got := profileDecodeMap(t, rec)["expense_id"]; got != tt.expense.String() {
				t.Errorf("expense_id = %v, want %s", got, tt.expense)
			}
			// One expense becomes at most one asset.
			if rec := f.do(t, f.owner, http.MethodPost, assetsPath, body); rec.Code != http.StatusConflict {
				t.Errorf("second asset for the expense: status = %d, want 409", rec.Code)
			}
		})
	}
}

// TestAssetsOpeningBookValue posts an asset with an opening book value
// and reads the continued schedule back. Hand computed in internal/afa's
// tests: 42000 over the 18 months left, 28000 and 14000.
func TestAssetsOpeningBookValue(t *testing.T) {
	t.Parallel()

	f := newAssetFixture(t)
	body := laptopBody()
	body["acquired_on"] = "2024-07-01"
	body["cost_basis_cents"] = 72000
	body["opening_book_value_cents"] = 42000
	body["opening_book_value_year"] = 2026
	created := createAsset(t, f, body)
	if created["imported"] != false {
		t.Errorf("imported = %v, want false: only the import of issue #81 sets it", created["imported"])
	}
	id, _ := created["id"].(string)

	sched := profileDecodeMap(t, f.do(t, f.owner, http.MethodGet, assetsPath+"/"+id+"/schedule", nil))
	years, _ := sched["years"].([]any)
	got := ""
	for _, y := range years {
		row, _ := y.(map[string]any)
		year, _ := row["year"].(float64)
		dep, _ := row["depreciation_cents"].(float64)
		got += strconv.Itoa(int(year)) + ":" + strconv.Itoa(int(dep)) + " "
	}
	if got != "2026:28000 2027:14000 " {
		t.Errorf("schedule = %q", got)
	}
}
