package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

type paymentResponse struct {
	ID                string  `json:"id"`
	DocumentID        string  `json:"document_id"`
	ReceivedOn        string  `json:"received_on"`
	AmountCents       int64   `json:"amount_cents"`
	Method            string  `json:"method"`
	Note              *string `json:"note"`
	ReversesPaymentID *string `json:"reverses_payment_id"`
	Imported          bool    `json:"imported"`
	CreatedAt         string  `json:"created_at"`
}

type paymentListResponse struct {
	Payments            []paymentResponse `json:"payments"`
	PaidCents           int64             `json:"paid_cents"`
	EffectiveDocumentID string            `json:"effective_document_id"`
	OpenCents           int64             `json:"open_cents"`
}

// kuInvoiceTotal is the grand total of docFixture.kuBody: 2.5 hours at
// 95.00 EUR, no VAT.
const kuInvoiceTotal = 23750

func paymentsPath(docID string) string { return documentsPath + "/" + docID + "/payments" }

func payBody(receivedOn string, amount int64) map[string]any {
	return map[string]any{"received_on": receivedOn, "amount_cents": amount, "method": "bank_transfer"}
}

func reversalBody(receivedOn string, amount int64, of string) map[string]any {
	body := payBody(receivedOn, amount)
	body["reverses_payment_id"] = of

	return body
}

func (f docFixture) issuedInvoice(t *testing.T) string {
	t.Helper()

	id := f.createID(t)
	f.mustIssue(t, id)

	return id
}

func (f docFixture) pay(t *testing.T, s profileSession, docID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()

	return f.do(t, s, http.MethodPost, paymentsPath(docID), body)
}

func (f docFixture) mustPay(t *testing.T, docID string, body map[string]any) paymentResponse {
	t.Helper()

	rec := f.pay(t, f.owner, docID, body)
	masterDataWantStatus(t, rec, http.StatusCreated)

	return masterDataDecode[paymentResponse](t, rec)
}

func (f docFixture) payments(t *testing.T, s profileSession, docID string) paymentListResponse {
	t.Helper()

	rec := f.do(t, s, http.MethodGet, paymentsPath(docID), nil)
	masterDataWantStatus(t, rec, http.StatusOK)

	return masterDataDecode[paymentListResponse](t, rec)
}

func (f docFixture) wantOpen(t *testing.T, docID string, paid, open int64) paymentListResponse {
	t.Helper()

	got := f.payments(t, f.owner, docID)
	if got.PaidCents != paid || got.OpenCents != open {
		t.Fatalf("paid_cents = %d, open_cents = %d, want %d and %d", got.PaidCents, got.OpenCents, paid, open)
	}

	return got
}

func TestPaymentsRequireASession(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	id := uuid.NewString()
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		if rec := doJSON(t, app.handler, method, paymentsPath(id), nil, nil, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s status = %d, want 401", method, rec.Code)
		}
	}
}

