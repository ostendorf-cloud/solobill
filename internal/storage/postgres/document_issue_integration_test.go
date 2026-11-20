//go:build integration

// Issue #70: issuing against a real PostgreSQL, through the restricted,
// NOBYPASSRLS application role. Same reasons as integration_test.go's own
// build tag comment.
package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostendorf-cloud/solobill/internal/docissue"
	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres"
)

// issueDraftForTest issues id the way docissue.Issuer orders it, without
// its checks: lock the draft, allocate the next number, write. The
// payload is testFrozenPayload's fixed bytes.
func issueDraftForTest(ctx context.Context, store *postgres.Store, id uuid.UUID, docType storage.DocumentType, year int) (storage.Document, error) {
	if _, err := store.Documents().Lock(ctx, id); err != nil {
		return storage.Document{}, err
	}
	n, err := store.DocumentNumbers().AllocateNext(ctx, docType, year)
	if err != nil {
		return storage.Document{}, err
	}
	payload, payloadHash := testFrozenPayload()

	return store.Documents().Issue(ctx, id, storage.DocumentIssue{
		Type:          docType,
		Number:        n,
		NumberYear:    year,
		DisplayNumber: fmt.Sprintf("RE-%d%04d", year, n),
		IssuedAt:      issuedAtForTest(year),
		Payload:       payload,
		PayloadSHA256: payloadHash,
	})
}

// issueTestNow is the clock of every issue below: a November day of 2026.
var issueTestNow = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

// issueEnv is the restricted store, the admin pool, one tenant with a
// complete Kleinunternehmer profile, a period from 2000 and a customer.
type issueEnv struct {
	ctx       context.Context
	adminPool *pgxpool.Pool
	store     *postgres.Store
	tenant    storage.Tenant
	customer  storage.Customer
}

func newIssueEnv(t *testing.T, maxConns int) issueEnv {
	t.Helper()

	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed issue test")
	}
	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	restrictedURL := setUpTestAppRole(ctx, t, adminPool, adminURL)
	pool, err := postgres.NewPool(ctx, withPoolMaxConns(t, restrictedURL, maxConns))
	if err != nil {
		t.Fatalf("open restricted pool as %s: %v", testAppRole, err)
	}
	t.Cleanup(pool.Close)

	e := issueEnv{ctx: ctx, adminPool: adminPool, store: postgres.NewStore(pool)}
	e.tenant, e.customer = e.seedTenant(t, "issue")

	return e
}

// seedTenant creates a tenant with a profile, a period and a customer.
func (e issueEnv) seedTenant(t *testing.T, prefix string) (storage.Tenant, storage.Customer) {
	t.Helper()

	tenant, err := e.store.Tenants().Create(e.ctx, storage.Tenant{Slug: prefix + "-" + uuid.NewString(), Name: prefix})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	text := func(s string) *string { return &s }

	var customer storage.Customer
	err = e.store.WithTenant(e.ctx, tenant.ID, func(ctx context.Context) error {
		if _, err := e.store.SellerProfiles().Put(ctx, storage.SellerProfile{
			TenantID:         tenant.ID,
			LegalName:        "Example Trading",
			Street:           "Main Street 1",
			PostalCode:       "12345",
			City:             "Example City",
			CountryCode:      "DE",
			TaxNumber:        text("12/345/67890"),
			DefaultVATRegime: storage.VATRegimeKleinunternehmer,
			DefaultLanguage:  storage.DocumentLanguageGerman,
			NumberFormats: map[storage.DocumentType]string{
				storage.DocumentTypeInvoice: "RE-{year}{seq:4}",
				storage.DocumentTypeOffer:   "AN-{year}{seq:4}",
			},
		}); err != nil {
			return fmt.Errorf("put profile: %w", err)
		}
		if _, err := e.store.VATRegimePeriods().Create(ctx, storage.VATRegimePeriod{
			TenantID:  tenant.ID,
			ValidFrom: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
			Regime:    storage.VATRegimeKleinunternehmer,
		}); err != nil {
			return fmt.Errorf("create period: %w", err)
		}
		var err error
		customer, err = e.store.Customers().Create(ctx, storage.Customer{
			TenantID:    tenant.ID,
			Name:        "Customer Ltd",
			Street:      text("Side Street 2"),
			PostalCode:  text("54321"),
			City:        text("Other City"),
			CountryCode: text("DE"),
		})

		return err
	})
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	return tenant, customer
}

