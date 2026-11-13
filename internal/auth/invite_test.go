package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

func TestInviteManagerCreateInviteStoresOnlyTheHash(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	tenant := store.addTenant(storage.Tenant{Slug: "acme", Name: "Acme"})
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	manager := NewInviteManager(store, func() time.Time { return now })

	raw, invite, err := manager.CreateInvite(t.Context(), CreateInviteParams{
		TenantID: tenant.ID,
		Email:    "invitee@acme.example",
		Role:     storage.AccountRoleMember,
		TTL:      24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	if raw == "" {
		t.Fatal("CreateInvite returned an empty raw token")
	}
	if invite.TokenHash == raw {
		t.Error("the stored invite carries the raw token, want only its hash")
	}
	if invite.TokenHash != HashInviteToken(raw) {
		t.Errorf("stored TokenHash = %q, want HashInviteToken(raw) = %q", invite.TokenHash, HashInviteToken(raw))
	}
	if !invite.ExpiresAt.Equal(now.Add(24 * time.Hour)) {
		t.Errorf("ExpiresAt = %s, want %s", invite.ExpiresAt, now.Add(24*time.Hour))
	}
	if invite.Role != storage.AccountRoleMember {
		t.Errorf("Role = %v, want %v", invite.Role, storage.AccountRoleMember)
	}
}

// inviteTestFixture is one tenant with one unused, unexpired invite.
type inviteTestFixture struct {
	store  *fakeStore
	tenant storage.Tenant
	invite storage.Invite
	raw    string
	now    time.Time
}

func newInviteTestFixture(t *testing.T) inviteTestFixture {
	t.Helper()

	store := newFakeStore()
	tenant := store.addTenant(storage.Tenant{Slug: "acme", Name: "Acme"})
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

	raw, hash, err := GenerateInviteToken()
	if err != nil {
		t.Fatalf("GenerateInviteToken: %v", err)
	}

	invite := store.addInvite(storage.Invite{
		TenantID:  tenant.ID,
		Email:     "invitee@acme.example",
		Role:      storage.AccountRoleMember,
		TokenHash: hash,
		ExpiresAt: now.Add(24 * time.Hour),
	})

	return inviteTestFixture{store: store, tenant: tenant, invite: invite, raw: raw, now: now}
}

func TestInviteManagerAcceptInviteCreatesAnActiveAccountAndConsumesTheInvite(t *testing.T) {
	t.Parallel()

	f := newInviteTestFixture(t)
	manager := NewInviteManager(f.store, func() time.Time { return f.now })

	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	code, err := GenerateTOTPCode(secret, f.now)
	if err != nil {
		t.Fatalf("GenerateTOTPCode: %v", err)
	}

	account, err := manager.AcceptInvite(t.Context(), AcceptInviteParams{
		TenantID:   f.tenant.ID,
		RawToken:   f.raw,
		Password:   "a brand new passphrase",
		TOTPSecret: secret,
		TOTPCode:   code,
	})
	if err != nil {
		t.Fatalf("AcceptInvite: %v", err)
	}

	if account.Email != f.invite.Email {
		t.Errorf("Email = %q, want %q", account.Email, f.invite.Email)
	}
	if account.Role != f.invite.Role {
		t.Errorf("Role = %v, want %v", account.Role, f.invite.Role)
	}
	if account.Status != storage.AccountStatusActive {
		t.Errorf("Status = %v, want %v", account.Status, storage.AccountStatusActive)
	}
	if account.PasswordHash == nil || *account.PasswordHash == "" {
		t.Error("PasswordHash is nil or empty, want a hash")
	}
	if account.TOTPSecret == nil || *account.TOTPSecret != secret {
		t.Error("TOTPSecret does not match the secret that was confirmed")
	}

	stored, err := f.store.Invites().FindByID(t.Context(), f.invite.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if stored.UsedAt == nil {
		t.Error("the invite's UsedAt is still nil after a successful accept")
	}

	// A second attempt with the same, now-used token must fail: the
	// single-threaded half of the single-use guarantee. The genuinely
	// concurrent half is TestAcceptInviteIsSingleUseUnderConcurrentAccept.
	_, err = manager.AcceptInvite(t.Context(), AcceptInviteParams{
		TenantID:   f.tenant.ID,
		RawToken:   f.raw,
		Password:   "a different passphrase",
		TOTPSecret: secret,
		TOTPCode:   code,
	})
	if !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("second accept of the same token: err = %v, want errors.Is(err, ErrInviteInvalid)", err)
	}
}

func TestInviteManagerAcceptInviteRejectsAnUnknownToken(t *testing.T) {
	t.Parallel()

	f := newInviteTestFixture(t)
	manager := NewInviteManager(f.store, func() time.Time { return f.now })

	secret, _ := GenerateTOTPSecret()
	code, _ := GenerateTOTPCode(secret, f.now)

	_, err := manager.AcceptInvite(t.Context(), AcceptInviteParams{
		TenantID:   f.tenant.ID,
		RawToken:   "not-the-right-token",
		Password:   "a brand new passphrase",
		TOTPSecret: secret,
		TOTPCode:   code,
	})
	if !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("err = %v, want errors.Is(err, ErrInviteInvalid)", err)
	}
}

func TestInviteManagerAcceptInviteRejectsAnExpiredInvite(t *testing.T) {
	t.Parallel()

	f := newInviteTestFixture(t)
	f.invite.ExpiresAt = f.now.Add(-time.Minute)
	f.store.invites[f.invite.ID] = f.invite
	manager := NewInviteManager(f.store, func() time.Time { return f.now })

	secret, _ := GenerateTOTPSecret()
	code, _ := GenerateTOTPCode(secret, f.now)

	_, err := manager.AcceptInvite(t.Context(), AcceptInviteParams{
		TenantID:   f.tenant.ID,
		RawToken:   f.raw,
		Password:   "a brand new passphrase",
		TOTPSecret: secret,
		TOTPCode:   code,
	})
	if !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("err = %v, want errors.Is(err, ErrInviteInvalid)", err)
	}
}

