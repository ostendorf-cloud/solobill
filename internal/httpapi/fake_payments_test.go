package httpapi_test

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/docissue/docissuetest"
	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// fakePaymentRepository is an in memory storage.PaymentRepository that
// mirrors the postgres one: it filters by the tenant the handler opened
// the scope for (httpapi.ScopedTenant), the way row level security does,
// and refuses with ErrConflict what payments_check_insert and
// payments_reversed_once refuse: a document that is not an issued invoice
// of the tenant, and a reversal that does not mirror an unreversed
// payment of its own chain. It has no Update and no Delete, like the
// table.
//
// leaky models a repository whose tenant filter is missing: reads then
// return rows of every tenant, so a test can prove the handler's own
// tenant check.
type fakePaymentRepository struct {
	mu    sync.Mutex
	docs  func() *docissuetest.Store
	rows  map[uuid.UUID]storage.Payment
	clock int
	leaky bool
}

// leakyDocumentsByTest holds the tests whose app reads documents through
// leakyDocuments.
var leakyDocumentsByTest sync.Map

// withLeakyDocuments makes the next app of t read documents through
// leakyDocuments.
func withLeakyDocuments(t *testing.T) {
	t.Helper()

	leakyDocumentsByTest.Store(t, true)
	t.Cleanup(func() { leakyDocumentsByTest.Delete(t) })
}

// leakyDocuments is the documents fake with the tenant filter of FindByID
// and Lock missing, so a test can prove the payment handler's own tenant
// check of the document. Every other method is the fake's own.
type leakyDocuments struct {
	*docissuetest.Store
}

func (l leakyDocuments) FindByID(_ context.Context, id uuid.UUID) (storage.Document, error) {
	d, ok := l.Get(id)
	if !ok {
		return storage.Document{}, storage.ErrNotFound
	}

	return d, nil
}

func (l leakyDocuments) Lock(ctx context.Context, id uuid.UUID) (storage.Document, error) {
	return l.FindByID(ctx, id)
}

func init() {
	repositoryHooks = append(repositoryHooks, func(t *testing.T, repos *httpapi.Repositories) {
		if _, ok := leakyDocumentsByTest.Load(t); ok {
			if store, ok := repos.Documents.(*docissuetest.Store); ok {
				repos.Documents = leakyDocuments{Store: store}
			}
		}
		repos.Payments = &fakePaymentRepository{
			rows: map[uuid.UUID]storage.Payment{},
			// The documents hook (fake_documents_test.go) runs first and
			// sets the store this one checks document_id against.
			docs: func() *docissuetest.Store {
				switch store := repos.Documents.(type) {
				case *docissuetest.Store:
					return store
				case leakyDocuments:
					return store.Store
				}

				return nil
			},
		}
	})
}

// paymentRepo reads the fake back out of the app.
func paymentRepo(t *testing.T, app testApp) *fakePaymentRepository {
	t.Helper()

	repo, ok := app.repos.Payments.(*fakePaymentRepository)
	if !ok {
		t.Fatalf("Payments = %T, want the fake", app.repos.Payments)
	}

	return repo
}

// leak switches the tenant filter of reads off.
func (r *fakePaymentRepository) leak() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.leaky = true
}

// put stores p as it is, whatever its tenant, for a test that needs a row
// the API would not write. It returns p with an id.
func (r *fakePaymentRepository) put(p storage.Payment) storage.Payment {
	r.mu.Lock()
	defer r.mu.Unlock()

	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	p.CreatedAt = r.stamp()
	r.rows[p.ID] = p

	return p
}

// all returns every stored row of every tenant, for assertions.
func (r *fakePaymentRepository) all() []storage.Payment {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]storage.Payment, 0, len(r.rows))
	for _, p := range r.rows {
		out = append(out, p)
	}

	return out
}

func (r *fakePaymentRepository) stamp() time.Time {
	r.clock++

	return time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC).Add(time.Duration(r.clock) * time.Second)
}

func (r *fakePaymentRepository) scope(ctx context.Context) (uuid.UUID, error) {
	id, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return uuid.Nil, storage.ErrNoTenantScope
	}

	return id, nil
}

// Create implements storage.PaymentRepository.
func (r *fakePaymentRepository) Create(ctx context.Context, p storage.Payment) (storage.Payment, error) {
	tenant, err := r.scope(ctx)
	if err != nil {
		return storage.Payment{}, err
	}
	if p.TenantID != tenant {
		return storage.Payment{}, docissuetest.ErrWrongTenant
	}

	// payments_check_insert: an issued invoice of the tenant.
	doc, ok := r.docs().Get(p.DocumentID)
	if !ok || doc.TenantID != tenant || doc.Type != storage.DocumentTypeInvoice || doc.State != storage.DocumentStateIssued {
		return storage.Payment{}, storage.ErrConflict
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if p.ReversesPaymentID != nil {
		original, ok := r.rows[*p.ReversesPaymentID]
		if !ok || original.TenantID != tenant || original.DocumentID != p.DocumentID ||
			original.ReversesPaymentID != nil || p.AmountCents != -original.AmountCents ||
			!p.ReceivedOn.Equal(original.ReceivedOn) {
			return storage.Payment{}, storage.ErrConflict
		}
		// payments_reversed_once.
		for _, q := range r.rows {
			if q.TenantID == tenant && q.ReversesPaymentID != nil && *q.ReversesPaymentID == original.ID {
				return storage.Payment{}, storage.ErrConflict
			}
		}
	}

	p.ID = uuid.New()
	p.CreatedAt = r.stamp()
	r.rows[p.ID] = p

	return p, nil
}

func (r *fakePaymentRepository) list(ctx context.Context, keep func(storage.Payment) bool) ([]storage.Payment, error) {
	tenant, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var out []storage.Payment
	for _, p := range r.rows {
		if (r.leaky || p.TenantID == tenant) && keep(p) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.ReceivedOn.Equal(b.ReceivedOn) {
			return a.ReceivedOn.Before(b.ReceivedOn)
		}

		return a.CreatedAt.Before(b.CreatedAt)
	})

	return out, nil
}

// ListByChain implements storage.PaymentRepository.
func (r *fakePaymentRepository) ListByChain(ctx context.Context, rootID uuid.UUID) ([]storage.Payment, error) {
	return r.list(ctx, func(p storage.Payment) bool { return p.DocumentID == rootID })
}

// ListReceivedBetween implements storage.PaymentRepository.
func (r *fakePaymentRepository) ListReceivedBetween(ctx context.Context, from, to time.Time) ([]storage.Payment, error) {
	return r.list(ctx, func(p storage.Payment) bool {
		return !p.ReceivedOn.Before(from) && !p.ReceivedOn.After(to)
	})
}