func (e issueEnv) issuer() docissue.Issuer {
	return docissue.Issuer{
		Documents:        e.store.Documents(),
		DocumentNumbers:  e.store.DocumentNumbers(),
		VATRegimePeriods: e.store.VATRegimePeriods(),
		Customers:        e.store.Customers(),
	}
}

// draft creates a Kleinunternehmer draft of tenant e.tenant.
func (e issueEnv) draft(t *testing.T, d storage.Document) storage.Document {
	t.Helper()

	d.TenantID = e.tenant.ID
	d.CustomerID = e.customer.ID
	if d.Type == "" {
		d.Type = storage.DocumentTypeInvoice
	}
	if d.VATRegime == "" {
		d.VATRegime = storage.VATRegimeKleinunternehmer
	}
	var created storage.Document
	err := e.store.WithTenant(e.ctx, e.tenant.ID, func(ctx context.Context) error {
		var err error
		created, err = e.store.Documents().Create(ctx, d, []storage.DocumentLine{
			{Description: "Consulting", QuantityThousandths: 2500, Unit: "hour", UnitPriceCents: 9500},
		})

		return err
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}

	return created
}

func (e issueEnv) issue(id uuid.UUID, issuer docissue.Issuer, now time.Time) (storage.Document, error) {
	var issued storage.Document
	err := e.store.WithTenant(e.ctx, e.tenant.ID, func(ctx context.Context) error {
		var err error
		issued, err = issuer.Issue(ctx, id, now)

		return err
	})

	return issued, err
}

func (e issueEnv) find(t *testing.T, id uuid.UUID) storage.Document {
	t.Helper()

	var d storage.Document
	if err := e.store.WithTenant(e.ctx, e.tenant.ID, func(ctx context.Context) error {
		var err error
		d, err = e.store.Documents().FindByID(ctx, id)

		return err
	}); err != nil {
		t.Fatalf("find %s: %v", id, err)
	}

	return d
}

// invoiceCounter is the invoice counter of 2026, the year of
// issueTestNow.
func (e issueEnv) invoiceCounter(t *testing.T) int64 {
	t.Helper()

	var n int64
	if err := e.store.WithTenant(e.ctx, e.tenant.ID, func(ctx context.Context) error {
		var err error
		n, err = e.store.DocumentNumbers().Current(ctx, storage.DocumentTypeInvoice, 2026)

		return err
	}); err != nil {
		t.Fatalf("read counter: %v", err)
	}

	return n
}

// TestIssue_ConcurrentIssuesAreGapless issues eight drafts at once through
// docissue.Issuer: the numbers are exactly 1 to 8, each once, and the
// counter is 8.
func TestIssue_ConcurrentIssuesAreGapless(t *testing.T) {
	const n = 8
	e := newIssueEnv(t, n+2)

	drafts := make([]storage.Document, n)
	for i := range drafts {
		drafts[i] = e.draft(t, storage.Document{})
	}

	issued := make([]storage.Document, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range drafts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			issued[i], errs[i] = e.issue(drafts[i].ID, e.issuer(), issueTestNow)
		}(i)
	}
	wg.Wait()

	seen := map[int64]bool{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("issue %d: %v", i, err)
		}
		number := *issued[i].Number
		if seen[number] {
			t.Errorf("number %d issued twice", number)
		}
		seen[number] = true
		if want := fmt.Sprintf("RE-2026%04d", number); *issued[i].DisplayNumber != want {
			t.Errorf("display number = %q, want %q", *issued[i].DisplayNumber, want)
		}
	}
	for want := int64(1); want <= n; want++ {
		if !seen[want] {
			t.Errorf("number %d was never issued: a gap", want)
		}
	}
	if got := e.invoiceCounter(t); got != n {
		t.Errorf("counter = %d, want %d", got, n)
	}
}

// TestIssue_ConcurrentIssueOfOneDraftHasOneWinner issues the same draft
// from eight transactions at once: one wins, the others wait on the row
// lock and find it issued, and the counter moved once.
func TestIssue_ConcurrentIssueOfOneDraftHasOneWinner(t *testing.T) {
	const n = 8
	e := newIssueEnv(t, n+2)
	draft := e.draft(t, storage.Document{})

	var wins, refused atomic.Int32
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.issue(draft.ID, e.issuer(), issueTestNow)
			switch {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, docissue.ErrNotDraft):
				refused.Add(1)
			default:
				t.Errorf("issue: %v", err)
			}
		}()
	}
	wg.Wait()

	if wins.Load() != 1 || refused.Load() != n-1 {
		t.Fatalf("wins = %d, refused = %d, want 1 and %d", wins.Load(), refused.Load(), n-1)
	}
	if got := e.invoiceCounter(t); got != 1 {
		t.Errorf("counter = %d, want 1", got)
	}
}

