package httpapi_test

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// fakeStore is a hand written fake satisfying auth.Store, for a handler
// test that exercises the real LocalAuthenticator and InviteManager logic
// behind internal/httpapi with no database. It mirrors internal/auth's own
// test fake; see that package's fake_store_test.go for why WithTenant need
// not simulate a real transaction here and why MarkUsed's atomicity under
// an actual concurrent race is proved elsewhere, against a real Postgres.
type fakeStore struct {
	mu            sync.Mutex
	tenants       map[uuid.UUID]storage.Tenant
	tenantsBySlug map[string]uuid.UUID
	accounts      map[uuid.UUID]storage.Account
	invites       map[uuid.UUID]storage.Invite
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		tenants:       map[uuid.UUID]storage.Tenant{},
		tenantsBySlug: map[string]uuid.UUID{},
		accounts:      map[uuid.UUID]storage.Account{},
		invites:       map[uuid.UUID]storage.Invite{},
	}
}

func (s *fakeStore) WithTenant(ctx context.Context, _ uuid.UUID, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

func (s *fakeStore) Tenants() storage.TenantRepository   { return &fakeTenantRepository{s} }
func (s *fakeStore) Accounts() storage.AccountRepository { return &fakeAccountRepository{s} }
func (s *fakeStore) Invites() storage.InviteRepository   { return &fakeInviteRepository{s} }

func (s *fakeStore) addTenant(t storage.Tenant) storage.Tenant {
	s.mu.Lock()
	defer s.mu.Unlock()

	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	s.tenants[t.ID] = t
	s.tenantsBySlug[t.Slug] = t.ID

	return t
}

func (s *fakeStore) addAccount(a storage.Account) storage.Account {
	s.mu.Lock()
	defer s.mu.Unlock()

	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	s.accounts[a.ID] = a

	return a
}

type fakeTenantRepository struct{ s *fakeStore }

func (r *fakeTenantRepository) Create(_ context.Context, tenant storage.Tenant) (storage.Tenant, error) {
	return r.s.addTenant(tenant), nil
}

func (r *fakeTenantRepository) FindByID(_ context.Context, id uuid.UUID) (storage.Tenant, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	t, ok := r.s.tenants[id]
	if !ok {
		return storage.Tenant{}, storage.ErrNotFound
	}

	return t, nil
}

func (r *fakeTenantRepository) FindBySlug(_ context.Context, slug string) (storage.Tenant, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	id, ok := r.s.tenantsBySlug[slug]
	if !ok {
		return storage.Tenant{}, storage.ErrNotFound
	}

	return r.s.tenants[id], nil
}

type fakeAccountRepository struct{ s *fakeStore }

func (r *fakeAccountRepository) Create(_ context.Context, account storage.Account) (storage.Account, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	for _, existing := range r.s.accounts {
		if existing.TenantID == account.TenantID && strings.EqualFold(existing.Email, account.Email) {
			return storage.Account{}, storage.ErrConflict
		}
	}

	if account.ID == uuid.Nil {
		account.ID = uuid.New()
	}
	now := time.Now()
	account.CreatedAt = now
	account.UpdatedAt = now
	r.s.accounts[account.ID] = account

	return account, nil
}

func (r *fakeAccountRepository) FindByID(_ context.Context, id uuid.UUID) (storage.Account, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	a, ok := r.s.accounts[id]
	if !ok {
		return storage.Account{}, storage.ErrNotFound
	}

	return a, nil
}

func (r *fakeAccountRepository) FindByEmail(_ context.Context, email string) (storage.Account, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	for _, a := range r.s.accounts {
		if strings.EqualFold(a.Email, email) {
			return a, nil
		}
	}

	return storage.Account{}, storage.ErrNotFound
}

func (r *fakeAccountRepository) List(_ context.Context) ([]storage.Account, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	list := make([]storage.Account, 0, len(r.s.accounts))
	for _, a := range r.s.accounts {
		list = append(list, a)
	}

	return list, nil
}

func (r *fakeAccountRepository) Update(_ context.Context, account storage.Account) (storage.Account, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	existing, ok := r.s.accounts[account.ID]
	if !ok {
		return storage.Account{}, storage.ErrNotFound
	}

	existing.PasswordHash = account.PasswordHash
	existing.TOTPSecret = account.TOTPSecret
	existing.Role = account.Role
	existing.Status = account.Status
	existing.UpdatedAt = time.Now()
	r.s.accounts[account.ID] = existing

	return existing, nil
}

type fakeInviteRepository struct{ s *fakeStore }

func (r *fakeInviteRepository) Create(_ context.Context, invite storage.Invite) (storage.Invite, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	if invite.ID == uuid.Nil {
		invite.ID = uuid.New()
	}
	r.s.invites[invite.ID] = invite

	return invite, nil
}

func (r *fakeInviteRepository) FindByID(_ context.Context, id uuid.UUID) (storage.Invite, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	inv, ok := r.s.invites[id]
	if !ok {
		return storage.Invite{}, storage.ErrNotFound
	}

	return inv, nil
}

func (r *fakeInviteRepository) FindByTokenHash(_ context.Context, tokenHash string) (storage.Invite, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	for _, inv := range r.s.invites {
		if inv.TokenHash == tokenHash {
			return inv, nil
		}
	}

	return storage.Invite{}, storage.ErrNotFound
}

func (r *fakeInviteRepository) MarkUsed(_ context.Context, id uuid.UUID, usedAt time.Time) (storage.Invite, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	inv, ok := r.s.invites[id]
	if !ok {
		return storage.Invite{}, storage.ErrNotFound
	}
	if inv.UsedAt != nil {
		return storage.Invite{}, storage.ErrConflict
	}

	used := usedAt
	inv.UsedAt = &used
	r.s.invites[id] = inv

	return inv, nil
}
