package httpapi_test

import (
	"encoding/csv"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// The report tests use hand computed ledgers. Every expected figure is
// worked out in the comment next to it, never by the code under test.

type euerSourceResponse struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	Date        string `json:"date"`
	AmountCents int64  `json:"amount_cents"`
}

type euerLineResponse struct {
	Key         string               `json:"key"`
	Zeile       int                  `json:"zeile"`
	Kennzahl    string               `json:"kennzahl"`
	Operator    string               `json:"operator"`
	Verified    bool                 `json:"verified"`
	LabelDE     string               `json:"label_de"`
	LabelEN     string               `json:"label_en"`
	AmountCents int64                `json:"amount_cents"`
	Sources     []euerSourceResponse `json:"sources"`
}

type euerBoundaryResponse struct {
	Kind            string `json:"kind"`
	ID              string `json:"id"`
	Date            string `json:"date"`
	AmountCents     int64  `json:"amount_cents"`
	AssignedTaxYear *int   `json:"assigned_tax_year"`
}

type euerWarningResponse struct {
	Code       string   `json:"code"`
	Kind       string   `json:"kind"`
	ID         string   `json:"id"`
	RelatedIDs []string `json:"related_ids"`
}

type euerReportResponse struct {
	Year    int `json:"year"`
	Mapping struct {
		TaxYear int    `json:"tax_year"`
		Status  string `json:"status"`
		Source  string `json:"source"`
	} `json:"mapping"`
	VATRegimes []struct {
		ValidFrom string `json:"valid_from"`
		Regime    string `json:"regime"`
	} `json:"vat_regimes"`
	Lines           []euerLineResponse     `json:"lines"`
	BoundaryEntries []euerBoundaryResponse `json:"boundary_entries"`
	Warnings        []euerWarningResponse  `json:"warnings"`
	Disclaimer      string                 `json:"disclaimer"`
}

// line returns the line of key, failing when there is none.
func (r euerReportResponse) line(t *testing.T, key string) euerLineResponse {
	t.Helper()

	for _, l := range r.Lines {
		if l.Key == key {
			return l
		}
	}
	t.Fatalf("no line %q in %+v", key, r.Lines)

	return euerLineResponse{}
}

// amounts maps every line key to its amount.
func (r euerReportResponse) amounts() map[string]int64 {
	out := map[string]int64{}
	for _, l := range r.Lines {
		out[l.Key] = l.AmountCents
	}

	return out
}

type assignmentResponse struct {
	ID           string  `json:"id"`
	EntryKind    string  `json:"entry_kind"`
	EntryID      string  `json:"entry_id"`
	TaxYear      int     `json:"tax_year"`
	Note         *string `json:"note"`
	SupersedesID *string `json:"supersedes_id"`
	CreatedAt    string  `json:"created_at"`
}

const (
	euerReportPath    = "/api/v1/reports/euer/"
	assignmentsPath   = "/api/v1/euer-assignments"
	mapping2026Source = "copy of 2025"
)

// report2026 reads the report of 2026, the first year solobill reports.
func report2026(t *testing.T, f docFixture, s profileSession) euerReportResponse {
	t.Helper()

	rec := f.do(t, s, http.MethodGet, euerReportPath+"2026", nil)
	masterDataWantStatus(t, rec, http.StatusOK)

	return masterDataDecode[euerReportResponse](t, rec)
}

func assignmentBody(kind, id string, year int) map[string]any {
	return map[string]any{"entry_kind": kind, "entry_id": id, "tax_year": year}
}

func (f docFixture) mustAssign(t *testing.T, body map[string]any) assignmentResponse {
	t.Helper()

	rec := f.do(t, f.owner, http.MethodPost, assignmentsPath, body)
	masterDataWantStatus(t, rec, http.StatusCreated)

	return masterDataDecode[assignmentResponse](t, rec)
}

func (f docFixture) assignments(t *testing.T, query string) []assignmentResponse {
	t.Helper()

	rec := f.do(t, f.owner, http.MethodGet, assignmentsPath+query, nil)
	masterDataWantStatus(t, rec, http.StatusOK)

	return masterDataDecode[struct {
		Assignments []assignmentResponse `json:"assignments"`
	}](t, rec).Assignments
}