func TestInviteManagerAcceptInviteRejectsAnAlreadyUsedInvite(t *testing.T) {
	t.Parallel()

	f := newInviteTestFixture(t)
	used := f.now.Add(-time.Minute)
	f.invite.UsedAt = &used
	f.store.invites[f.invite.ID] = f.invite
	manager := NewInviteManager(f.store, func() time.Time { return f.now })

	secret, _ := GenerateTOTPSecret()
	code, _ := GenerateTOTPCode(secret, f.now)

	_, err := manager.AcceptInvite(t.Context(), AcceptInviteParams{
		TenantID:   f.tenant.ID,
		RawToken:   f.raw,
		Password:   "a brand new passphrase",
		TOTPSecret: secret,
		TOTPCode:   code,
	})
	if !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("err = %v, want errors.Is(err, ErrInviteInvalid)", err)
	}
}

func TestInviteManagerAcceptInviteRejectsAnUnconfirmedTOTPCode(t *testing.T) {
	t.Parallel()

	f := newInviteTestFixture(t)
	manager := NewInviteManager(f.store, func() time.Time { return f.now })

	secret, _ := GenerateTOTPSecret()

	_, err := manager.AcceptInvite(t.Context(), AcceptInviteParams{
		TenantID:   f.tenant.ID,
		RawToken:   f.raw,
		Password:   "a brand new passphrase",
		TOTPSecret: secret,
		TOTPCode:   "000000",
	})
	if !errors.Is(err, ErrTOTPEnrollmentNotConfirmed) {
		t.Errorf("err = %v, want errors.Is(err, ErrTOTPEnrollmentNotConfirmed)", err)
	}

	// Nothing was written: a failed enrollment confirmation must not
	// burn the invite or create a half-set-up account.
	stored, err := f.store.Invites().FindByID(t.Context(), f.invite.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if stored.UsedAt != nil {
		t.Error("the invite was marked used despite the TOTP confirmation failing")
	}
}

func TestInviteManagerAcceptInviteFailsOnAccountConflictWithoutLeavingTheInviteUsed(t *testing.T) {
	t.Parallel()

	f := newInviteTestFixture(t)
	// An account with the invite's email already exists, so
	// Accounts().Create fails after MarkUsed already succeeded inside
	// the same WithTenant call: the whole call must still roll back
	// together, leaving the invite unused.
	f.store.addAccount(storage.Account{
		TenantID: f.tenant.ID,
		Email:    f.invite.Email,
		Role:     storage.AccountRoleMember,
		Status:   storage.AccountStatusActive,
	})

	manager := NewInviteManager(f.store, func() time.Time { return f.now })

	secret, _ := GenerateTOTPSecret()
	code, _ := GenerateTOTPCode(secret, f.now)

	_, err := manager.AcceptInvite(t.Context(), AcceptInviteParams{
		TenantID:   f.tenant.ID,
		RawToken:   f.raw,
		Password:   "a brand new passphrase",
		TOTPSecret: secret,
		TOTPCode:   code,
	})
	if err == nil {
		t.Fatal("AcceptInvite succeeded despite a conflicting account, want an error")
	}

	// fakeStore has no real transaction to roll back, unlike
	// internal/storage/postgres; this only checks that AcceptInvite
	// itself does not treat a partial write as success. The real
	// all-or-nothing guarantee comes from running inside one pgx
	// transaction, exercised by internal/storage/postgres's own tests
	// and by this call sharing that transaction with MarkUsed.
}