func TestPartialPaymentsAndTheOpenAmount(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "partial")
	invoice := f.issuedInvoice(t)
	f.wantOpen(t, invoice, 0, kuInvoiceTotal)

	note := "first instalment"
	body := payBody("2026-11-02", 10000)
	body["note"] = note
	first := f.mustPay(t, invoice, body)
	if first.DocumentID != invoice || first.ReceivedOn != "2026-11-02" || first.AmountCents != 10000 ||
		first.Method != "bank_transfer" || first.Note == nil || *first.Note != note || first.ReversesPaymentID != nil ||
		first.Imported || first.ID == "" || first.CreatedAt == "" {
		t.Fatalf("payment = %+v", first)
	}
	f.wantOpen(t, invoice, 10000, kuInvoiceTotal-10000)

	// The write locks the chain root, once, before it reads the chain.
	locksBefore := len(documentStore(t, f.app).Locked())
	f.mustPay(t, invoice, payBody("2026-11-03", kuInvoiceTotal-10000))
	if locked := documentStore(t, f.app).Locked(); len(locked) != locksBefore+1 || locked[len(locked)-1].String() != invoice {
		t.Fatalf("locks after the payment = %v, had %d before, want one more lock of the root %s", locked, locksBefore, invoice)
	}
	// The lock came before the read of the chain's payments: the read the
	// open amount is decided on already saw it.
	if seen := paymentRepo(t, f.app).locksAtLastList(); len(seen) != locksBefore+1 || seen[len(seen)-1].String() != invoice {
		t.Fatalf("locks when the payments were read = %v, had %d before, want the root locked first", seen, locksBefore)
	}
	got := f.wantOpen(t, invoice, kuInvoiceTotal, 0)
	if len(got.Payments) != 2 || got.Payments[0].ID != first.ID || got.EffectiveDocumentID != invoice {
		t.Fatalf("list = %+v", got)
	}

	// A member reads, but never records a payment.
	member := f.member(t)
	if list := f.payments(t, member, invoice); len(list.Payments) != 2 {
		t.Fatalf("member lists %d payments, want 2", len(list.Payments))
	}
	wantErrorCode(t, f.pay(t, member, invoice, payBody("2026-11-04", 1)), http.StatusForbidden, "permission_denied")
	if n := len(paymentRepo(t, f.app).all()); n != 2 {
		t.Fatalf("%d payments stored after a member's attempt, want 2", n)
	}

	// The open amount is derived on read: the stored rows carry none.
	for _, p := range paymentRepo(t, f.app).all() {
		if p.DocumentID.String() != invoice || p.TenantID != f.tenant.ID {
			t.Fatalf("stored payment = %+v", p)
		}
	}
}

// TestOverpaymentIsAcceptedAndRefunded: docs/api.md accepts an
// overpayment and shows it as a negative open_cents. A refund pays back at
// most the refund due.
func TestOverpaymentIsAcceptedAndRefunded(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "overpaid")
	invoice := f.issuedInvoice(t)

	// No refund before anything is due.
	masterDataWantField(t, f.pay(t, f.owner, invoice, payBody("2026-11-02", -100)), "amount_cents")

	f.mustPay(t, invoice, payBody("2026-11-02", kuInvoiceTotal+1250))
	f.wantOpen(t, invoice, kuInvoiceTotal+1250, -1250)

	masterDataWantField(t, f.pay(t, f.owner, invoice, payBody("2026-11-05", -1251)), "amount_cents")
	refund := f.mustPay(t, invoice, payBody("2026-11-05", -1250))
	if refund.AmountCents != -1250 || refund.ReversesPaymentID != nil {
		t.Fatalf("refund = %+v", refund)
	}
	f.wantOpen(t, invoice, kuInvoiceTotal, 0)

	masterDataWantField(t, f.pay(t, f.owner, invoice, payBody("2026-11-06", -1)), "amount_cents")

	// A refund recorded by mistake is undone by a positive reversal on the
	// refund's own day, once.
	masterDataWantField(t, f.pay(t, f.owner, invoice, reversalBody("2026-11-05", -1250, refund.ID)), "amount_cents")
	undo := f.mustPay(t, invoice, reversalBody("2026-11-05", 1250, refund.ID))
	if undo.AmountCents != 1250 || undo.ReversesPaymentID == nil || *undo.ReversesPaymentID != refund.ID {
		t.Fatalf("reversal of the refund = %+v", undo)
	}
	f.wantOpen(t, invoice, kuInvoiceTotal+1250, -1250)
	wantErrorCode(t, f.pay(t, f.owner, invoice, reversalBody("2026-11-05", 1250, refund.ID)), http.StatusConflict, "conflict")
}

