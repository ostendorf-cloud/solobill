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

// fakeCustomerRepository is an in memory storage.CustomerRepository that
// mirrors the postgres one: FindByID answers a soft deleted row, List,
// Update and Delete do not, and a number is unique per tenant among
// active customers, case insensitively.
//
// fakeStore.WithTenant does not carry the tenant into the context, so the
// fake cannot read it back the way row level security does. A test names
// the tenant the next requests run as through actAs, the same tenant it
// logged in with. Create still stores the tenant the handler put on the
// customer, so a test can see that it came from the session.
type fakeCustomerRepository struct {
	mu     sync.Mutex
	rows   map[uuid.UUID]storage.Customer
	acting uuid.UUID
}

func init() {
	repositoryHooks = append(repositoryHooks, func(_ *testing.T, repos *httpapi.Repositories) {
		repos.Customers = &fakeCustomerRepository{rows: map[uuid.UUID]storage.Customer{}}
	})
}

// actAs sets the tenant every following call is filtered to.
func (r *fakeCustomerRepository) actAs(tenantID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.acting = tenantID
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

func (r *fakeCustomerRepository) Create(_ context.Context, c storage.Customer) (storage.Customer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if c.DocumentLanguage == "" {
		c.DocumentLanguage = storage.DocumentLanguageGerman
	}
	if r.numberTaken(c) {
		return storage.Customer{}, storage.ErrConflict
	}
	now := time.Now().UTC()
	c.CreatedAt, c.UpdatedAt = now, now
	r.rows[c.ID] = c

	return c, nil
}

func (r *fakeCustomerRepository) FindByID(_ context.Context, id uuid.UUID) (storage.Customer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	c, ok := r.rows[id]
	if !ok || c.TenantID != r.acting {
		return storage.Customer{}, storage.ErrNotFound
	}

	return c, nil
}

func (r *fakeCustomerRepository) List(_ context.Context) ([]storage.Customer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var list []storage.Customer
	for _, c := range r.rows {
		if c.TenantID == r.acting && c.DeletedAt == nil {
			list = append(list, c)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })

	return list, nil
}

func (r *fakeCustomerRepository) Update(_ context.Context, c storage.Customer) (storage.Customer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.rows[c.ID]
	if !ok || existing.TenantID != r.acting || existing.DeletedAt != nil {
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
	c.UpdatedAt = time.Now().UTC()
	r.rows[c.ID] = c

	return c, nil
}

func (r *fakeCustomerRepository) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	c, ok := r.rows[id]
	if !ok || c.TenantID != r.acting || c.DeletedAt != nil {
		return storage.ErrNotFound
	}
	now := time.Now().UTC()
	c.DeletedAt = &now
	r.rows[id] = c

	return nil
}
