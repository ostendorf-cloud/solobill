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

// fakeExpenseRepository is an in memory storage.ExpenseRepository that
// mirrors the postgres one: it filters by the tenant the handler opened
// the scope for (httpapi.ScopedTenant), the way row level security does,
// refuses every write to a booked row with ErrConflict, the way the
// trigger does, and lets an original be reversed once.
//
// leaky models a repository whose tenant filter is missing: reads then
// return rows of every tenant, so a test can prove the handler's own
// tenant check. unguarded models one whose draft guard and trigger are
// missing: UpdateDraft and DeleteDraft then touch a booked row too, so a
// test can prove the handler's own state check.
type fakeExpenseRepository struct {
	mu        sync.Mutex
	rows      map[uuid.UUID]storage.Expense
	clock     int
	leaky     bool
	unguarded bool
}

// leak switches the tenant filter of reads off.
func (r *fakeExpenseRepository) leak() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.leaky = true
}

// unguard switches the draft guard of UpdateDraft and DeleteDraft off.
func (r *fakeExpenseRepository) unguard() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.unguarded = true
}

func init() {
	repositoryHooks = append(repositoryHooks, func(_ *testing.T, repos *httpapi.Repositories) {
		repos.Expenses = &fakeExpenseRepository{rows: map[uuid.UUID]storage.Expense{}}
	})
}

// row reads a stored row back, whatever its tenant or state.
func (r *fakeExpenseRepository) row(id uuid.UUID) (storage.Expense, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.rows[id]

	return e, ok
}

// count is the number of stored rows of every tenant.
func (r *fakeExpenseRepository) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.rows)
}

// now hands out strictly increasing timestamps, so the creation order is
// the order of created_at, as in the database.
func (r *fakeExpenseRepository) now() time.Time {
	r.clock++

	return time.Date(2026, 11, 20, 10, 0, 0, 0, fakeLocation).Add(time.Duration(r.clock) * time.Second)
}

func (r *fakeExpenseRepository) Create(ctx context.Context, e storage.Expense) (storage.Expense, error) {
	tenant, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return storage.Expense{}, storage.ErrNoTenantScope
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// The row level security policy refuses a row of another tenant.
	if e.TenantID != tenant {
		return storage.Expense{}, storage.ErrNotFound
	}
	e.ID = uuid.New()
	e.State = storage.BookingStateDraft
	e.StornoOfID = nil
	e.BookedAt = nil
	e.CreatedAt = r.now()
	e.UpdatedAt = e.CreatedAt
	r.rows[e.ID] = e

	return e, nil
}

// find must be called with r.mu held.
func (r *fakeExpenseRepository) find(ctx context.Context, id uuid.UUID) (storage.Expense, error) {
	tenant, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return storage.Expense{}, storage.ErrNoTenantScope
	}
	e, ok := r.rows[id]
	if !ok || (e.TenantID != tenant && !r.leaky) {
		return storage.Expense{}, storage.ErrNotFound
	}

	return e, nil
}

func (r *fakeExpenseRepository) FindByID(ctx context.Context, id uuid.UUID) (storage.Expense, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.find(ctx, id)
}

func (r *fakeExpenseRepository) List(ctx context.Context, f storage.ExpenseFilter) ([]storage.Expense, error) {
	tenant, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return nil, storage.ErrNoTenantScope
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var out []storage.Expense
	for _, e := range r.rows {
		if e.TenantID != tenant && !r.leaky {
			continue
		}
		if f.Year != nil && (e.PaidOn == nil || e.PaidOn.Year() != *f.Year) {
			continue
		}
		if f.State != nil && e.State != *f.State {
			continue
		}
		if f.Category != nil && e.Category != *f.Category {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		return fakeBookingLess(out[i].PaidOn, out[j].PaidOn, out[i].CreatedAt, out[j].CreatedAt, out[i].ID, out[j].ID)
	})

	return out, nil
}

// fakeBookingLess is the order of the list queries: the date, nulls
// last, then created_at, then id.
func fakeBookingLess(da, db *time.Time, ca, cb time.Time, ia, ib uuid.UUID) bool {
	switch {
	case da == nil && db != nil:
		return false
	case da != nil && db == nil:
		return true
	case da != nil && !da.Equal(*db):
		return da.Before(*db)
	case !ca.Equal(cb):
		return ca.Before(cb)
	default:
		return ia.String() < ib.String()
	}
}

func (r *fakeExpenseRepository) UpdateDraft(ctx context.Context, e storage.Expense) (storage.Expense, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, err := r.find(ctx, e.ID)
	if err != nil {
		return storage.Expense{}, err
	}
	if existing.State != storage.BookingStateDraft && !r.unguarded {
		return storage.Expense{}, storage.ErrConflict
	}
	e.TenantID = existing.TenantID
	e.State = existing.State
	e.StornoOfID = existing.StornoOfID
	e.Imported = existing.Imported
	e.BookedAt = nil
	e.CreatedAt = existing.CreatedAt
	e.UpdatedAt = r.now()
	r.rows[e.ID] = e

	return e, nil
}

func (r *fakeExpenseRepository) DeleteDraft(ctx context.Context, id uuid.UUID) error {
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

func (r *fakeExpenseRepository) Book(ctx context.Context, id uuid.UUID, bookedAt time.Time) (storage.Expense, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	e, err := r.find(ctx, id)
	if err != nil {
		return storage.Expense{}, err
	}
	if e.State != storage.BookingStateDraft || e.PaidOn == nil || e.Category == "" {
		return storage.Expense{}, storage.ErrConflict
	}
	e.State = storage.BookingStateBooked
	e.BookedAt = &bookedAt
	e.UpdatedAt = r.now()
	r.rows[id] = e

	return e, nil
}

func (r *fakeExpenseRepository) Storno(ctx context.Context, id uuid.UUID, paidOn, bookedAt time.Time) (storage.Expense, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	original, err := r.find(ctx, id)
	if err != nil {
		return storage.Expense{}, err
	}
	if original.State != storage.BookingStateBooked || original.StornoOfID != nil {
		return storage.Expense{}, storage.ErrConflict
	}
	for _, other := range r.rows {
		if other.StornoOfID != nil && *other.StornoOfID == id {
			return storage.Expense{}, storage.ErrConflict
		}
	}

	storno := original
	storno.ID = uuid.New()
	storno.PaidOn = &paidOn
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