// putKUInvoice stores an issued Kleinunternehmer invoice of gross cents,
// issued on day, so a payment of an earlier year can be recorded against
// it through the API.
func putKUInvoice(t *testing.T, f docFixture, day string, gross int64) storage.Document {
	t.Helper()

	payload, err := docpayload.Marshal(docpayload.FrozenPayloadV1{
		Version: docpayload.Version1,
		Totals:  docpayload.Totals{GrandTotalCents: gross},
	})
	if err != nil {
		t.Fatal(err)
	}
	issued := mustDay(t, day)

	return documentStore(t, f.app).Put(storage.Document{
		TenantID:      f.tenant.ID,
		CustomerID:    uuid.MustParse(f.customer),
		Type:          storage.DocumentTypeInvoice,
		State:         storage.DocumentStateIssued,
		VATRegime:     storage.VATRegimeKleinunternehmer,
		IssuedAt:      &issued,
		FrozenPayload: payload,
	})
}

func mustDay(t *testing.T, day string) time.Time {
	t.Helper()

	d, err := time.Parse(time.DateOnly, day)
	if err != nil {
		t.Fatal(err)
	}

	return d
}

// bookedExpense creates and books body through the API.
func (f docFixture) bookedExpense(t *testing.T, body map[string]any) expenseResponse {
	t.Helper()

	rec := f.do(t, f.owner, http.MethodPost, "/api/v1/expenses", body)
	masterDataWantStatus(t, rec, http.StatusCreated)
	id := masterDataDecode[expenseResponse](t, rec).ID
	rec = f.do(t, f.owner, http.MethodPost, "/api/v1/expenses/"+id.String()+"/book", nil)
	masterDataWantStatus(t, rec, http.StatusOK)

	return masterDataDecode[expenseResponse](t, rec)
}

func TestEuerRoutesRequireASession(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, euerReportPath + "2026"},
		{http.MethodGet, euerReportPath + "2026/csv"},
		{http.MethodGet, assignmentsPath},
		{http.MethodPost, assignmentsPath},
	} {
		if rec := doJSON(t, app.handler, route.method, route.path, nil, nil, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s status = %d, want 401", route.method, route.path, rec.Code)
		}
	}
}

