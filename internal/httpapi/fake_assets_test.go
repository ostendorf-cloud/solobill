package httpapi_test

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// fakeAssets is the in memory AssetRepository of the asset tests. It
// follows the interface's own contract for ErrConflict and ErrNotFound.
// Like fakeVATRegimes it holds one tenant's rows.
type fakeAssets struct {
	mu     sync.Mutex
	assets []storage.Asset
}

func (f *fakeAssets) index(id uuid.UUID) int {
	for i, a := range f.assets {
		if a.ID == id {
			return i
		}
	}

	return -1
}

func (f *fakeAssets) expenseTaken(a storage.Asset) bool {
	if a.ExpenseID == nil {
		return false
	}
	for _, existing := range f.assets {
		if existing.ID != a.ID && existing.ExpenseID != nil && *existing.ExpenseID == *a.ExpenseID {
			return true
		}
	}

	return false
}

func (f *fakeAssets) Create(_ context.Context, a storage.Asset) (storage.Asset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.expenseTaken(a) {
		return storage.Asset{}, storage.ErrConflict
	}
	a.ID = uuid.New()
	a.CreatedAt = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	a.UpdatedAt = a.CreatedAt
	f.assets = append(f.assets, a)

	return a, nil
}

func (f *fakeAssets) FindByID(_ context.Context, id uuid.UUID) (storage.Asset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	i := f.index(id)
	if i < 0 {
		return storage.Asset{}, storage.ErrNotFound
	}

	return f.assets[i], nil
}

func (f *fakeAssets) List(context.Context) ([]storage.Asset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := append([]storage.Asset(nil), f.assets...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].AcquiredOn.Before(out[j].AcquiredOn) })

	return out, nil
}

func fakeFirstScheduleYear(a storage.Asset) int {
	if a.OpeningBookValueYear != nil {
		return int(*a.OpeningBookValueYear)
	}

	return a.AcquiredOn.Year()
}

func (f *fakeAssets) Update(_ context.Context, a storage.Asset, now time.Time) (storage.Asset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	i := f.index(a.ID)
	if i < 0 {
		return storage.Asset{}, storage.ErrNotFound
	}
	stored := f.assets[i]
	if stored.DisposedOn != nil || fakeFirstScheduleYear(stored) < now.Year() || fakeFirstScheduleYear(a) < now.Year() {
		return storage.Asset{}, storage.ErrConflict
	}
	if f.expenseTaken(a) {
		return storage.Asset{}, storage.ErrConflict
	}

	a.TenantID, a.Imported, a.CreatedAt = stored.TenantID, stored.Imported, stored.CreatedAt
	a.DisposedOn, a.DisposalProceedsCents = stored.DisposedOn, stored.DisposalProceedsCents
	a.UpdatedAt = stored.UpdatedAt.Add(time.Second)
	f.assets[i] = a

	return a, nil
}

func (f *fakeAssets) RecordDisposal(_ context.Context, id uuid.UUID, disposedOn time.Time, proceeds *int64) (storage.Asset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	i := f.index(id)
	if i < 0 {
		return storage.Asset{}, storage.ErrNotFound
	}
	if f.assets[i].DisposedOn != nil {
		return storage.Asset{}, storage.ErrConflict
	}
	f.assets[i].DisposedOn = &disposedOn
	f.assets[i].DisposalProceedsCents = proceeds

	return f.assets[i], nil
}

// put stores a fixed asset directly, for a state no request can create
// any more, such as an asset acquired in a closed year.
func (f *fakeAssets) put(a storage.Asset) storage.Asset {
	f.mu.Lock()
	defer f.mu.Unlock()

	a.ID = uuid.New()
	f.assets = append(f.assets, a)

	return a
}

// assetExpenses is an ExpenseRepository (issue #76's interface) whose
// FindByID answers a set list. Every other method answers an error, so
// the asset handlers cannot depend on them.
type assetExpenses struct {
	expenses map[uuid.UUID]storage.Expense
}

var errAssetExpensesReadOnly = errors.New("assetExpenses: only FindByID is faked")

func (f *assetExpenses) FindByID(_ context.Context, id uuid.UUID) (storage.Expense, error) {
	e, ok := f.expenses[id]
	if !ok {
		return storage.Expense{}, storage.ErrNotFound
	}

	return e, nil
}

func (f *assetExpenses) Create(context.Context, storage.Expense) (storage.Expense, error) {
	return storage.Expense{}, errAssetExpensesReadOnly
}

func (f *assetExpenses) List(context.Context, storage.ExpenseFilter) ([]storage.Expense, error) {
	return nil, errAssetExpensesReadOnly
}

func (f *assetExpenses) UpdateDraft(context.Context, storage.Expense) (storage.Expense, error) {
	return storage.Expense{}, errAssetExpensesReadOnly
}

func (f *assetExpenses) DeleteDraft(context.Context, uuid.UUID) error {
	return errAssetExpensesReadOnly
}

func (f *assetExpenses) Book(context.Context, uuid.UUID, time.Time) (storage.Expense, error) {
	return storage.Expense{}, errAssetExpensesReadOnly
}

func (f *assetExpenses) Storno(context.Context, uuid.UUID, time.Time, time.Time) (storage.Expense, error) {
	return storage.Expense{}, errAssetExpensesReadOnly
}

func init() {
	repositoryHooks = append(repositoryHooks, func(_ *testing.T, repos *httpapi.Repositories) {
		if repos.Assets == nil {
			repos.Assets = &fakeAssets{}
		}
	})
}