// failAfterAllocation refuses the payload on its second call, which the
// issuer makes after it allocated the number.
type failAfterAllocation struct{ calls atomic.Int32 }

func (f *failAfterAllocation) CheckPayload(context.Context, docpayload.FrozenPayloadV1) (map[string]string, error) {
	if f.calls.Add(1) == 2 {
		return map[string]string{"lines[0].description": "refused after the allocation"}, nil
	}

	return nil, nil
}

// TestIssue_RefusalsLeaveTheCounter covers both refusals: one before the
// allocation (an incomplete profile) and one after it, which the rollback
// of the transaction has to undo.
func TestIssue_RefusalsLeaveTheCounter(t *testing.T) {
	e := newIssueEnv(t, 4)

	issuer := e.issuer()
	issuer.Checker = &failAfterAllocation{}
	draft := e.draft(t, storage.Document{})
	_, err := e.issue(draft.ID, issuer, issueTestNow)
	var invalid *docissue.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("issue = %v, want a ValidationError after the allocation", err)
	}
	if got := e.invoiceCounter(t); got != 0 {
		t.Fatalf("counter = %d after a rolled back issue, want 0", got)
	}
	if got := e.find(t, draft.ID); got.State != storage.DocumentStateDraft {
		t.Fatalf("state = %q, want draft", got.State)
	}

	if err := e.store.WithTenant(e.ctx, e.tenant.ID, func(ctx context.Context) error {
		p, err := e.store.SellerProfiles().Get(ctx)
		if err != nil {
			return err
		}
		p.TaxNumber = nil
		_, err = e.store.SellerProfiles().Put(ctx, p)

		return err
	}); err != nil {
		t.Fatalf("clear the tax number: %v", err)
	}
	_, err = e.issue(draft.ID, e.issuer(), issueTestNow)
	if !errors.As(err, &invalid) || invalid.Details["seller.tax_number"] == "" {
		t.Fatalf("issue = %v, want seller.tax_number", err)
	}
	if got := e.invoiceCounter(t); got != 0 {
		t.Fatalf("counter = %d, want 0", got)
	}
}

// TestIssue_ASecondIssueOfANumberIsRefused writes the same display number
// and the same counter key twice, straight through the repository: the
// unique indexes are the last guard.
func TestIssue_ASecondIssueOfANumberIsRefused(t *testing.T) {
	e := newIssueEnv(t, 4)
	first := e.draft(t, storage.Document{})
	second := e.draft(t, storage.Document{})
	payload, hash := testFrozenPayload()
	issueAt := func(id uuid.UUID, number int64, display string) error {
		return e.store.WithTenant(e.ctx, e.tenant.ID, func(ctx context.Context) error {
			_, err := e.store.Documents().Issue(ctx, id, storage.DocumentIssue{
				Type: storage.DocumentTypeInvoice, Number: number, NumberYear: 2026, DisplayNumber: display,
				IssuedAt: issuedAtForTest(2026), Payload: payload, PayloadSHA256: hash,
			})

			return err
		})
	}

	if err := issueAt(first.ID, 1, "RE-20260001"); err != nil {
		t.Fatalf("first issue: %v", err)
	}
	if err := issueAt(second.ID, 2, "RE-20260001"); !errors.Is(err, storage.ErrConflict) {
		t.Errorf("same display number = %v, want ErrConflict", err)
	}
	if err := issueAt(second.ID, 1, "RE-20260001-X"); !errors.Is(err, storage.ErrConflict) {
		t.Errorf("same number and year = %v, want ErrConflict", err)
	}
	if err := issueAt(first.ID, 3, "RE-20260003"); !errors.Is(err, storage.ErrConflict) {
		t.Errorf("issuing an issued document again = %v, want ErrConflict", err)
	}
	if got := e.find(t, second.ID); got.State != storage.DocumentStateDraft {
		t.Errorf("second state = %q, want draft", got.State)
	}
}