// TestEuerReportOfAKleinunternehmer: the owner's own case.
//
// Invoice of 237.50 EUR issued on 1 December 2025, paid with 100.00 EUR
// on 5 January 2026 and 137.50 EUR on 10 March 2026. An expense of 59.50
// EUR (Arbeitsmittel, Zeile 51) paid on 1 June 2026 without a receipt.
//
// 2026, no assignment: Zeile 12 = 100.00 + 137.50 = 237.50, Zeile 51 =
// 59.50, gross, no VAT line. The payment of 5 January lies in the window
// and is listed unassigned.
//
// Assigned to 2025: Zeile 12 = 137.50. The changed decision back to 2026
// restores 237.50.
func TestEuerReportOfAKleinunternehmer(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "euer-ku")
	invoice := putKUInvoice(t, f, "2025-12-01", 23750).ID.String()
	january := f.mustPay(t, invoice, payBody("2026-01-05", 10000))
	march := f.mustPay(t, invoice, payBody("2026-03-10", 13750))
	paper := f.bookedExpense(t, kleinExpense("2026-06-01"))

	r := report2026(t, f, f.owner)
	if r.Year != 2026 || r.Mapping.TaxYear != 2026 || r.Mapping.Status != "unverified" || r.Mapping.Source != mapping2026Source {
		t.Errorf("year %d, mapping %+v, want 2026 and the unverified copy of 2025", r.Year, r.Mapping)
	}
	if r.Disclaimer == "" {
		t.Error("the report carries no disclaimer")
	}
	if len(r.VATRegimes) != 1 || r.VATRegimes[0].Regime != "kleinunternehmer" || r.VATRegimes[0].ValidFrom != "2000-01-01" {
		t.Errorf("vat_regimes = %+v, want kleinunternehmer since 2000-01-01", r.VATRegimes)
	}
	if got, want := r.amounts(), map[string]int64{"euer.z12": 23750, "euer.z51": 5950}; !maps.Equal(got, want) {
		t.Fatalf("lines = %v, want %v", got, want)
	}
	z12 := r.line(t, "euer.z12")
	if z12.Zeile != 12 || z12.Kennzahl != "111" || z12.Verified || z12.Operator != "" || z12.LabelDE == "" || z12.LabelEN == "" {
		t.Errorf("Zeile 12 = %+v, want Kz 111, unverified in 2026, labelled", z12)
	}
	wantSources := []euerSourceResponse{
		{Kind: "payment", ID: january.ID, Date: "2026-01-05", AmountCents: 10000},
		{Kind: "payment", ID: march.ID, Date: "2026-03-10", AmountCents: 13750},
	}
	if !slices.Equal(z12.Sources, wantSources) {
		t.Errorf("Zeile 12 sources = %+v, want %+v", z12.Sources, wantSources)
	}
	if got := r.line(t, "euer.z51").Sources; len(got) != 1 || got[0].ID != paper.ID.String() || got[0].Kind != "expense" {
		t.Errorf("Zeile 51 sources = %+v, want the expense %s", got, paper.ID)
	}
	wantBoundary := []euerBoundaryResponse{{Kind: "payment", ID: january.ID, Date: "2026-01-05", AmountCents: 10000}}
	if !boundaryEqual(r.BoundaryEntries, wantBoundary) {
		t.Errorf("boundary_entries = %+v, want %+v", r.BoundaryEntries, wantBoundary)
	}
	wantWarnings := []string{"missing_receipt expense " + paper.ID.String(), "unassigned_boundary_entry payment " + january.ID}
	if got := warningKeys(r); !slices.Equal(got, wantWarnings) {
		t.Errorf("warnings = %v, want %v", got, wantWarnings)
	}

	// A member reads the report but never assigns.
	member := f.member(t)
	report2026(t, f, member)
	wantErrorCode(t, f.do(t, member, http.MethodPost, assignmentsPath, assignmentBody("payment", january.ID, 2025)),
		http.StatusForbidden, "permission_denied")

	body := assignmentBody("payment", january.ID, 2025)
	body["note"] = "Rechnung Dezember"
	first := f.mustAssign(t, body)
	if first.EntryKind != "payment" || first.EntryID != january.ID || first.TaxYear != 2025 ||
		first.Note == nil || *first.Note != "Rechnung Dezember" || first.SupersedesID != nil {
		t.Fatalf("assignment = %+v", first)
	}

	r = report2026(t, f, f.owner)
	if got := r.line(t, "euer.z12").AmountCents; got != 13750 {
		t.Errorf("Zeile 12 after the assignment to 2025 = %d, want 13750", got)
	}
	if b := r.BoundaryEntries; len(b) != 1 || b[0].AssignedTaxYear == nil || *b[0].AssignedTaxYear != 2025 {
		t.Errorf("boundary_entries = %+v, want the payment assigned to 2025", b)
	}
	if got := warningKeys(r); !slices.Equal(got, wantWarnings[:1]) {
		t.Errorf("warnings = %v, want only the missing receipt", got)
	}

	// A second decision must name the first.
	wantErrorCode(t, f.do(t, f.owner, http.MethodPost, assignmentsPath, assignmentBody("payment", january.ID, 2026)),
		http.StatusConflict, "conflict")
	again := assignmentBody("payment", january.ID, 2026)
	again["supersedes_id"] = first.ID
	second := f.mustAssign(t, again)
	if second.SupersedesID == nil || *second.SupersedesID != first.ID {
		t.Fatalf("second = %+v, want it to supersede %s", second, first.ID)
	}
	if got := f.assignments(t, ""); len(got) != 1 || got[0].ID != second.ID {
		t.Errorf("assignments = %+v, want only the effective %s", got, second.ID)
	}
	if got := f.assignments(t, "?tax_year=2025"); len(got) != 0 {
		t.Errorf("assignments of 2025 = %+v, want none", got)
	}
	if got := report2026(t, f, f.owner).line(t, "euer.z12").AmountCents; got != 23750 {
		t.Errorf("Zeile 12 after the changed decision = %d, want 23750", got)
	}
	// Append-only: both rows are kept.
	if n := len(assignmentRepo(t, f.app).all()); n != 2 {
		t.Errorf("%d assignment rows stored, want 2", n)
	}
}