func TestPaymentReversal(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "reversal")
	invoice := f.issuedInvoice(t)
	wrong := f.mustPay(t, invoice, payBody("2026-11-03", 10000))
	f.mustPay(t, invoice, payBody("2026-11-04", 5000))

	// The amount must be exactly the negated amount and the date the
	// reversed payment's own, neither earlier nor later.
	masterDataWantField(t, f.pay(t, f.owner, invoice, reversalBody("2026-11-03", -9999, wrong.ID)), "amount_cents")
	masterDataWantField(t, f.pay(t, f.owner, invoice, reversalBody("2026-11-03", 10000, wrong.ID)), "amount_cents")
	masterDataWantField(t, f.pay(t, f.owner, invoice, reversalBody("2026-11-02", -10000, wrong.ID)), "received_on")
	masterDataWantField(t, f.pay(t, f.owner, invoice, reversalBody("2026-11-04", -10000, wrong.ID)), "received_on")
	masterDataWantField(t, f.pay(t, f.owner, invoice, reversalBody("2026-11-03", -10000, "not-an-id")), "reverses_payment_id")

	reversal := f.mustPay(t, invoice, reversalBody("2026-11-03", -10000, wrong.ID))
	if reversal.ReversesPaymentID == nil || *reversal.ReversesPaymentID != wrong.ID || reversal.DocumentID != invoice ||
		reversal.ReceivedOn != "2026-11-03" {
		t.Fatalf("reversal = %+v", reversal)
	}
	f.wantOpen(t, invoice, 5000, kuInvoiceTotal-5000)

	// Reversed once only, and a reversal is never reversed.
	wantErrorCode(t, f.pay(t, f.owner, invoice, reversalBody("2026-11-03", -10000, wrong.ID)), http.StatusConflict, "conflict")
	wantErrorCode(t, f.pay(t, f.owner, invoice, reversalBody("2026-11-03", 10000, reversal.ID)), http.StatusConflict, "conflict")

	// A payment of another chain, or no payment at all, is refused.
	other := f.issuedInvoice(t)
	foreign := f.mustPay(t, other, payBody("2026-11-03", 700))
	wantErrorCode(t, f.pay(t, f.owner, invoice, reversalBody("2026-11-03", -700, foreign.ID)), http.StatusConflict, "conflict")
	wantErrorCode(t, f.pay(t, f.owner, invoice, reversalBody("2026-11-03", -700, uuid.NewString())), http.StatusConflict, "conflict")

	f.wantOpen(t, invoice, 5000, kuInvoiceTotal-5000)
	f.wantOpen(t, other, 700, kuInvoiceTotal-700)
}

func TestPaymentsBelongToIssuedInvoicesOnly(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "payable")
	draft := f.createID(t)

	offerBody := f.kuBody()
	offerBody["type"] = "offer"
	delete(offerBody, "service_date")
	offerBody["valid_until"] = "2026-12-31"
	draftOffer := f.create(t, f.owner, offerBody).str(t, "id")
	issuedOffer := f.create(t, f.owner, offerBody).str(t, "id")
	f.mustIssue(t, issuedOffer)

	for name, id := range map[string]string{"draft invoice": draft, "draft offer": draftOffer, "issued offer": issuedOffer} {
		t.Run(name, func(t *testing.T) {
			wantErrorCode(t, f.pay(t, f.owner, id, payBody("2026-11-02", 100)), http.StatusConflict, "conflict")
			wantErrorCode(t, f.do(t, f.owner, http.MethodGet, paymentsPath(id), nil), http.StatusConflict, "conflict")
		})
	}
	if n := len(paymentRepo(t, f.app).all()); n != 0 {
		t.Fatalf("%d payments stored, want none", n)
	}

	wantErrorCode(t, f.pay(t, f.owner, uuid.NewString(), payBody("2026-11-02", 100)), http.StatusNotFound, "not_found")
	wantErrorCode(t, f.do(t, f.owner, http.MethodGet, paymentsPath("not-a-uuid"), nil), http.StatusNotFound, "not_found")
}