// TestIssue_CorrectionChain issues corrections: each gets the root's
// number plus its sequence, eight concurrent corrections on one base give
// one winner and seven correction_base_changed, a duplicate sequence is
// refused by the index, and the original never changes.
func TestIssue_CorrectionChain(t *testing.T) {
	const n = 8
	e := newIssueEnv(t, n+2)

	root, err := e.issue(e.draft(t, storage.Document{}).ID, e.issuer(), issueTestNow)
	if err != nil {
		t.Fatalf("issue the original: %v", err)
	}
	rootBefore := e.find(t, root.ID)

	correctionOf := func(base storage.Document) storage.Document {
		seq := int32(0)
		if base.CorrectionSeq != nil {
			seq = *base.CorrectionSeq
		}

		return e.draft(t, storage.Document{
			Type:               storage.DocumentTypeCorrection,
			CorrectsDocumentID: &root.ID,
			CorrectionBaseID:   &base.ID,
			CorrectionBaseSeq:  &seq,
		})
	}

	first, err := e.issue(correctionOf(root).ID, e.issuer(), issueTestNow.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("issue correction 1: %v", err)
	}
	if *first.DisplayNumber != *root.DisplayNumber+"-1" || *first.CorrectionSeq != 1 ||
		*first.Number != *root.Number || *first.NumberYear != *root.NumberYear {
		t.Fatalf("correction 1 = %q seq %d key %d/%d", *first.DisplayNumber, *first.CorrectionSeq, *first.Number, *first.NumberYear)
	}

	drafts := make([]storage.Document, n)
	for i := range drafts {
		drafts[i] = correctionOf(first)
	}
	var wins, changed atomic.Int32
	var wg sync.WaitGroup
	for i := range drafts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := e.issue(drafts[i].ID, e.issuer(), issueTestNow.Add(48*time.Hour))
			var baseChanged *docissue.BaseChangedError
			switch {
			case err == nil:
				wins.Add(1)
			case errors.As(err, &baseChanged) && baseChanged.Seq == 2:
				changed.Add(1)
			default:
				t.Errorf("issue: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 1 || changed.Load() != n-1 {
		t.Fatalf("wins = %d, base changed = %d, want 1 and %d", wins.Load(), changed.Load(), n-1)
	}

	// A duplicate sequence written past the issuer is refused.
	dup := correctionOf(first)
	payload, hash := testFrozenPayload()
	seq := int32(1)
	err = e.store.WithTenant(e.ctx, e.tenant.ID, func(ctx context.Context) error {
		_, err := e.store.Documents().Issue(ctx, dup.ID, storage.DocumentIssue{
			Type: storage.DocumentTypeCorrection, Number: *root.Number, NumberYear: int(*root.NumberYear),
			DisplayNumber: "RE-DUPLICATE", CorrectionSeq: &seq,
			IssuedAt: issuedAtForTest(2026), Payload: payload, PayloadSHA256: hash,
		})

		return err
	})
	if !errors.Is(err, storage.ErrConflict) {
		t.Errorf("duplicate correction_seq = %v, want ErrConflict", err)
	}

	rootAfter := e.find(t, root.ID)
	if !bytes.Equal(rootBefore.FrozenPayload, rootAfter.FrozenPayload) || !rootBefore.UpdatedAt.Equal(rootAfter.UpdatedAt) ||
		*rootAfter.DisplayNumber != *rootBefore.DisplayNumber {
		t.Errorf("the original changed")
	}
	if got := e.invoiceCounter(t); got != 1 {
		t.Errorf("invoice counter = %d, want 1: a correction never allocates", got)
	}
}

// TestIssue_PeriodByIssueDate switches the regime on 2026-10-14 and
// issues a Kleinunternehmer draft on both sides of the switch.
func TestIssue_PeriodByIssueDate(t *testing.T) {
	e := newIssueEnv(t, 4)
	if err := e.store.WithTenant(e.ctx, e.tenant.ID, func(ctx context.Context) error {
		_, err := e.store.VATRegimePeriods().Create(ctx, storage.VATRegimePeriod{
			TenantID: e.tenant.ID, ValidFrom: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), Regime: storage.VATRegimeStandard,
		})

		return err
	}); err != nil {
		t.Fatalf("create the switch: %v", err)
	}

	before := e.draft(t, storage.Document{})
	issued, err := e.issue(before.ID, e.issuer(), time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("issue before the switch: %v", err)
	}
	p, err := docpayload.Unmarshal(issued.FrozenPayload)
	if err != nil || p.TaxNote != docissue.TaxNoteKleinunternehmerDE {
		t.Errorf("payload tax note = %q (%v), want the section 19 note", p.TaxNote, err)
	}

	after := e.draft(t, storage.Document{})
	_, err = e.issue(after.ID, e.issuer(), time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC))
	var mismatch *docissue.RegimeMismatchError
	if !errors.As(err, &mismatch) || mismatch.Regime != storage.VATRegimeStandard {
		t.Fatalf("issue after the switch = %v, want a mismatch naming standard", err)
	}
}

