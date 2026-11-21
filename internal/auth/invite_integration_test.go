//go:build integration

// This file needs a real, reachable Postgres, the same way
// internal/storage/postgres's own TestRowLevelSecurity does: excluded
// from a plain "go test ./..." by the build tag above, and skipped at
// runtime without DATABASE_URL. "task test:integration" is the only
// Taskfile target that passes "-tags=integration".
//
// Unlike TestRowLevelSecurity, this test runs as DATABASE_URL's own role
// directly, not through the restricted, NOBYPASSRLS testAppRole that
// test sets up. What this test proves is MarkUsed's row locking under a
// genuine concurrent race, not row level security; running as a
// superuser does not change whether two concurrent UPDATEs against the
// same row can both succeed, which is the thing actually under test.
package auth_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/account"

	"github.com/ostendorf-cloud/solobill/internal/auth"
	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres"
)

// TestAcceptInviteIsSingleUseUnderConcurrentAccept races two concurrent
// calls to InviteManager.AcceptInvite against the same invite token, and
// asserts that exactly one of them succeeds: the single-use guarantee
// InviteRepository.MarkUsed's "WHERE used_at IS NULL" guard is meant to
// hold under concurrency, not only in a single-threaded happy path.
func TestAcceptInviteIsSingleUseUnderConcurrentAccept(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed invite race test")
	}

	ctx := context.Background()

	pool, err := postgres.NewPool(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	store := postgres.NewStore(pool)

	tenant, err := store.Tenants().Create(ctx, storage.Tenant{
		Slug: "invite-race-" + uuid.NewString(),
		Name: "Invite Race Tenant",
	})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	manager := auth.NewInviteManager(store, integrationHasher(t), time.Now)

	rawToken, invite, err := manager.CreateInvite(ctx, auth.CreateInviteParams{
		TenantID: tenant.ID,
		Email:    "racer@invite-race.example",
		Role:     storage.AccountRoleMember,
		TTL:      time.Hour,
	})
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	secret, _, err := manager.IssueTOTPSecret(ctx, tenant.ID, rawToken)
	if err != nil {
		t.Fatalf("IssueTOTPSecret: %v", err)
	}

	const attempts = 10

	codes := make([]string, attempts)

	for i := range attempts {
		code, err := account.TOTPCode(secret, time.Now())
		if err != nil {
			t.Fatalf("TOTPCode: %v", err)
		}
		codes[i] = code
	}

	var (
		wg        sync.WaitGroup
		succeeded atomic.Int32
		failed    atomic.Int32
		start     = make(chan struct{})
	)

	for i := range attempts {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			<-start

			_, err := manager.AcceptInvite(ctx, auth.AcceptInviteParams{
				TenantID: tenant.ID,
				RawToken: rawToken,
				Password: "a racing passphrase",
				TOTPCode: codes[i],
			})
			if err == nil {
				succeeded.Add(1)

				return
			}
			if !errors.Is(err, auth.ErrInviteInvalid) {
				t.Errorf("attempt %d: unexpected error: %v", i, err)

				return
			}
			failed.Add(1)
		}(i)
	}

	close(start)
	wg.Wait()

	if got := succeeded.Load(); got != 1 {
		t.Errorf("%d of %d concurrent accept attempts succeeded, want exactly 1", got, attempts)
	}
	if got := failed.Load(); got != attempts-1 {
		t.Errorf("%d of %d concurrent accept attempts failed with ErrInviteInvalid, want exactly %d", got, attempts, attempts-1)
	}

	// Exactly one account exists for the invite's email: the race did
	// not let a second caller slip an account past MarkUsed's guard
	// through some path other than the one just counted above.
	var accountCount int

	if err := store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
		accounts, err := store.Accounts().List(ctx)
		if err != nil {
			return err
		}

		for _, account := range accounts {
			if account.Email == invite.Email {
				accountCount++
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("list accounts: %v", err)
	}

	if accountCount != 1 {
		t.Errorf("%d accounts exist for %q after the race, want exactly 1", accountCount, invite.Email)
	}
}

// TestAcceptInviteOIDCIsSingleUseUnderConcurrentAccept is
// TestAcceptInviteIsSingleUseUnderConcurrentAccept's counterpart for the
// OIDC accept path: the same invite token redeemed by several different
// subjects at once, racing MarkUsed's "WHERE used_at IS NULL" guard
// against a real Postgres. Exactly one subject may win and become bound
// to the account; every other one must see ErrInviteInvalid, never a
// partially created account and never a second account for the same
// email.
func TestAcceptInviteOIDCIsSingleUseUnderConcurrentAccept(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed oidc invite race test")
	}

	ctx := context.Background()

	pool, err := postgres.NewPool(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	store := postgres.NewStore(pool)

	tenant, err := store.Tenants().Create(ctx, storage.Tenant{
		Slug: "oidc-invite-race-" + uuid.NewString(),
		Name: "OIDC Invite Race Tenant",
	})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	manager := auth.NewInviteManager(store, integrationHasher(t), time.Now)

	rawToken, invite, err := manager.CreateInvite(ctx, auth.CreateInviteParams{
		TenantID: tenant.ID,
		Email:    "oidc-racer@invite-race.example",
		Role:     storage.AccountRoleMember,
		TTL:      time.Hour,
	})
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	const attempts = 10

	var (
		wg        sync.WaitGroup
		succeeded atomic.Int32
		failed    atomic.Int32
		start     = make(chan struct{})
	)

	for i := range attempts {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			<-start

			_, err := manager.AcceptInviteOIDC(ctx, auth.AcceptInviteOIDCParams{
				TenantID: tenant.ID,
				RawToken: rawToken,
				Subject:  fmt.Sprintf("f:realm:racing-subject-%d", i),
			})
			if err == nil {
				succeeded.Add(1)

				return
			}
			if !errors.Is(err, auth.ErrInviteInvalid) {
				t.Errorf("attempt %d: unexpected error: %v", i, err)

				return
			}
			failed.Add(1)
		}(i)
	}

	close(start)
	wg.Wait()

	if got := succeeded.Load(); got != 1 {
		t.Errorf("%d of %d concurrent oidc accept attempts succeeded, want exactly 1", got, attempts)
	}
	if got := failed.Load(); got != attempts-1 {
		t.Errorf("%d of %d concurrent oidc accept attempts failed with ErrInviteInvalid, want exactly %d", got, attempts, attempts-1)
	}

	// Exactly one account exists for the invite's email, and it carries
	// exactly one of the racing subjects: the race did not let a second
	// caller slip an account past MarkUsed's guard, and did not leave a
	// row with no subject bound at all.
	var matching []storage.Account

	if err := store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
		accounts, err := store.Accounts().List(ctx)
		if err != nil {
			return err
		}

		for _, account := range accounts {
			if account.Email == invite.Email {
				matching = append(matching, account)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("list accounts: %v", err)
	}

	if len(matching) != 1 {
		t.Fatalf("%d accounts exist for %q after the race, want exactly 1", len(matching), invite.Email)
	}
	if matching[0].OIDCSubject == nil || *matching[0].OIDCSubject == "" {
		t.Error("the one account created by the race has no oidc_subject bound")
	}
}

// integrationHasher has one slot per racer, so every concurrent accept
// reaches MarkUsed instead of a few answering account.ErrHashingBusy
// before the race these tests are about.
func integrationHasher(t *testing.T) *account.Hasher {
	t.Helper()

	hasher, err := account.NewHasher(account.HasherConfig{MaxConcurrentHashes: 16})
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}

	return hasher
}
