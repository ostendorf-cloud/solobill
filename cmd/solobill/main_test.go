package main

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// TestCheckEmbeddedDist proves the embedded dist directory is shaped the
// way fs.Sub expects: a build with an empty or mis-shaped frontend build
// fails here instead of deep inside httpapi.NewHandler. run itself needs a
// full environment (a database, both signing keys) and a real Postgres to
// reach meaningfully, which this package does not have in a unit test; see
// internal/config and internal/httpapi for what is actually tested here.
func TestCheckEmbeddedDist(t *testing.T) {
	t.Parallel()

	if err := checkEmbeddedDist(); err != nil {
		t.Fatalf("checkEmbeddedDist() error = %v", err)
	}
}

// panicIfTenantsCalled is a tenantSeeder whose Tenants() panics: proof
// that seedTenant, called with an empty slug (production's default, see
// internal/config.Config.SeedTenantSlug), returns before it ever reads
// the store at all, rather than merely happening to find nothing to do.
type panicIfTenantsCalled struct{}

func (panicIfTenantsCalled) Tenants() storage.TenantRepository {
	panic("seedTenant must not call Tenants() when slug is empty")
}

// TestSeedTenantNoopWhenSlugEmpty covers the common, production case:
// SEED_TENANT_SLUG unset leaves Config.SeedTenantSlug empty, and
// seedTenant must do nothing at all, every docker-compose.yaml-only
// behaviour included.
func TestSeedTenantNoopWhenSlugEmpty(t *testing.T) {
	t.Parallel()

	if err := seedTenant(context.Background(), panicIfTenantsCalled{}, "", ""); err != nil {
		t.Fatalf("seedTenant() error = %v, want nil", err)
	}
}

// fakeOwnerSeederStore is a minimal ownerSeeder for seedOwnerAccount's own
// tests: no real transaction behind WithTenant, matching
// internal/auth's own fakeStore reasoning for why that is enough to unit
// test this package's logic (the real all-or-nothing guarantee is
// internal/storage/postgres's to prove).
type fakeOwnerSeederStore struct {
	tenants  map[string]storage.Tenant
	accounts []storage.Account
}

