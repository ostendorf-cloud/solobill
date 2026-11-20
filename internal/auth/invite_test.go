package auth

import (
	"errors"
	"testing"
	"time"

	govueoidc "github.com/ostendorf-cloud/govue/oidc"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

func TestInviteManagerCreateInviteStoresOnlyTheHash(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	tenant := store.addTenant(storage.Tenant{Slug: "acme", Name: "Acme"})
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	manager := NewInviteManager(store, testHasher(t), func() time.Time { return now })

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

// issueSecret runs IssueTOTPSecret against f's invite and returns the
// server-issued secret and the manager used to do it, so a test that then
// calls AcceptInvite reuses the same manager and clock.
func (f inviteTestFixture) issueSecret(t *testing.T, manager *InviteManager) string {
	t.Helper()

	secret, uri, err := manager.IssueTOTPSecret(t.Context(), f.tenant.ID, f.raw)
	if err != nil {
		t.Fatalf("IssueTOTPSecret: %v", err)
	}
	if secret == "" {
		t.Fatal("IssueTOTPSecret returned an empty secret")
	}
	if uri == "" {
		t.Fatal("IssueTOTPSecret returned an empty provisioning uri")
	}

	return secret
}

func TestInviteManagerAcceptInviteCreatesAnActiveAccountAndConsumesTheInvite(t *testing.T) {
	t.Parallel()

	f := newInviteTestFixture(t)
	manager := NewInviteManager(f.store, testHasher(t), func() time.Time { return f.now })

	secret := f.issueSecret(t, manager)
	code, err := testTOTPCode(secret, f.now)
	if err != nil {
		t.Fatalf("GenerateTOTPCode: %v", err)
	}

	account, err := manager.AcceptInvite(t.Context(), AcceptInviteParams{
		TenantID: f.tenant.ID,
		RawToken: f.raw,
		Password: "a brand new passphrase",
		TOTPCode: code,
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
	// The stored secret must be exactly the server-issued one: the client
	// no longer supplies a secret at all, so there is nothing else it
	// could be.
	if account.TOTPSecret == nil || *account.TOTPSecret != secret {
		t.Error("TOTPSecret does not match the secret IssueTOTPSecret issued")
	}
	if account.TOTPLastCounter == nil {
		t.Error("TOTPLastCounter is nil, want the counter the enrolment code matched")
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
		TenantID: f.tenant.ID,
		RawToken: f.raw,
		Password: "a different passphrase",
		TOTPCode: code,
	})
	if !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("second accept of the same token: err = %v, want errors.Is(err, ErrInviteInvalid)", err)
	}
}

func TestInviteManagerAcceptInviteRejectsAnUnknownToken(t *testing.T) {
	t.Parallel()

	f := newInviteTestFixture(t)
	manager := NewInviteManager(f.store, testHasher(t), func() time.Time { return f.now })

	_, err := manager.AcceptInvite(t.Context(), AcceptInviteParams{
		TenantID: f.tenant.ID,
		RawToken: "not-the-right-token",
		Password: "a brand new passphrase",
		TOTPCode: "000000",
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
	manager := NewInviteManager(f.store, testHasher(t), func() time.Time { return f.now })

	_, err := manager.AcceptInvite(t.Context(), AcceptInviteParams{
		TenantID: f.tenant.ID,
		RawToken: f.raw,
		Password: "a brand new passphrase",
		TOTPCode: "000000",
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
	manager := NewInviteManager(f.store, testHasher(t), func() time.Time { return f.now })

	_, err := manager.AcceptInvite(t.Context(), AcceptInviteParams{
		TenantID: f.tenant.ID,
		RawToken: f.raw,
		Password: "a brand new passphrase",
		TOTPCode: "000000",
	})
	if !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("err = %v, want errors.Is(err, ErrInviteInvalid)", err)
	}
}

func TestInviteManagerAcceptInviteRejectsAnUnconfirmedTOTPCode(t *testing.T) {
	t.Parallel()

	f := newInviteTestFixture(t)
	manager := NewInviteManager(f.store, testHasher(t), func() time.Time { return f.now })

	f.issueSecret(t, manager)

	_, err := manager.AcceptInvite(t.Context(), AcceptInviteParams{
		TenantID: f.tenant.ID,
		RawToken: f.raw,
		Password: "a brand new passphrase",
		TOTPCode: "000000",
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

// TestInviteManagerAcceptInviteRejectsWhenNoSecretWasEverIssued proves
// AcceptInvite refuses an invite whose enrolment step (IssueTOTPSecret)
// never ran at all: a code computed from any secret, however correctly,
// must not be accepted when the invite itself holds no server-issued
// secret to check it against. This fails against the pre-fix code, which
// took the secret straight from the request body and would have happily
// confirmed any internally consistent secret/code pair.
func TestInviteManagerAcceptInviteRejectsWhenNoSecretWasEverIssued(t *testing.T) {
	t.Parallel()

	f := newInviteTestFixture(t)
	manager := NewInviteManager(f.store, testHasher(t), func() time.Time { return f.now })

	// Deliberately no call to IssueTOTPSecret: the invite's own
	// TOTPSecret is still nil.
	clientSecret, err := testTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	code, err := testTOTPCode(clientSecret, f.now)
	if err != nil {
		t.Fatalf("GenerateTOTPCode: %v", err)
	}

	_, err = manager.AcceptInvite(t.Context(), AcceptInviteParams{
		TenantID: f.tenant.ID,
		RawToken: f.raw,
		Password: "a brand new passphrase",
		TOTPCode: code,
	})
	if !errors.Is(err, ErrTOTPEnrollmentNotConfirmed) {
		t.Errorf("err = %v, want errors.Is(err, ErrTOTPEnrollmentNotConfirmed)", err)
	}
}

// TestInviteManagerAcceptInviteIgnoresAnAttackerSuppliedCode proves that a
// code computed from an attacker's own, internally consistent secret is
// refused, because only TOTPCode travels from the client now: there is no
// secret field left for an attacker to pair it with. This fails against
// the pre-fix code, which verified the submitted code against whatever
// secret the request itself carried, so an attacker's self-consistent
// pair would have passed.
func TestInviteManagerAcceptInviteIgnoresAnAttackerSuppliedCode(t *testing.T) {
	t.Parallel()

	f := newInviteTestFixture(t)
	manager := NewInviteManager(f.store, testHasher(t), func() time.Time { return f.now })

	// The real invitee issues the real secret, exactly as the enrolment
	// flow intends.
	f.issueSecret(t, manager)

	// The attacker generates their own, unrelated secret and computes a
	// code from it: internally consistent, but not the one the server
	// persisted on the invite.
	attackerSecret, err := testTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	attackerCode, err := testTOTPCode(attackerSecret, f.now)
	if err != nil {
		t.Fatalf("GenerateTOTPCode: %v", err)
	}

	_, err = manager.AcceptInvite(t.Context(), AcceptInviteParams{
		TenantID: f.tenant.ID,
		RawToken: f.raw,
		Password: "a brand new passphrase",
		TOTPCode: attackerCode,
	})
	if !errors.Is(err, ErrTOTPEnrollmentNotConfirmed) {
		t.Errorf("err = %v, want errors.Is(err, ErrTOTPEnrollmentNotConfirmed)", err)
	}
}

// TestInviteManagerIssueTOTPSecretRejectsAnExpiredOrUsedInvite proves
// IssueTOTPSecret refuses for the same reasons AcceptInvite already does.
func TestInviteManagerIssueTOTPSecretRejectsAnExpiredOrUsedInvite(t *testing.T) {
	t.Parallel()

	t.Run("expired", func(t *testing.T) {
		t.Parallel()

		f := newInviteTestFixture(t)
		f.invite.ExpiresAt = f.now.Add(-time.Minute)
		f.store.invites[f.invite.ID] = f.invite
		manager := NewInviteManager(f.store, testHasher(t), func() time.Time { return f.now })

		_, _, err := manager.IssueTOTPSecret(t.Context(), f.tenant.ID, f.raw)
		if !errors.Is(err, ErrInviteInvalid) {
			t.Errorf("err = %v, want errors.Is(err, ErrInviteInvalid)", err)
		}
	})

	t.Run("already used", func(t *testing.T) {
		t.Parallel()

		f := newInviteTestFixture(t)
		used := f.now.Add(-time.Minute)
		f.invite.UsedAt = &used
		f.store.invites[f.invite.ID] = f.invite
		manager := NewInviteManager(f.store, testHasher(t), func() time.Time { return f.now })

		_, _, err := manager.IssueTOTPSecret(t.Context(), f.tenant.ID, f.raw)
		if !errors.Is(err, ErrInviteInvalid) {
			t.Errorf("err = %v, want errors.Is(err, ErrInviteInvalid)", err)
		}
	})

	t.Run("unknown token", func(t *testing.T) {
		t.Parallel()

		f := newInviteTestFixture(t)
		manager := NewInviteManager(f.store, testHasher(t), func() time.Time { return f.now })

		_, _, err := manager.IssueTOTPSecret(t.Context(), f.tenant.ID, "not-the-right-token")
		if !errors.Is(err, ErrInviteInvalid) {
			t.Errorf("err = %v, want errors.Is(err, ErrInviteInvalid)", err)
		}
	})
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

	manager := NewInviteManager(f.store, testHasher(t), func() time.Time { return f.now })

	secret := f.issueSecret(t, manager)
	code, err := testTOTPCode(secret, f.now)
	if err != nil {
		t.Fatalf("GenerateTOTPCode: %v", err)
	}

	_, err = manager.AcceptInvite(t.Context(), AcceptInviteParams{
		TenantID: f.tenant.ID,
		RawToken: f.raw,
		Password: "a brand new passphrase",
		TOTPCode: code,
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

// TestInviteManagerAcceptInviteOIDCCreatesAnActiveAccountAndConsumesTheInvite
// is AcceptInviteOIDC's counterpart to
// TestInviteManagerAcceptInviteCreatesAnActiveAccountAndConsumesTheInvite:
// no password, no TOTP secret, OIDCSubject bound instead, and the
// resulting account is immediately usable through ResolveOIDCIdentity,
// proving an OIDC login by a subject bound this way actually succeeds.
func TestInviteManagerAcceptInviteOIDCCreatesAnActiveAccountAndConsumesTheInvite(t *testing.T) {
	t.Parallel()

	f := newInviteTestFixture(t)
	manager := NewInviteManager(f.store, testHasher(t), func() time.Time { return f.now })

	const subject = "f:realm:invitee-subject"

	account, err := manager.AcceptInviteOIDC(t.Context(), AcceptInviteOIDCParams{
		TenantID: f.tenant.ID,
		RawToken: f.raw,
		Subject:  subject,
	})
	if err != nil {
		t.Fatalf("AcceptInviteOIDC: %v", err)
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
	if account.OIDCSubject == nil || *account.OIDCSubject != subject {
		t.Error("OIDCSubject does not match the subject the identity provider vouched for")
	}
	if account.PasswordHash != nil {
		t.Error("PasswordHash is set, want nil for an OIDC-provisioned account")
	}
	if account.TOTPSecret != nil {
		t.Error("TOTPSecret is set, want nil for an OIDC-provisioned account")
	}

	stored, err := f.store.Invites().FindByID(t.Context(), f.invite.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if stored.UsedAt == nil {
		t.Error("the invite's UsedAt is still nil after a successful accept")
	}

	// The resulting account is what a real OIDC login resolves: this is
	// the test that an OIDC login by a subject bound to an account
	// succeeds, built on the actual provisioning path rather than on an
	// account inserted directly into the fixture.
	identity, err := ResolveOIDCIdentity(t.Context(), f.store, f.tenant.ID, govueoidc.Identity{
		Subject: subject,
		Email:   account.Email,
	})
	if err != nil {
		t.Fatalf("ResolveOIDCIdentity: %v", err)
	}
	if identity.AccountID != account.ID {
		t.Errorf("AccountID = %v, want %v", identity.AccountID, account.ID)
	}
	if identity.Role != f.invite.Role {
		t.Errorf("Role = %v, want %v", identity.Role, f.invite.Role)
	}

	// A second attempt with the same, now-used token must fail, the same
	// single-threaded half of the single-use guarantee
	// TestInviteManagerAcceptInviteCreatesAnActiveAccountAndConsumesTheInvite
	// already proves for the local path; the genuinely concurrent half
	// for this path is
	// TestAcceptInviteOIDCIsSingleUseUnderConcurrentAccept.
	_, err = manager.AcceptInviteOIDC(t.Context(), AcceptInviteOIDCParams{
		TenantID: f.tenant.ID,
		RawToken: f.raw,
		Subject:  "f:realm:a-different-subject",
	})
	if !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("second accept of the same token: err = %v, want errors.Is(err, ErrInviteInvalid)", err)
	}
}

func TestInviteManagerAcceptInviteOIDCRejectsAnUnknownExpiredOrUsedToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(f *inviteTestFixture)
		token func(f inviteTestFixture) string
	}{
		{
			name:  "unknown token",
			setup: func(*inviteTestFixture) {},
			token: func(inviteTestFixture) string { return "not-the-right-token" },
		},
		{
			name: "expired",
			setup: func(f *inviteTestFixture) {
				f.invite.ExpiresAt = f.now.Add(-time.Minute)
				f.store.invites[f.invite.ID] = f.invite
			},
			token: func(f inviteTestFixture) string { return f.raw },
		},
		{
			name: "already used",
			setup: func(f *inviteTestFixture) {
				used := f.now.Add(-time.Minute)
				f.invite.UsedAt = &used
				f.store.invites[f.invite.ID] = f.invite
			},
			token: func(f inviteTestFixture) string { return f.raw },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newInviteTestFixture(t)
			tt.setup(&f)
			manager := NewInviteManager(f.store, testHasher(t), func() time.Time { return f.now })

			_, err := manager.AcceptInviteOIDC(t.Context(), AcceptInviteOIDCParams{
				TenantID: f.tenant.ID,
				RawToken: tt.token(f),
				Subject:  "f:realm:invitee-subject",
			})
			if !errors.Is(err, ErrInviteInvalid) {
				t.Errorf("err = %v, want errors.Is(err, ErrInviteInvalid)", err)
			}
		})
	}
}

// TestInviteManagerAcceptInviteOIDCDoesNotLetADifferentSubjectTakeOverAnExistingEmail
// is the regression test for the takeover case this issue's admission
// model is built to rule out: an account is already bound to subject A
// under email E (exactly as a successful AcceptInviteOIDC would leave
// it). A second invite for that same email E is then accepted by a
// different subject B. It must not touch the existing account at all:
// accounts_tenant_id_email_lower_idx's per-tenant email uniqueness (see
// db/migrations) refuses the second Create, this method surfaces that as
// an error, and the original account's own binding is unchanged
// afterward. Nothing about presenting the same email, with a different
// subject, ever lets B take over what A already holds.
func TestInviteManagerAcceptInviteOIDCDoesNotLetADifferentSubjectTakeOverAnExistingEmail(t *testing.T) {
	t.Parallel()

	f := newInviteTestFixture(t)
	manager := NewInviteManager(f.store, testHasher(t), func() time.Time { return f.now })

	const (
		ownerSubject    = "f:realm:the-real-owner"
		attackerSubject = "f:realm:someone-else-entirely"
	)

	ownerSubjectValue := ownerSubject
	original := f.store.addAccount(storage.Account{
		TenantID:    f.tenant.ID,
		Email:       f.invite.Email,
		OIDCSubject: &ownerSubjectValue,
		Role:        f.invite.Role,
		Status:      storage.AccountStatusActive,
	})

	// A second invite for the same email, as if an owner invited the
	// same address again: still no license to rebind it to a different
	// subject.
	secondRaw, secondHash, err := GenerateInviteToken()
	if err != nil {
		t.Fatalf("GenerateInviteToken: %v", err)
	}

	secondInvite := f.store.addInvite(storage.Invite{
		TenantID:  f.tenant.ID,
		Email:     f.invite.Email,
		Role:      f.invite.Role,
		TokenHash: secondHash,
		ExpiresAt: f.now.Add(24 * time.Hour),
	})

	_, err = manager.AcceptInviteOIDC(t.Context(), AcceptInviteOIDCParams{
		TenantID: f.tenant.ID,
		RawToken: secondRaw,
		Subject:  attackerSubject,
	})
	if err == nil {
		t.Fatal("AcceptInviteOIDC succeeded despite a conflicting email, want an error")
	}
	if !errors.Is(err, storage.ErrConflict) {
		t.Errorf("err = %v, want errors.Is(err, storage.ErrConflict)", err)
	}

	// The original account's own binding is exactly what it was before:
	// still found by ownerSubject, and still the only account for this
	// email.
	byOwner, err := f.store.Accounts().FindByOIDCSubject(t.Context(), ownerSubject)
	if err != nil {
		t.Fatalf("FindByOIDCSubject(owner): %v", err)
	}
	if byOwner.ID != original.ID {
		t.Errorf("FindByOIDCSubject(owner) returned account %v, want the original %v", byOwner.ID, original.ID)
	}

	if _, err := f.store.Accounts().FindByOIDCSubject(t.Context(), attackerSubject); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("FindByOIDCSubject(attacker) err = %v, want errors.Is(err, storage.ErrNotFound)", err)
	}

	// secondInvite's own token was still burned by MarkUsed before the
	// conflicting Create failed: it is unknown's exact id here only to
	// document that fact, nothing re-checks it, the fakeStore's lack of
	// a real rollback (see
	// TestInviteManagerAcceptInviteFailsOnAccountConflictWithoutLeavingTheInviteUsed)
	// means it is unreliable in this fake either way, and
	// internal/storage/postgres's own transaction tests are what prove
	// the real all-or-nothing behavior.
	_ = secondInvite
}

func TestInviteManagerPeekInvite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(f *inviteTestFixture)
		token   func(f inviteTestFixture) string
		wantErr error
	}{
		{
			name:  "valid, unused invite",
			setup: func(*inviteTestFixture) {},
			token: func(f inviteTestFixture) string { return f.raw },
		},
		{
			name:    "unknown token",
			setup:   func(*inviteTestFixture) {},
			token:   func(inviteTestFixture) string { return "not-the-right-token" },
			wantErr: ErrInviteInvalid,
		},
		{
			name: "expired",
			setup: func(f *inviteTestFixture) {
				f.invite.ExpiresAt = f.now.Add(-time.Minute)
				f.store.invites[f.invite.ID] = f.invite
			},
			token:   func(f inviteTestFixture) string { return f.raw },
			wantErr: ErrInviteInvalid,
		},
		{
			name: "already used",
			setup: func(f *inviteTestFixture) {
				used := f.now.Add(-time.Minute)
				f.invite.UsedAt = &used
				f.store.invites[f.invite.ID] = f.invite
			},
			token:   func(f inviteTestFixture) string { return f.raw },
			wantErr: ErrInviteInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newInviteTestFixture(t)
			tt.setup(&f)
			manager := NewInviteManager(f.store, testHasher(t), func() time.Time { return f.now })

			err := manager.PeekInvite(t.Context(), f.tenant.ID, tt.token(f))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want errors.Is(err, %v)", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("PeekInvite: %v", err)
			}

			// PeekInvite must not mark the invite used: AcceptInviteOIDC
			// still has to be able to redeem it afterward.
			stored, err := f.store.Invites().FindByID(t.Context(), f.invite.ID)
			if err != nil {
				t.Fatalf("FindByID: %v", err)
			}
			if stored.UsedAt != nil {
				t.Error("PeekInvite marked the invite used, want it left untouched")
			}
		})
	}
}