// TestACorrectionIDResolvesToTheChainRoot pays through the id of an issued
// correction: the payment is stored against the original invoice, and
// both ids list the same payments.
func TestACorrectionIDResolvesToTheChainRoot(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "root")
	original := f.issuedInvoice(t)
	rec := f.do(t, f.owner, http.MethodPost, documentsPath+"/"+original+"/correction", nil)
	masterDataWantStatus(t, rec, http.StatusCreated)
	correction := decodeObject(t, rec).str(t, "id")

	// A correction draft is no member of the chain yet.
	wantErrorCode(t, f.pay(t, f.owner, correction, payBody("2026-11-02", 100)), http.StatusConflict, "conflict")

	f.mustIssue(t, correction)
	locksBefore := len(documentStore(t, f.app).Locked())
	paid := f.mustPay(t, correction, payBody("2026-11-02", 5000))
	if paid.DocumentID != original {
		t.Fatalf("document_id = %s, want the root %s", paid.DocumentID, original)
	}
	if locked := documentStore(t, f.app).Locked(); len(locked) != locksBefore+1 || locked[len(locked)-1].String() != original {
		t.Fatalf("locks = %v, had %d before, want one more lock of the root %s, not of the correction", locked, locksBefore, original)
	}
	for _, id := range []string{original, correction} {
		got := f.wantOpen(t, id, 5000, kuInvoiceTotal-5000)
		if got.EffectiveDocumentID != correction || len(got.Payments) != 1 || got.Payments[0].ID != paid.ID {
			t.Fatalf("list through %s = %+v", id, got)
		}
	}
}

// putIssued stores an issued document whose frozen payload carries the
// totals given, for a chain the API cannot produce: a correction that
// lowers the total.
func putIssued(t *testing.T, f docFixture, d storage.Document, grand, vat int64) storage.Document {
	t.Helper()

	payload, err := docpayload.Marshal(docpayload.FrozenPayloadV1{
		Version: docpayload.Version1,
		Totals:  docpayload.Totals{GrandTotalCents: grand, VATTotalCents: vat},
	})
	if err != nil {
		t.Fatal(err)
	}
	d.TenantID = f.tenant.ID
	d.CustomerID = uuid.MustParse(f.customer)
	d.State = storage.DocumentStateIssued
	d.VATRegime = storage.VATRegimeStandard
	d.FrozenPayload = payload

	return documentStore(t, f.app).Put(d)
}

// TestRefundAfterACorrection: the original of 119.00 EUR is paid in full,
// a correction lowers the total to 107.00 EUR. The open amount follows the
// effective document and is negative, and a refund settles it.
func TestRefundAfterACorrection(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "refund")
	issued := func(day string) *time.Time {
		d, err := time.Parse(time.DateOnly, day)
		if err != nil {
			t.Fatal(err)
		}

		return &d
	}
	original := putIssued(t, f, storage.Document{Type: storage.DocumentTypeInvoice, IssuedAt: issued("2026-11-01")}, 11900, 1900)
	f.mustPay(t, original.ID.String(), payBody("2026-11-05", 11900))
	f.wantOpen(t, original.ID.String(), 11900, 0)

	seq := int32(1)
	correction := putIssued(t, f, storage.Document{
		Type: storage.DocumentTypeCorrection, IssuedAt: issued("2026-11-10"),
		CorrectsDocumentID: &original.ID, CorrectionSeq: &seq,
	}, 10700, 700)

	got := f.wantOpen(t, correction.ID.String(), 11900, -1200)
	if got.EffectiveDocumentID != correction.ID.String() {
		t.Fatalf("effective_document_id = %s, want the correction", got.EffectiveDocumentID)
	}
	masterDataWantField(t, f.pay(t, f.owner, correction.ID.String(), payBody("2026-11-12", -1201)), "amount_cents")
	refund := f.mustPay(t, correction.ID.String(), payBody("2026-11-12", -1200))
	if refund.DocumentID != original.ID.String() {
		t.Fatalf("refund document_id = %s, want the root", refund.DocumentID)
	}
	f.wantOpen(t, original.ID.String(), 10700, 0)
}