func (s *fakeOwnerSeederStore) WithTenant(ctx context.Context, _ uuid.UUID, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

func (s *fakeOwnerSeederStore) Tenants() storage.TenantRepository { return fakeOwnerTenantRepo{s} }

func (s *fakeOwnerSeederStore) Accounts() storage.AccountRepository { return &fakeOwnerAccountRepo{s} }

type fakeOwnerTenantRepo struct{ s *fakeOwnerSeederStore }

func (r fakeOwnerTenantRepo) Create(context.Context, storage.Tenant) (storage.Tenant, error) {
	panic("not used by seedOwnerAccount's own tests")
}

func (r fakeOwnerTenantRepo) FindByID(context.Context, uuid.UUID) (storage.Tenant, error) {
	panic("not used by seedOwnerAccount's own tests")
}

func (r fakeOwnerTenantRepo) FindBySlug(_ context.Context, slug string) (storage.Tenant, error) {
	tenant, ok := r.s.tenants[slug]
	if !ok {
		return storage.Tenant{}, storage.ErrNotFound
	}

	return tenant, nil
}

type fakeOwnerAccountRepo struct{ s *fakeOwnerSeederStore }

func (r *fakeOwnerAccountRepo) Create(_ context.Context, account storage.Account) (storage.Account, error) {
	if account.ID == uuid.Nil {
		account.ID = uuid.New()
	}

	r.s.accounts = append(r.s.accounts, account)

	return account, nil
}

func (r *fakeOwnerAccountRepo) FindByID(context.Context, uuid.UUID) (storage.Account, error) {
	panic("not used by seedOwnerAccount's own tests")
}

func (r *fakeOwnerAccountRepo) FindByEmail(_ context.Context, email string) (storage.Account, error) {
	for _, a := range r.s.accounts {
		if strings.EqualFold(a.Email, email) {
			return a, nil
		}
	}

	return storage.Account{}, storage.ErrNotFound
}

func (r *fakeOwnerAccountRepo) FindByOIDCSubject(_ context.Context, subject string) (storage.Account, error) {
	for _, a := range r.s.accounts {
		if a.OIDCSubject != nil && *a.OIDCSubject == subject {
			return a, nil
		}
	}

	return storage.Account{}, storage.ErrNotFound
}

func (r *fakeOwnerAccountRepo) List(_ context.Context) ([]storage.Account, error) {
	return r.s.accounts, nil
}

func (r *fakeOwnerAccountRepo) Update(context.Context, storage.Account) (storage.Account, error) {
	panic("not used by seedOwnerAccount's own tests")
}

// panicIfOwnerSeederCalled is an ownerSeeder whose every method panics:
// proof that seedOwnerAccount, called with an empty subject (production's
// default, see internal/config.Config.SeedOwnerOIDCSubject), returns
// before it ever reads the store at all.
type panicIfOwnerSeederCalled struct{}

func (panicIfOwnerSeederCalled) WithTenant(context.Context, uuid.UUID, func(context.Context) error) error {
	panic("seedOwnerAccount must not call WithTenant when subject is empty")
}

func (panicIfOwnerSeederCalled) Tenants() storage.TenantRepository {
	panic("seedOwnerAccount must not call Tenants() when subject is empty")
}

func (panicIfOwnerSeederCalled) Accounts() storage.AccountRepository {
	panic("seedOwnerAccount must not call Accounts() when subject is empty")
}

// TestSeedOwnerAccountNoopWhenSubjectEmpty covers the common, production
// case: SEED_OWNER_OIDC_SUBJECT unset leaves Config.SeedOwnerOIDCSubject
// empty, and seedOwnerAccount must do nothing at all.
func TestSeedOwnerAccountNoopWhenSubjectEmpty(t *testing.T) {
	t.Parallel()

	if err := seedOwnerAccount(context.Background(), panicIfOwnerSeederCalled{}, "", "", ""); err != nil {
		t.Fatalf("seedOwnerAccount() error = %v, want nil", err)
	}
}

// TestSeedOwnerAccountCreatesTheFirstOwner is the first-run bootstrap
// test: a fresh tenant with no accounts at all ends up with exactly one
// active owner account, bound to the configured subject, once
// seedOwnerAccount runs. That account is what a real OIDC login then
// resolves through ResolveOIDCIdentity, exactly like one AcceptInviteOIDC
// provisions.
func TestSeedOwnerAccountCreatesTheFirstOwner(t *testing.T) {
	t.Parallel()

	tenant := storage.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	store := &fakeOwnerSeederStore{tenants: map[string]storage.Tenant{tenant.Slug: tenant}}

	const (
		email   = "owner@acme.example"
		subject = "f:realm:the-owner"
	)

	if err := seedOwnerAccount(context.Background(), store, tenant.Slug, email, subject); err != nil {
		t.Fatalf("seedOwnerAccount() error = %v", err)
	}

	if len(store.accounts) != 1 {
		t.Fatalf("%d accounts exist after seeding, want exactly 1", len(store.accounts))
	}

	account := store.accounts[0]
	if account.TenantID != tenant.ID {
		t.Errorf("TenantID = %v, want %v", account.TenantID, tenant.ID)
	}
	if account.Email != email {
		t.Errorf("Email = %q, want %q", account.Email, email)
	}
	if account.OIDCSubject == nil || *account.OIDCSubject != subject {
		t.Error("OIDCSubject does not match the configured subject")
	}
	if account.Role != storage.AccountRoleOwner {
		t.Errorf("Role = %v, want %v", account.Role, storage.AccountRoleOwner)
	}
	if account.Status != storage.AccountStatusActive {
		t.Errorf("Status = %v, want %v", account.Status, storage.AccountStatusActive)
	}
}

// TestSeedOwnerAccountIsIdempotentWhenAlreadyProvisioned proves a second
// startup against the same, already-seeded instance (the common case
// after the very first one) creates no second account.
func TestSeedOwnerAccountIsIdempotentWhenAlreadyProvisioned(t *testing.T) {
	t.Parallel()

	tenant := storage.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	store := &fakeOwnerSeederStore{tenants: map[string]storage.Tenant{tenant.Slug: tenant}}

	const (
		email   = "owner@acme.example"
		subject = "f:realm:the-owner"
	)

	for range 2 {
		if err := seedOwnerAccount(context.Background(), store, tenant.Slug, email, subject); err != nil {
			t.Fatalf("seedOwnerAccount() error = %v", err)
		}
	}

	if len(store.accounts) != 1 {
		t.Errorf("%d accounts exist after seeding twice, want exactly 1", len(store.accounts))
	}
}

// TestSeedOwnerAccountDoesNotRebindAnExistingEmailToADifferentSubject is
// the bootstrap-level instance of this issue's core rule: an account's
// subject, once bound, is never rebound by a different subject
// presenting the same email, startup seeding included. A deployer who
// changes SEED_OWNER_OIDC_SUBJECT without also changing SEED_OWNER_EMAIL
// must not have the existing owner account silently rebound to a new
// identity on the next restart.
func TestSeedOwnerAccountDoesNotRebindAnExistingEmailToADifferentSubject(t *testing.T) {
	t.Parallel()

	tenant := storage.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	const email = "owner@acme.example"

	originalSubject := "f:realm:the-original-owner"
	store := &fakeOwnerSeederStore{
		tenants: map[string]storage.Tenant{tenant.Slug: tenant},
		accounts: []storage.Account{{
			ID:          uuid.New(),
			TenantID:    tenant.ID,
			Email:       email,
			OIDCSubject: &originalSubject,
			Role:        storage.AccountRoleOwner,
			Status:      storage.AccountStatusActive,
		}},
	}

	if err := seedOwnerAccount(context.Background(), store, tenant.Slug, email, "f:realm:a-different-subject"); err != nil {
		t.Fatalf("seedOwnerAccount() error = %v", err)
	}

	if len(store.accounts) != 1 {
		t.Fatalf("%d accounts exist after reseeding with a different subject, want exactly 1", len(store.accounts))
	}
	if *store.accounts[0].OIDCSubject != originalSubject {
		t.Errorf("OIDCSubject = %q, want it left at the original %q", *store.accounts[0].OIDCSubject, originalSubject)
	}
}