// TestDocuments_CrossTenant runs every document path of tenant B against
// rows of tenant A.
func TestDocuments_CrossTenant(t *testing.T) {
	e := newIssueEnv(t, 4)
	draftA := e.draft(t, storage.Document{})
	rootA, err := e.issue(e.draft(t, storage.Document{}).ID, e.issuer(), issueTestNow)
	if err != nil {
		t.Fatalf("issue A: %v", err)
	}
	offerA := e.draft(t, storage.Document{Type: storage.DocumentTypeOffer})
	tenantB, customerB := e.seedTenant(t, "issue-b")

	err = e.store.WithTenant(e.ctx, tenantB.ID, func(ctx context.Context) error {
		docs := e.store.Documents()
		if _, err := docs.FindByID(ctx, draftA.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("FindByID = %v, want ErrNotFound", err)
		}
		if _, err := docs.Lock(ctx, draftA.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("Lock = %v, want ErrNotFound", err)
		}
		if chain, err := docs.Chain(ctx, rootA.ID); err != nil || len(chain) != 0 {
			t.Errorf("Chain = %v, %v, want nothing", chain, err)
		}
		if list, err := docs.List(ctx); err != nil || len(list) != 0 {
			t.Errorf("List = %v, %v, want nothing", list, err)
		}
		if _, err := docs.Update(ctx, storage.Document{ID: draftA.ID, CustomerID: customerB.ID, VATRegime: storage.VATRegimeStandard}, nil); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("Update = %v, want ErrNotFound", err)
		}
		if err := docs.DeleteDraft(ctx, draftA.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("DeleteDraft = %v, want ErrNotFound", err)
		}
		payload, hash := testFrozenPayload()
		if _, err := docs.Issue(ctx, draftA.ID, storage.DocumentIssue{
			Type: storage.DocumentTypeInvoice, Number: 99, NumberYear: 2026, DisplayNumber: "RE-20260099",
			IssuedAt: issuedAtForTest(2026), Payload: payload, PayloadSHA256: hash,
		}); !errors.Is(err, storage.ErrConflict) {
			t.Errorf("Issue = %v, want ErrConflict", err)
		}

		// Every call above matches zero rows and raises no SQL error, so
		// this scope still commits. A refused write aborts its
		// transaction: each one below runs in a scope of its own.
		return nil
	})
	if err != nil {
		t.Fatalf("tenant B scope: %v", err)
	}

	// marker tags every forged row, so the read backs below find it if a
	// refusal ever stops working.
	marker := "forged-" + uuid.NewString()
	createInB := func(d storage.Document) error {
		d.CustomerReference = &marker

		return e.store.WithTenant(e.ctx, tenantB.ID, func(ctx context.Context) error {
			_, err := e.store.Documents().Create(ctx, d, nil)

			return err
		})
	}

	// documents_check_references looks the root up under B's tenant and
	// finds nothing: 23514 before the composite foreign key runs.
	requireSQLState(t, createInB(storage.Document{
		TenantID: tenantB.ID, Type: storage.DocumentTypeCorrection, CustomerID: customerB.ID,
		VATRegime: storage.VATRegimeKleinunternehmer, CorrectsDocumentID: &rootA.ID,
	}), "23514", "a correction of A's invoice created in B")
	requireSQLState(t, createInB(storage.Document{
		TenantID: tenantB.ID, Type: storage.DocumentTypeInvoice, CustomerID: customerB.ID,
		VATRegime: storage.VATRegimeKleinunternehmer, SourceOfferID: &offerA.ID,
	}), "23514", "an invoice from A's offer created in B")
	// A row naming tenant A, written in B's scope: row level security's
	// WITH CHECK refuses it.
	requireSQLState(t, createInB(storage.Document{
		TenantID: e.tenant.ID, Type: storage.DocumentTypeInvoice, CustomerID: e.customer.ID,
		VATRegime: storage.VATRegimeKleinunternehmer,
	}), sqlStateInsufficientPrivilege, "B writing a row of tenant A")

	// Read back in both owning tenants' scopes: no forged row exists, and
	// A's draft is exactly as it was.
	for _, tenantID := range []uuid.UUID{e.tenant.ID, tenantB.ID} {
		if err := e.store.WithTenant(e.ctx, tenantID, func(ctx context.Context) error {
			list, err := e.store.Documents().List(ctx)
			if err != nil {
				return err
			}
			for _, d := range list {
				if d.CustomerReference != nil && *d.CustomerReference == marker {
					t.Errorf("tenant %s holds a forged row: %+v", tenantID, d)
				}
			}

			return nil
		}); err != nil {
			t.Fatalf("read back in tenant %s: %v", tenantID, err)
		}
	}

	got := e.find(t, draftA.ID)
	if got.State != storage.DocumentStateDraft || got.CustomerID != e.customer.ID || got.VATRegime != draftA.VATRegime ||
		!got.UpdatedAt.Equal(draftA.UpdatedAt) || len(got.Lines) != len(draftA.Lines) {
		t.Errorf("A's draft changed: %+v", got)
	}
}