// TestEuerReportUnderTheRegelbesteuerungWithARefund: the original of
// 119.00 EUR (100.00 net, 19.00 VAT) issued on 1 February 2026 is paid in
// full on 5 February. A correction of 1 March lowers it to 107.00 EUR
// (100.00 net, 7.00 VAT), and 12.00 EUR are refunded on 5 March.
//
// The payment keeps the ratio of the original: 100.00 net, 19.00 VAT.
// The refund takes the ratio of the correction, rounded cumulatively over
// the chain: 19.00 + (-12.00 * 7 / 107 = -0.78504...) = 18.21495... is
// 18.21, so the refund's VAT is 18.21 - 19.00 = -0.79 and its net -12.00
// + 0.79 = -11.21.
//
// Zeile 15 = 100.00 - 11.21 = 88.79, Zeile 17 = 19.00 - 0.79 = 18.21.
// An expense of 119.00 EUR gross on 1 June: 100.00 on Zeile 51, 19.00 on
// Zeile 57.
func TestEuerReportUnderTheRegelbesteuerungWithARefund(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "euer-std")
	f.setPeriodRegime(t, "2026-01-01", storage.VATRegimeStandard)
	issued := func(day string) *time.Time { d := mustDay(t, day); return &d }

	original := putIssued(t, f, storage.Document{Type: storage.DocumentTypeInvoice, IssuedAt: issued("2026-02-01")}, 11900, 1900)
	paid := f.mustPay(t, original.ID.String(), payBody("2026-02-05", 11900))
	seq := int32(1)
	correction := putIssued(t, f, storage.Document{
		Type: storage.DocumentTypeCorrection, IssuedAt: issued("2026-03-01"),
		CorrectsDocumentID: &original.ID, CorrectionSeq: &seq,
	}, 10700, 700)
	refund := f.mustPay(t, correction.ID.String(), payBody("2026-03-05", -1200))
	f.bookedExpense(t, standardExpense("2026-06-01"))

	r := report2026(t, f, f.owner)
	want := map[string]int64{"euer.z15": 8879, "euer.z17": 1821, "euer.z51": 10000, "euer.z57": 1900}
	if got := r.amounts(); !maps.Equal(got, want) {
		t.Fatalf("lines = %v, want %v", got, want)
	}
	z17 := r.line(t, "euer.z17").Sources
	wantVAT := []euerSourceResponse{
		{Kind: "payment", ID: paid.ID, Date: "2026-02-05", AmountCents: 1900},
		{Kind: "payment", ID: refund.ID, Date: "2026-03-05", AmountCents: -79},
	}
	if !slices.Equal(z17, wantVAT) {
		t.Errorf("Zeile 17 sources = %+v, want %+v", z17, wantVAT)
	}
	if len(r.VATRegimes) != 1 || r.VATRegimes[0].Regime != "standard" || r.VATRegimes[0].ValidFrom != "2026-01-01" {
		t.Errorf("vat_regimes = %+v, want standard from 2026-01-01", r.VATRegimes)
	}
}

// TestEuerReportOfTheCommutingLinesAndAStorno: 1,200.00 EUR on Zeile 72
// and 900.00 EUR on Zeile 73, booked without a receipt, keep their
// amounts and carry the form's operator, and raise no missing receipt.
// The paper of 59.50 EUR without a receipt is reversed on 2 July: Zeile
// 51 is 59.50 - 59.50 = 0.00, and one warning on the original names the
// storno.
func TestEuerReportOfTheCommutingLinesAndAStorno(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "euer-car")
	car := func(key string, cents int64) map[string]any {
		body := withField(withField(kleinExpense("2026-06-30"), "category", key), "gross_cents", cents)

		return withField(body, "net_cents", cents)
	}
	f.bookedExpense(t, car("euer.z72", 120000))
	f.bookedExpense(t, car("euer.z73", 90000))
	paper := f.bookedExpense(t, kleinExpense("2026-06-01"))
	rec := f.do(t, f.owner, http.MethodPost, "/api/v1/expenses/"+paper.ID.String()+"/storno", map[string]any{"paid_on": "2026-07-02"})
	masterDataWantStatus(t, rec, http.StatusCreated)
	storno := masterDataDecode[expenseResponse](t, rec)

	r := report2026(t, f, f.owner)
	if got, want := r.amounts(), map[string]int64{"euer.z51": 0, "euer.z72": 120000, "euer.z73": 90000}; !maps.Equal(got, want) {
		t.Fatalf("lines = %v, want %v", got, want)
	}
	for key, want := range map[string]struct{ kz, op string }{"euer.z72": {"142", "-"}, "euer.z73": {"176", "+"}, "euer.z51": {"229", ""}} {
		if l := r.line(t, key); l.Kennzahl != want.kz || l.Operator != want.op {
			t.Errorf("%s = Kz %q operator %q, want %q %q", key, l.Kennzahl, l.Operator, want.kz, want.op)
		}
	}
	want := []euerWarningResponse{{Code: "missing_receipt", Kind: "expense", ID: paper.ID.String(), RelatedIDs: []string{storno.ID.String()}}}
	if len(r.Warnings) != 1 || r.Warnings[0].Code != want[0].Code || r.Warnings[0].ID != want[0].ID ||
		!slices.Equal(r.Warnings[0].RelatedIDs, want[0].RelatedIDs) {
		t.Errorf("warnings = %+v, want %+v", r.Warnings, want)
	}
}

