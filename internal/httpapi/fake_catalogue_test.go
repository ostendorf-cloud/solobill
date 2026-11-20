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
// active items, case insensitively. See fakeCustomerRepository for why
// the tenant is named through actAs.
type fakeCatalogueRepository struct {
	mu     sync.Mutex
	rows   map[uuid.UUID]storage.CatalogueItem
	acting uuid.UUID
}

func init() {
	repositoryHooks = append(repositoryHooks, func(_ *testing.T, repos *httpapi.Repositories) {
		repos.CatalogueItems = &fakeCatalogueRepository{rows: map[uuid.UUID]storage.CatalogueItem{}}
	})
}

// actAs sets the tenant every following call is filtered to.
func (r *fakeCatalogueRepository) actAs(tenantID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.acting = tenantID
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

func (r *fakeCatalogueRepository) Create(_ context.Context, item storage.CatalogueItem) (storage.CatalogueItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}
	if item.Unit == "" {
		item.Unit = "piece"
	}
	if r.skuTaken(item) {
		return storage.CatalogueItem{}, storage.ErrConflict
	}
	now := time.Now().UTC()
	item.CreatedAt, item.UpdatedAt = now, now
	r.rows[item.ID] = item

	return item, nil
}

func (r *fakeCatalogueRepository) FindByID(_ context.Context, id uuid.UUID) (storage.CatalogueItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	i, ok := r.rows[id]
	if !ok || i.TenantID != r.acting {
		return storage.CatalogueItem{}, storage.ErrNotFound
	}

	return i, nil
}

func (r *fakeCatalogueRepository) List(_ context.Context) ([]storage.CatalogueItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var list []storage.CatalogueItem
	for _, i := range r.rows {
		if i.TenantID == r.acting && i.DeletedAt == nil {
			list = append(list, i)
		}
	}
	sort.Slice(list, func(a, b int) bool { return list[a].Name < list[b].Name })

	return list, nil
}

func (r *fakeCatalogueRepository) Update(_ context.Context, item storage.CatalogueItem) (storage.CatalogueItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.rows[item.ID]
	if !ok || existing.TenantID != r.acting || existing.DeletedAt != nil {
		return storage.CatalogueItem{}, storage.ErrNotFound
	}
	item.TenantID = existing.TenantID
	if r.skuTaken(item) {
		return storage.CatalogueItem{}, storage.ErrConflict
	}
	item.CreatedAt = existing.CreatedAt
	item.UpdatedAt = time.Now().UTC()
	r.rows[item.ID] = item

	return item, nil
}

func (r *fakeCatalogueRepository) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	i, ok := r.rows[id]
	if !ok || i.TenantID != r.acting || i.DeletedAt != nil {
		return storage.ErrNotFound
	}
	now := time.Now().UTC()
	i.DeletedAt = &now
	r.rows[id] = i

	return nil
}
