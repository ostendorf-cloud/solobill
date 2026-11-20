package httpapi_test

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// fakeCatalogueRepository is an in memory storage.CatalogueItemRepository
// that mirrors the postgres one: FindByID answers a soft deleted row,
// List, Update and Delete do not, and a sku is unique per tenant among
// active items, case insensitively. It filters by the tenant the handler
// opened the scope for, see fakeCustomerRepository.
type fakeCatalogueRepository struct {
	mu   sync.Mutex
	rows map[uuid.UUID]storage.CatalogueItem
}

func init() {
	repositoryHooks = append(repositoryHooks, func(_ *testing.T, repos *httpapi.Repositories) {
		repos.CatalogueItems = &fakeCatalogueRepository{rows: map[uuid.UUID]storage.CatalogueItem{}}
	})
}

// row reads a stored row back, whatever its tenant or state.
func (r *fakeCatalogueRepository) row(id uuid.UUID) storage.CatalogueItem {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.rows[id]
}

func (r *fakeCatalogueRepository) skuTaken(item storage.CatalogueItem) bool {
	if item.SKU == nil {
		return false
	}
	for _, other := range r.rows {
		if other.ID != item.ID && other.TenantID == item.TenantID && other.DeletedAt == nil &&
			other.SKU != nil && strings.EqualFold(*other.SKU, *item.SKU) {
			return true
		}
	}

	return false
}

func (r *fakeCatalogueRepository) Create(ctx context.Context, item storage.CatalogueItem) (storage.CatalogueItem, error) {
	tenant, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return storage.CatalogueItem{}, storage.ErrNoTenantScope
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// The row level security policy refuses a row of another tenant.
	if item.TenantID != tenant {
		return storage.CatalogueItem{}, storage.ErrNotFound
	}
	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}
	if item.Unit == "" {
		item.Unit = "piece"
	}
	if r.skuTaken(item) {
		return storage.CatalogueItem{}, storage.ErrConflict
	}
	now := time.Now().In(fakeLocation)
	item.CreatedAt, item.UpdatedAt = now, now
	r.rows[item.ID] = item

	return item, nil
}

func (r *fakeCatalogueRepository) FindByID(ctx context.Context, id uuid.UUID) (storage.CatalogueItem, error) {
	tenant, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return storage.CatalogueItem{}, storage.ErrNoTenantScope
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	i, ok := r.rows[id]
	if !ok || i.TenantID != tenant {
		return storage.CatalogueItem{}, storage.ErrNotFound
	}

	return i, nil
}

func (r *fakeCatalogueRepository) List(ctx context.Context) ([]storage.CatalogueItem, error) {
	tenant, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return nil, storage.ErrNoTenantScope
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var list []storage.CatalogueItem
	for _, i := range r.rows {
		if i.TenantID == tenant && i.DeletedAt == nil {
			list = append(list, i)
		}
	}
	sort.Slice(list, func(a, b int) bool { return list[a].Name < list[b].Name })

	return list, nil
}

func (r *fakeCatalogueRepository) Update(ctx context.Context, item storage.CatalogueItem) (storage.CatalogueItem, error) {
	tenant, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return storage.CatalogueItem{}, storage.ErrNoTenantScope
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.rows[item.ID]
	if !ok || existing.TenantID != tenant || existing.DeletedAt != nil {
		return storage.CatalogueItem{}, storage.ErrNotFound
	}
	item.TenantID = existing.TenantID
	if r.skuTaken(item) {
		return storage.CatalogueItem{}, storage.ErrConflict
	}
	item.CreatedAt = existing.CreatedAt
	item.UpdatedAt = time.Now().In(fakeLocation)
	r.rows[item.ID] = item

	return item, nil
}

func (r *fakeCatalogueRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tenant, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return storage.ErrNoTenantScope
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	i, ok := r.rows[id]
	if !ok || i.TenantID != tenant || i.DeletedAt != nil {
		return storage.ErrNotFound
	}
	now := time.Now().In(fakeLocation)
	i.DeletedAt = &now
	r.rows[id] = i

	return nil
}
