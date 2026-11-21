package httpapi_test

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// fakeOtherIncomeRepository is fakeExpenseRepository for other income,
// with the same tenant filter, immutability and storno rule.
type fakeOtherIncomeRepository struct {
	mu       sync.Mutex
	rows     map[uuid.UUID]storage.OtherIncome
	clock    int
	bookHook func(storage.OtherIncome) storage.OtherIncome
	// unguarded models a repository whose draft guard and trigger are
	// missing, see fakeExpenseRepository.
	unguarded bool
}

// changeOnBook makes Book apply fn to the stored row first, as a
// concurrent update between the handler's read and the booking would.
func (r *fakeOtherIncomeRepository) changeOnBook(fn func(storage.OtherIncome) storage.OtherIncome) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.bookHook = fn
}

// unguard switches the draft guard of UpdateDraft and DeleteDraft off.
func (r *fakeOtherIncomeRepository) unguard() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.unguarded = true
}

func init() {
	repositoryHooks = append(repositoryHooks, func(_ *testing.T, repos *httpapi.Repositories) {
		repos.OtherIncome = &fakeOtherIncomeRepository{rows: map[uuid.UUID]storage.OtherIncome{}}
	})
}

func (r *fakeOtherIncomeRepository) row(id uuid.UUID) (storage.OtherIncome, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	o, ok := r.rows[id]

	return o, ok
}

func (r *fakeOtherIncomeRepository) now() time.Time {
	r.clock++

	return time.Date(2026, 11, 20, 10, 0, 0, 0, fakeLocation).Add(time.Duration(r.clock) * time.Second)
}

func (r *fakeOtherIncomeRepository) Create(ctx context.Context, o storage.OtherIncome) (storage.OtherIncome, error) {
	tenant, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return storage.OtherIncome{}, storage.ErrNoTenantScope
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if o.TenantID != tenant {
		return storage.OtherIncome{}, storage.ErrNotFound
	}
	o.ID = uuid.New()
	o.State = storage.BookingStateDraft
	o.StornoOfID = nil
	o.BookedAt = nil
	o.CreatedAt = r.now()
	o.UpdatedAt = o.CreatedAt
	r.rows[o.ID] = o

	return o, nil
}

// find must be called with r.mu held.
func (r *fakeOtherIncomeRepository) find(ctx context.Context, id uuid.UUID) (storage.OtherIncome, error) {
	tenant, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return storage.OtherIncome{}, storage.ErrNoTenantScope
	}
	o, ok := r.rows[id]
	if !ok || o.TenantID != tenant {
		return storage.OtherIncome{}, storage.ErrNotFound
	}

	return o, nil
}

func (r *fakeOtherIncomeRepository) FindByID(ctx context.Context, id uuid.UUID) (storage.OtherIncome, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.find(ctx, id)
}

func (r *fakeOtherIncomeRepository) List(ctx context.Context, f storage.OtherIncomeFilter) ([]storage.OtherIncome, error) {
	tenant, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return nil, storage.ErrNoTenantScope
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var out []storage.OtherIncome
	for _, o := range r.rows {
		if o.TenantID != tenant {
			continue
		}
		if f.Year != nil && (o.ReceivedOn == nil || o.ReceivedOn.Year() != *f.Year) {
			continue
		}
		if f.State != nil && o.State != *f.State {
			continue
		}
		if f.Category != nil && o.Category != *f.Category {
			continue
		}
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool {
		return fakeBookingLess(out[i].ReceivedOn, out[j].ReceivedOn, out[i].CreatedAt, out[j].CreatedAt, out[i].ID, out[j].ID)
	})

	return out, nil
}

func (r *fakeOtherIncomeRepository) UpdateDraft(ctx context.Context, o storage.OtherIncome) (storage.OtherIncome, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, err := r.find(ctx, o.ID)
	if err != nil {
		return storage.OtherIncome{}, err
	}
	if existing.State != storage.BookingStateDraft && !r.unguarded {
		return storage.OtherIncome{}, storage.ErrConflict
	}
	o.TenantID = existing.TenantID
	o.State = existing.State
	o.StornoOfID = existing.StornoOfID
	o.Imported = existing.Imported
	o.BookedAt = nil
	o.CreatedAt = existing.CreatedAt
	o.UpdatedAt = r.now()
	r.rows[o.ID] = o

	return o, nil
}

func (r *fakeOtherIncomeRepository) DeleteDraft(ctx context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, err := r.find(ctx, id)
	if err != nil {
		return err
	}
	if existing.State != storage.BookingStateDraft && !r.unguarded {
		return storage.ErrConflict
	}
	delete(r.rows, id)

	return nil
}

func (r *fakeOtherIncomeRepository) Book(ctx context.Context, id uuid.UUID, bookedAt time.Time) (storage.OtherIncome, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	o, err := r.find(ctx, id)
	if err != nil {
		return storage.OtherIncome{}, err
	}
	if o.State != storage.BookingStateDraft || o.ReceivedOn == nil || o.Category == "" {
		return storage.OtherIncome{}, storage.ErrConflict
	}
	if r.bookHook != nil {
		// A change that slips in between the handler's read and the booking.
		o = r.bookHook(o)
	}
	o.State = storage.BookingStateBooked
	o.BookedAt = &bookedAt
	o.UpdatedAt = r.now()
	r.rows[id] = o

	return o, nil
}

func (r *fakeOtherIncomeRepository) Storno(ctx context.Context, id uuid.UUID, receivedOn, bookedAt time.Time) (storage.OtherIncome, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	original, err := r.find(ctx, id)
	if err != nil {
		return storage.OtherIncome{}, err
	}
	if original.State != storage.BookingStateBooked || original.StornoOfID != nil {
		return storage.OtherIncome{}, storage.ErrConflict
	}
	for _, other := range r.rows {
		if other.StornoOfID != nil && *other.StornoOfID == id {
			return storage.OtherIncome{}, storage.ErrConflict
		}
	}

	storno := original
	storno.ID = uuid.New()
	storno.ReceivedOn = &receivedOn
	storno.GrossCents = -original.GrossCents
	storno.NetCents = -original.NetCents
	storno.VATCents = -original.VATCents
	storno.StornoOfID = &original.ID
	storno.Imported = false
	storno.BookedAt = &bookedAt
	storno.CreatedAt = r.now()
	storno.UpdatedAt = storno.CreatedAt
	r.rows[storno.ID] = storno

	return storno, nil
}