func TestPaymentsOfAnotherTenantAnswer404(t *testing.T) {
	t.Parallel()

	a := newDocFixture(t, "pay-a")
	invoice := a.issuedInvoice(t)
	paid := a.mustPay(t, invoice, payBody("2026-11-02", 1000))
	b := addDocTenant(t, a.app, "pay-b")

	wantErrorCode(t, b.do(t, b.owner, http.MethodGet, paymentsPath(invoice), nil), http.StatusNotFound, "not_found")
	wantErrorCode(t, b.pay(t, b.owner, invoice, payBody("2026-11-02", 1000)), http.StatusNotFound, "not_found")
	wantErrorCode(t, b.pay(t, b.owner, invoice, reversalBody("2026-11-02", -1000, paid.ID)), http.StatusNotFound, "not_found")

	// B's own invoice can never reverse A's payment.
	own := b.issuedInvoice(t)
	wantErrorCode(t, b.pay(t, b.owner, own, reversalBody("2026-11-03", -1000, paid.ID)), http.StatusConflict, "conflict")

	if n := len(paymentRepo(t, a.app).all()); n != 1 {
		t.Fatalf("%d payments stored, want only A's one", n)
	}
	a.wantOpen(t, invoice, 1000, kuInvoiceTotal-1000)
}

// TestPaymentHandlerChecksTheDocumentTenantItself: a documents repository
// whose tenant filter is missing hands tenant B the invoice of tenant A.
// The handler still answers 404 and writes nothing.
func TestPaymentHandlerChecksTheDocumentTenantItself(t *testing.T) {
	t.Parallel()

	withLeakyDocuments(t)
	withPayloadChecker(t)
	a := addDocTenant(t, buildTestApp(t, 1000), "leak-doc-a")
	if _, ok := a.app.repos.Documents.(leakyDocuments); !ok {
		t.Fatalf("Documents = %T, want the leaky wrapper", a.app.repos.Documents)
	}
	invoice := a.issuedInvoice(t)
	a.mustPay(t, invoice, payBody("2026-11-02", 1000))
	b := addDocTenant(t, a.app, "leak-doc-b")

	wantErrorCode(t, b.do(t, b.owner, http.MethodGet, paymentsPath(invoice), nil), http.StatusNotFound, "not_found")
	wantErrorCode(t, b.pay(t, b.owner, invoice, payBody("2026-11-02", 1000)), http.StatusNotFound, "not_found")
	if n := len(paymentRepo(t, a.app).all()); n != 1 {
		t.Fatalf("%d payments stored, want only A's one", n)
	}
}

// TestPaymentHandlerChecksTheTenantItself: a repository whose tenant
// filter is missing returns another tenant's row of the same chain id.
// The handler leaves it out of the list and the sums.
func TestPaymentHandlerChecksTheTenantItself(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "leak")
	invoice := f.issuedInvoice(t)
	f.mustPay(t, invoice, payBody("2026-11-02", 1000))

	repo := paymentRepo(t, f.app)
	repo.put(storage.Payment{
		TenantID: uuid.New(), DocumentID: uuid.MustParse(invoice), ReceivedOn: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC),
		AmountCents: 5000, Method: storage.PaymentMethodCash,
	})
	repo.leak()

	got := f.wantOpen(t, invoice, 1000, kuInvoiceTotal-1000)
	if len(got.Payments) != 1 {
		t.Fatalf("list = %+v, want only the tenant's own payment", got.Payments)
	}
}