func TestEuerReportCSV(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "euer-csv")
	invoice := putKUInvoice(t, f, "2026-02-01", 23750).ID.String()
	first := f.mustPay(t, invoice, payBody("2026-02-10", 10000))
	second := f.mustPay(t, invoice, payBody("2026-03-10", 13750))

	rec := f.do(t, f.owner, http.MethodGet, euerReportPath+"2026/csv", nil)
	masterDataWantStatus(t, rec, http.StatusOK)
	if ct := rec.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="euer-2026.csv"` {
		t.Errorf("Content-Disposition = %q", cd)
	}

	rd := csv.NewReader(strings.NewReader(rec.Body.String()))
	rd.Comma = ';'
	rows, err := rd.ReadAll()
	if err != nil {
		t.Fatalf("read the csv: %v", err)
	}
	want := [][]string{
		{"key", "zeile", "kennzahl", "operator", "label", "amount", "source_kind", "source_id", "source_amount"},
		{"euer.z12", "12", "111", "", rows[1][4], "237,50", "payment", first.ID, "100,00"},
		{"euer.z12", "12", "111", "", rows[1][4], "237,50", "payment", second.ID, "137,50"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %q, want %q", rows, want)
	}
	for i := range want {
		if !slices.Equal(rows[i], want[i]) {
			t.Errorf("row %d = %q, want %q", i, rows[i], want[i])
		}
	}
	if rows[1][4] == "" {
		t.Error("the label is empty")
	}
}

func TestEuerReportRefusals(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "euer-refuse")
	tests := []struct {
		name   string
		path   string
		status int
		code   string
	}{
		{"archive year", euerReportPath + "2025", http.StatusNotFound, "not_found"},
		{"first imported year", euerReportPath + "2023", http.StatusNotFound, "not_found"},
		{"archive year csv", euerReportPath + "2024/csv", http.StatusNotFound, "not_found"},
		{"year without a mapping", euerReportPath + "2027", http.StatusNotFound, "not_found"},
		{"no number", euerReportPath + "abcd", http.StatusNotFound, "not_found"},
		{"five digits", euerReportPath + "02026", http.StatusNotFound, "not_found"},
		{"bad tax_year filter", assignmentsPath + "?tax_year=26", http.StatusBadRequest, "invalid_argument"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wantErrorCode(t, f.do(t, f.owner, http.MethodGet, tt.path, nil), tt.status, tt.code)
		})
	}
}

// TestEuerReportWithoutARegimePeriod: a tenant without any VAT regime
// period has no report.
func TestEuerReportWithoutARegimePeriod(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	c := newMasterDataCaller(t, app, "euer-noregime")
	rec := c.do(t, app, http.MethodGet, euerReportPath+"2026", nil)
	masterDataWantStatus(t, rec, http.StatusBadRequest)
	if got := masterDataDecode[masterDataErrorBody](t, rec).Error.Code; got != "vat_regime_missing" {
		t.Errorf("code = %q, want vat_regime_missing", got)
	}
}

// TestEuerReportKeepsToItsTenant: with the tenant filter of the payments
// and expenses fakes switched off, as a broken row level security would,
// tenant B's report still shows none of A's rows.
func TestEuerReportKeepsToItsTenant(t *testing.T) {
	t.Parallel()

	a := newDocFixture(t, "euer-a")
	invoice := putKUInvoice(t, a, "2026-02-01", 23750).ID.String()
	a.mustPay(t, invoice, payBody("2026-02-10", 23750))
	a.bookedExpense(t, kleinExpense("2026-06-01"))

	b := addDocTenant(t, a.app, "euer-b")
	b.setPeriodRegime(t, "2001-01-01", storage.VATRegimeKleinunternehmer)
	paymentRepo(t, a.app).leak()
	expenseRepo(t, a.app).leak()

	r := report2026(t, b, b.owner)
	if len(r.Lines) != 0 || len(r.Warnings) != 0 {
		t.Errorf("B's report = lines %+v, warnings %+v, want nothing of A", r.Lines, r.Warnings)
	}
	if len(r.VATRegimes) != 1 || r.VATRegimes[0].ValidFrom != "2001-01-01" {
		t.Errorf("B's regimes = %+v, want only its own period", r.VATRegimes)
	}
	// A's rows are there.
	if got := report2026(t, a, a.owner).amounts()["euer.z12"]; got != 23750 {
		t.Errorf("A's Zeile 12 = %d, want 23750", got)
	}
}

func TestEuerAssignmentRefusals(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "euer-assign")
	invoice := putKUInvoice(t, f, "2025-12-01", 50000).ID.String()
	january := f.mustPay(t, invoice, payBody("2026-01-05", 10000))
	march := f.mustPay(t, invoice, payBody("2026-03-10", 10000))
	reversed := f.mustPay(t, invoice, payBody("2026-01-06", 500))
	reversal := f.mustPay(t, invoice, reversalBody("2026-01-06", -500, reversed.ID))
	december := f.bookedExpense(t, kleinExpense("2025-12-28"))
	asset := f.bookedExpense(t, withField(kleinExpense("2025-12-29"), "is_asset", true))
	june := f.bookedExpense(t, kleinExpense("2026-06-01"))

	other := addDocTenant(t, f.app, "euer-assign-b")
	foreign := other.bookedExpense(t, kleinExpense("2025-12-30"))

	tests := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"empty body", map[string]any{}, "entry_kind"},
		{"no entry id", map[string]any{"entry_kind": "payment", "tax_year": 2026}, "entry_id"},
		{"no tax year", map[string]any{"entry_kind": "payment", "entry_id": january.ID}, "tax_year"},
		{"unknown kind", assignmentBody("asset", january.ID, 2026), "entry_kind"},
		{"entry id no uuid", assignmentBody("payment", "nope", 2026), "entry_id"},
		{"tax year out of range", assignmentBody("payment", january.ID, 1999), "tax_year"},
		{"bad supersedes id", withField(assignmentBody("payment", january.ID, 2026), "supersedes_id", "nope"), "supersedes_id"},
		{"note too long", withField(assignmentBody("payment", january.ID, 2026), "note", strings.Repeat("x", 1001)), "note"},
		{"payment outside the window", assignmentBody("payment", march.ID, 2025), "entry_id"},
		{"expense outside the window", assignmentBody("expense", june.ID.String(), 2026), "entry_id"},
		{"not the neighbouring year", assignmentBody("payment", january.ID, 2027), "entry_id"},
		{"wrong year of a december entry", assignmentBody("expense", december.ID.String(), 2024), "tax_year"},
		{"a reversal", assignmentBody("payment", reversal.ID, 2025), "entry_id"},
		{"an asset expense", assignmentBody("expense", asset.ID.String(), 2026), "entry_id"},
		{"another tenant's expense", assignmentBody("expense", foreign.ID.String(), 2026), "entry_id"},
		{"unknown other income", assignmentBody("other_income", uuid.NewString(), 2026), "entry_id"},
	}
	// Sequential: the count below needs every refusal done.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			masterDataWantField(t, f.do(t, f.owner, http.MethodPost, assignmentsPath, tt.body), tt.field)
		})
	}
	if n := len(assignmentRepo(t, f.app).all()); n != 0 {
		t.Fatalf("%d assignments stored, want none", n)
	}

	// The December expense goes to the next year.
	if got := f.mustAssign(t, assignmentBody("expense", december.ID.String(), 2026)); got.TaxYear != 2026 {
		t.Errorf("assignment = %+v", got)
	}
	// A supersedes_id that names no effective row of the entry.
	body := withField(assignmentBody("payment", january.ID, 2025), "supersedes_id", uuid.NewString())
	wantErrorCode(t, f.do(t, f.owner, http.MethodPost, assignmentsPath, body), http.StatusConflict, "conflict")
}

func warningKeys(r euerReportResponse) []string {
	out := make([]string, 0, len(r.Warnings))
	for _, w := range r.Warnings {
		out = append(out, w.Code+" "+w.Kind+" "+w.ID)
	}

	return out
}

func boundaryEqual(got, want []euerBoundaryResponse) bool {
	return slices.EqualFunc(got, want, func(a, b euerBoundaryResponse) bool {
		sameYear := (a.AssignedTaxYear == nil) == (b.AssignedTaxYear == nil) &&
			(a.AssignedTaxYear == nil || *a.AssignedTaxYear == *b.AssignedTaxYear)

		return a.Kind == b.Kind && a.ID == b.ID && a.Date == b.Date && a.AmountCents == b.AmountCents && sameYear
	})
}
