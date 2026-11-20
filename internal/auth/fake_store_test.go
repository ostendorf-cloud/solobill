package auth

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/auth/authtest"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// fakeStore is a hand written fake satisfying Store, for a unit test that
// exercises the login adapters and InviteManager logic with no database.
//
// WithTenant here only runs fn: it does not simulate row level security
// or a real transaction, because nothing under unit test through this
// fake depends on either. Row level security itself is proved by
// internal/storage/postgres's TestRowLevelSecurity, against a real
// Postgres; MarkUsed's atomicity under a genuine concurrent race is
// proved by TestAcceptInviteIsSingleUseUnderConcurrentAccept in this
// package, also against a real Postgres (see invite_integration_test.go).
// This fake's own MarkUsed enforces the same "already used" rule under
// its mutex, which is enough to unit test InviteManager's logic, but it
// is not a proof of the race: a single process holding one mutex around
// two map operations cannot demonstrate what two separate Postgres
// transactions racing a row lock demonstrate.
type fakeStore struct {
	mu            sync.Mutex
	tenants       map[uuid.UUID]storage.Tenant
	tenantsBySlug map[string]uuid.UUID
	accounts      map[uuid.UUID]storage.Account
	invites       map[uuid.UUID]storage.Invite

	// login and passkeys sit on top of accounts: see authtest.
	login    *authtest.LoginAccounts
	passkeys *authtest.Passkeys

	// scopes records every tenant WithTenant ran for, in order.
	scopes []uuid.UUID
}

func newFakeStore() *fakeStore {
	s := &fakeStore{
		tenants:       map[uuid.UUID]storage.Tenant{},
		tenantsBySlug: map[string]uuid.UUID{},
		accounts:      map[uuid.UUID]storage.Account{},
		invites:       map[uuid.UUID]storage.Invite{},
	}
	s.login = &authtest.LoginAccounts{Accounts: &fakeAccountRepository{s}}
	s.passkeys = &authtest.Passkeys{Accounts: &fakeAccountRepository{s}}

	return s
}

func (s *fakeStore) WithTenant(ctx context.Context, tenantID uuid.UUID, fn func(ctx context.Context) error) error {
	s.mu.Lock()
	s.scopes = append(s.scopes, tenantID)
	s.mu.Unlock()

	return fn(ctx)
}

func (s *fakeStore) Tenants() storage.TenantRepository { return &fakeTenantRepository{s} }

func (s *fakeStore) Accounts() storage.AccountRepository           { return &fakeAccountRepository{s} }
func (s *fakeStore) LoginAccounts() storage.LoginAccountRepository { return s.login }
func (s *fakeStore) Passkeys() storage.PasskeyRepository           { return s.passkeys }
func (s *fakeStore) Invites() storage.InviteRepository             { return &fakeInviteRepository{s} }

// addTenant and addAccount are test setup helpers, bypassing the
// repository interfaces: a test fixture is not exercising Create.

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

func (s *fakeStore) addInvite(i storage.Invite) storage.Invite {
	s.mu.Lock()
	defer s.mu.Unlock()

	if i.ID == uuid.Nil {
		i.ID = uuid.New()
	}
	s.invites[i.ID] = i

	return i
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

func (r *fakeAccountRepository) FindByOIDCSubject(_ context.Context, subject string) (storage.Account, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	for _, a := range r.s.accounts {
		if a.OIDCSubject != nil && *a.OIDCSubject == subject {
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
	existing.TOTPLastCounter = account.TOTPLastCounter
	existing.OIDCSubject = account.OIDCSubject
	existing.Role = account.Role
	existing.Status = account.Status
	existing.UpdatedAt = time.Now()
	r.s.accounts[account.ID] = existing

	return existing, nil
}

type fakeInviteRepository struct{ s *fakeStore }

func (r *fakeInviteRepository) Create(_ context.Context, invite storage.Invite) (storage.Invite, error) {
	return r.s.addInvite(invite), nil
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

// MarkUsed mirrors the real postgres repository's "WHERE used_at IS NULL"
// guard under this fake's own mutex: already used fails with
// storage.ErrConflict, the same sentinel the real implementation uses.
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

// SetTOTPSecret mirrors the real postgres repository's "WHERE used_at IS
// NULL" guard under this fake's own mutex, the same way MarkUsed does:
// already used fails with storage.ErrConflict.
func (r *fakeInviteRepository) SetTOTPSecret(_ context.Context, id uuid.UUID, secret string) (storage.Invite, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	inv, ok := r.s.invites[id]
	if !ok {
		return storage.Invite{}, storage.ErrNotFound
	}
	if inv.UsedAt != nil {
		return storage.Invite{}, storage.ErrConflict
	}

	value := secret
	inv.TOTPSecret = &value
	r.s.invites[id] = inv

	return inv, nil
}