func TestPaymentBodyValidation(t *testing.T) {
	t.Parallel()

	f := newDocFixture(t, "validation")
	invoice := f.issuedInvoice(t)
	today := time.Now()

	with := func(key string, value any) map[string]any {
		body := payBody("2026-11-02", 100)
		body[key] = value

		return body
	}
	without := func(key string) map[string]any {
		body := payBody("2026-11-02", 100)
		delete(body, key)

		return body
	}

	for name, tc := range map[string]struct {
		body  map[string]any
		field string
	}{
		"no date":         {without("received_on"), "received_on"},
		"bad date":        {with("received_on", "03.12.2026"), "received_on"},
		"future date":     {with("received_on", today.AddDate(0, 0, 2).Format(time.DateOnly)), "received_on"},
		"ancient date":    {with("received_on", "1999-12-31"), "received_on"},
		"no amount":       {without("amount_cents"), "amount_cents"},
		"zero amount":     {with("amount_cents", 0), "amount_cents"},
		"too much":        {with("amount_cents", int64(100_000_000_001)), "amount_cents"},
		"no method":       {without("method"), "method"},
		"unknown method":  {with("method", "wire"), "method"},
		"note too long":   {with("note", strings.Repeat("ä", 1001)), "note"},
		"note with NUL":   {with("note", "paid\x00"), "note"},
		"note with tab":   {with("note", "paid\tcash"), "note"},
		"note with bidi":  {with("note", "paid"+string(rune(0x202e))+"cash"), "note"},
		"note with CRLF":  {with("note", "first\r\nsecond"), "note"},
		"note with LS":    {with("note", "first"+string(rune(0x2028))+"second"), "note"},
		"note with PS":    {with("note", "first"+string(rune(0x2029))+"second"), "note"},
		"wrong type":      {with("amount_cents", "100"), "amount_cents"},
		"bad reversal id": {with("reverses_payment_id", "x"), "reverses_payment_id"},
	} {
		t.Run(name, func(t *testing.T) {
			masterDataWantField(t, f.pay(t, f.owner, invoice, tc.body), tc.field)
		})
	}

	// Unknown and server owned fields are refused, never ignored.
	for _, key := range []string{"tenant_id", "document_id", "imported", "id", "created_at", "vat_cents"} {
		wantErrorCode(t, f.pay(t, f.owner, invoice, with(key, uuid.NewString())), http.StatusBadRequest, "invalid_argument")
	}
	if n := len(paymentRepo(t, f.app).all()); n != 0 {
		t.Fatalf("%d payments stored, want none", n)
	}

	// Today itself is accepted.
	f.mustPay(t, invoice, with("received_on", time.Now().In(berlinForTest(t)).Format(time.DateOnly)))

	// A note of exactly 1000 characters (runes, not bytes) and a line
	// break are accepted.
	long := f.mustPay(t, invoice, with("note", strings.Repeat("ä", 1000)))
	if long.Note == nil || len([]rune(*long.Note)) != 1000 {
		t.Fatalf("note = %v, want 1000 characters", long.Note)
	}
	multiline := f.mustPay(t, invoice, with("note", "first line\nsecond line"))
	if multiline.Note == nil || *multiline.Note != "first line\nsecond line" {
		t.Fatalf("note = %v, want both lines", multiline.Note)
	}
}

// TestReceivedOnEndsAtMidnightInBerlin: at 23:30 UTC on 21 November it is
// already 22 November in Berlin (UTC+1). The 22nd is today and accepted,
// the 23rd is the future and refused. Not parallel: it sets the clock of
// the payment routes, which is package state.
func TestReceivedOnEndsAtMidnightInBerlin(t *testing.T) {
	restore := httpapi.SetPaymentsNow(func() time.Time { return time.Date(2026, 11, 21, 23, 30, 0, 0, time.UTC) })
	t.Cleanup(restore)

	f := newDocFixture(t, "midnight")
	invoice := f.issuedInvoice(t)
	masterDataWantField(t, f.pay(t, f.owner, invoice, payBody("2026-11-23", 100)), "received_on")
	if got := f.mustPay(t, invoice, payBody("2026-11-22", 100)); got.ReceivedOn != "2026-11-22" {
		t.Fatalf("received_on = %s, want 2026-11-22", got.ReceivedOn)
	}

	// In summer Berlin is UTC+2: at 22:30 UTC on 30 June it is 1 July.
	restoreSummer := httpapi.SetPaymentsNow(func() time.Time { return time.Date(2026, 6, 30, 22, 30, 0, 0, time.UTC) })
	defer restoreSummer()
	masterDataWantField(t, f.pay(t, f.owner, invoice, payBody("2026-07-02", 100)), "received_on")
	f.mustPay(t, invoice, payBody("2026-07-01", 100))
}

func berlinForTest(t *testing.T) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}

	return loc
}
