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

// fakeLocation is a zone other than UTC. The fakes stamp their rows in
// it, the way a database driver returns a timestamptz in the session
// zone, so a test sees whether the API converts to UTC.
var fakeLocation = time.FixedZone("CET", 3600)

// fakeCustomerRepository is an in memory storage.CustomerRepository that
// mirrors the postgres one: FindByID answers a soft deleted row, List,
// Update and Delete do not, and a number is unique per tenant among
// active customers, case insensitively.
//
// It filters by the tenant the handler opened the scope for, read back
// through httpapi.ScopedTenant, the way row level security filters by the
// tenant of the transaction. A context without one fails like the real
// repository does.
type fakeCustomerRepository struct {
	mu   sync.Mutex
	rows map[uuid.UUID]storage.Customer
}

func init() {
	repositoryHooks = append(repositoryHooks, func(_ *testing.T, repos *httpapi.Repositories) {
		repos.Customers = &fakeCustomerRepository{rows: map[uuid.UUID]storage.Customer{}}
	})
}

// row reads a stored row back, whatever its tenant or state.
func (r *fakeCustomerRepository) row(id uuid.UUID) storage.Customer {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.rows[id]
}

func (r *fakeCustomerRepository) numberTaken(c storage.Customer) bool {
	if c.Number == nil {
		return false
	}
	for _, other := range r.rows {
		if other.ID != c.ID && other.TenantID == c.TenantID && other.DeletedAt == nil &&
			other.Number != nil && strings.EqualFold(*other.Number, *c.Number) {
			return true
		}
	}

	return false
}

func (r *fakeCustomerRepository) Create(ctx context.Context, c storage.Customer) (storage.Customer, error) {
	tenant, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return storage.Customer{}, storage.ErrNoTenantScope
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// The row level security policy refuses a row of another tenant.
	if c.TenantID != tenant {
		return storage.Customer{}, storage.ErrNotFound
	}
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if c.DocumentLanguage == "" {
		c.DocumentLanguage = storage.DocumentLanguageGerman
	}
	if r.numberTaken(c) {
		return storage.Customer{}, storage.ErrConflict
	}
	now := time.Now().In(fakeLocation)
	c.CreatedAt, c.UpdatedAt = now, now
	r.rows[c.ID] = c

	return c, nil
}

func (r *fakeCustomerRepository) FindByID(ctx context.Context, id uuid.UUID) (storage.Customer, error) {
	tenant, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return storage.Customer{}, storage.ErrNoTenantScope
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	c, ok := r.rows[id]
	if !ok || c.TenantID != tenant {
		return storage.Customer{}, storage.ErrNotFound
	}

	return c, nil
}

func (r *fakeCustomerRepository) List(ctx context.Context) ([]storage.Customer, error) {
	tenant, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return nil, storage.ErrNoTenantScope
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var list []storage.Customer
	for _, c := range r.rows {
		if c.TenantID == tenant && c.DeletedAt == nil {
			list = append(list, c)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })

	return list, nil
}

func (r *fakeCustomerRepository) Update(ctx context.Context, c storage.Customer) (storage.Customer, error) {
	tenant, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return storage.Customer{}, storage.ErrNoTenantScope
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.rows[c.ID]
	if !ok || existing.TenantID != tenant || existing.DeletedAt != nil {
		return storage.Customer{}, storage.ErrNotFound
	}
	if c.DocumentLanguage == "" {
		c.DocumentLanguage = storage.DocumentLanguageGerman
	}
	c.TenantID = existing.TenantID
	if r.numberTaken(c) {
		return storage.Customer{}, storage.ErrConflict
	}
	c.CreatedAt = existing.CreatedAt
	c.UpdatedAt = time.Now().In(fakeLocation)
	r.rows[c.ID] = c

	return c, nil
}

func (r *fakeCustomerRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tenant, ok := httpapi.ScopedTenant(ctx)
	if !ok {
		return storage.ErrNoTenantScope
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	c, ok := r.rows[id]
	if !ok || c.TenantID != tenant || c.DeletedAt != nil {
		return storage.ErrNotFound
	}
	now := time.Now().In(fakeLocation)
	c.DeletedAt = &now
	r.rows[id] = c

	return nil
}