// TestDocuments_LedgerTriggers checks the triggers of
// 20261121120000_add_document_fields.sql, as the superuser, which the
// grants do not limit: only the triggers stand.
func TestDocuments_LedgerTriggers(t *testing.T) {
	e := newIssueEnv(t, 4)
	issued, err := e.issue(e.draft(t, storage.Document{}).ID, e.issuer(), issueTestNow)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	exec := func(sql string, args ...any) error {
		tx := beginScoped(e.ctx, t, e.adminPool, e.tenant.ID)
		defer func() { _ = tx.Rollback(e.ctx) }()
		_, err := tx.Exec(e.ctx, sql, args...)

		return err
	}

	assertBlockedByImmutabilityTrigger(t,
		exec(`UPDATE documents SET display_number = 'RE-OTHER', notes = 'tampered' WHERE id = $1`, issued.ID), "documents")

	for _, table := range []string{"documents", "document_lines", "document_numbers"} {
		requireSQLState(t, exec(`TRUNCATE `+table+` CASCADE`), sqlStateInsufficientPrivilege, "TRUNCATE "+table)
	}

	var pgErr *pgconn.PgError
	err = exec(`UPDATE document_numbers SET last_number = 0 WHERE tenant_id = $1 AND type = 'invoice'`, e.tenant.ID)
	if !errors.As(err, &pgErr) || pgErr.Code != pgIntegrityConstraintViolationClass {
		t.Errorf("lowering the counter = %v, want the trigger's refusal", err)
	}
	err = exec(`DELETE FROM document_numbers WHERE tenant_id = $1`, e.tenant.ID)
	if !errors.As(err, &pgErr) || pgErr.Code != pgIntegrityConstraintViolationClass {
		t.Errorf("deleting the counter = %v, want the trigger's refusal", err)
	}

	// A correction must name an issued invoice as its root.
	draft := e.draft(t, storage.Document{})
	err = exec(`INSERT INTO documents (tenant_id, type, customer_id, vat_regime, corrects_document_id)
		VALUES ($1, 'correction', $2, 'kleinunternehmer', $3)`, e.tenant.ID, e.customer.ID, draft.ID)
	requireSQLState(t, err, "23514", "a correction of a draft")
}

// TestSellerProfileForIssueHoldsAConcurrentSave holds the profile lock of
// an issue and checks that a profile save waits for it.
func TestSellerProfileForIssueHoldsAConcurrentSave(t *testing.T) {
	e := newIssueEnv(t, 4)

	locked := make(chan struct{})
	release := make(chan struct{})
	issueDone := make(chan error, 1)
	go func() {
		issueDone <- e.store.WithTenant(e.ctx, e.tenant.ID, func(ctx context.Context) error {
			if _, err := e.store.Documents().SellerProfileForIssue(ctx); err != nil {
				return err
			}
			close(locked)
			<-release

			return nil
		})
	}()
	<-locked

	saved := make(chan error, 1)
	go func() {
		saved <- e.store.WithTenant(e.ctx, e.tenant.ID, func(ctx context.Context) error {
			p, err := e.store.SellerProfiles().Get(ctx)
			if err != nil {
				return err
			}
			p.LegalName = "Renamed Trading"
			_, err = e.store.SellerProfiles().Put(ctx, p)

			return err
		})
	}()

	select {
	case err := <-saved:
		t.Fatalf("the profile save finished while an issue held the profile: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if err := <-issueDone; err != nil {
		t.Fatalf("issue transaction: %v", err)
	}
	if err := <-saved; err != nil {
		t.Fatalf("profile save: %v", err)
	}
}
