//go:build integration

// govue's conformance suites against solobill's real PostgreSQL adapters
// (issue #86), run through the restricted NOSUPERUSER NOBYPASSRLS test
// role of TestRowLevelSecurity, so row level security applies to every
// statement the adapters make. Like the rest of this package's
// integration tests they need DATABASE_URL and run in the integration job.
package postgres_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ostendorf-cloud/govue/account"
	"github.com/ostendorf-cloud/govue/account/accounttest"
	"github.com/ostendorf-cloud/govue/passkey"
	"github.com/ostendorf-cloud/govue/passkey/passkeytest"

	"github.com/ostendorf-cloud/solobill/internal/auth"
	"github.com/ostendorf-cloud/solobill/internal/auth/authtest"
	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres"
)

// loginTestStore opens the restricted store and creates a fresh tenant.
func loginTestStore(t *testing.T) (*postgres.Store, *pgxpool.Pool, storage.Tenant) {
	t.Helper()

	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed login adapter tests")
	}

	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	restrictedPool, err := postgres.NewPool(ctx, setUpTestAppRole(ctx, t, adminPool, adminURL))
	if err != nil {
		t.Fatalf("open restricted pool as %s: %v", testAppRole, err)
	}
	t.Cleanup(restrictedPool.Close)

	store := postgres.NewStore(restrictedPool)

	tenant, err := store.Tenants().Create(ctx, storage.Tenant{Slug: "login-" + uuid.NewString(), Name: "Login Tenant"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	return store, adminPool, tenant
}

func scopeTo(tenantID uuid.UUID) func(ctx context.Context) context.Context {
	return func(ctx context.Context) context.Context { return auth.ContextWithTenant(ctx, tenantID) }
}

// setStatus moves an account to status, the way an owner disables one.
func setStatus(t *testing.T, store *postgres.Store, tenantID uuid.UUID, id string, status storage.AccountStatus) {
	t.Helper()

	accountID, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("parse account id %q: %v", id, err)
	}

	if err := store.WithTenant(context.Background(), tenantID, func(ctx context.Context) error {
		a, err := store.Accounts().FindByID(ctx, accountID)
		if err != nil {
			return err
		}
		a.Status = status
		_, err = store.Accounts().Update(ctx, a)

		return err
	}); err != nil {
		t.Fatalf("set status %s: %v", status, err)
	}
}

// TestAccountStoreConformance runs govue's accounttest suite against
// auth.AccountStore over PostgreSQL. Rounds is lowered from govue's 1000:
// every guard of the adapter is one conditional statement
// (queries/login_accounts.sql), which is what proves it atomic, and the
// suite here is the safety net govue describes.
func TestAccountStoreConformance(t *testing.T) {
	store, _, tenant := loginTestStore(t)

	accounttest.Run(t, accounttest.Harness{
		NewStore: func(*testing.T) account.Store {
			return authtest.ScopedAccountStore{Store: auth.NewAccountStore(store), Scope: scopeTo(tenant.ID)}
		},
		FoldsCase: true,
		Disable: func(t *testing.T, _ account.Store, id string) {
			setStatus(t, store, tenant.ID, id, storage.AccountStatusDisabled)
		},
		Rounds: 200,
	})
}

// TestPasskeyStoreConformance runs govue's passkeytest suite against
// auth.PasskeyStore over PostgreSQL.
func TestPasskeyStoreConformance(t *testing.T) {
	store, _, tenant := loginTestStore(t)

	passkeytest.Run(t, passkeytest.Harness{
		NewStore: func(*testing.T) passkey.Store {
			return authtest.ScopedPasskeyStore{Store: auth.NewPasskeyStore(store), Scope: scopeTo(tenant.ID)}
		},
		NewAccount: func(t *testing.T, _ passkey.Store) account.Identity {
			email := "passkey-" + uuid.NewString() + "@login.example"

			var created storage.Account
			if err := store.WithTenant(context.Background(), tenant.ID, func(ctx context.Context) error {
				var err error
				created, err = store.Accounts().Create(ctx, storage.Account{
					TenantID: tenant.ID,
					Email:    email,
					Role:     storage.AccountRoleMember,
					Status:   storage.AccountStatusActive,
				})

				return err
			}); err != nil {
				t.Fatalf("create account: %v", err)
			}

			return account.Identity{ID: created.ID.String(), Login: email}
		},
		Disable: func(t *testing.T, _ passkey.Store, id string) {
			setStatus(t, store, tenant.ID, id, storage.AccountStatusDisabled)
		},
	})
}

// TestLoginAdaptersStayInTheirTenant: an account and a passkey of tenant
// A are invisible through the adapters scoped to tenant B, and B's writes
// cannot reach them, enforced by row level security below the queries.
func TestLoginAdaptersStayInTheirTenant(t *testing.T) {
	store, _, tenantA := loginTestStore(t)
	ctx := context.Background()

	tenantB, err := store.Tenants().Create(ctx, storage.Tenant{Slug: "login-b-" + uuid.NewString(), Name: "Login Tenant B"})
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	inA := authtest.ScopedAccountStore{Store: auth.NewAccountStore(store), Scope: scopeTo(tenantA.ID)}
	inB := authtest.ScopedAccountStore{Store: auth.NewAccountStore(store), Scope: scopeTo(tenantB.ID)}

	const hash = "$argon2id$v=19$m=65536,t=3,p=4$ZmFrZS1zYWx0$ZmFrZS1oYXNoLW5vdC1hLXJlYWwtb25l" // gitleaks:allow
	const secret = "FAKESECRETFAKESECRETFAKESECRETFA"                                           // gitleaks:allow

	created, err := inA.Create(ctx, account.Record{Login: "shared@login.example", PasswordHash: hash, TOTPSecret: secret})
	if err != nil {
		t.Fatalf("Create in A: %v", err)
	}

	if _, err := inB.FindByLogin(ctx, "shared@login.example"); !errors.Is(err, account.ErrNotFound) {
		t.Errorf("FindByLogin in B of A's login = %v, want account.ErrNotFound", err)
	}
	if err := inB.ConfirmTOTP(ctx, created.ID, secret, 100); !errors.Is(err, account.ErrNotFound) {
		t.Errorf("ConfirmTOTP in B of A's account = %v, want account.ErrNotFound", err)
	}
	if err := inB.SetPasswordHash(ctx, created.ID, hash, hash+"x"); !errors.Is(err, account.ErrNotFound) {
		t.Errorf("SetPasswordHash in B of A's account = %v, want account.ErrNotFound", err)
	}

	// The same email is a separate account in B: uniqueness is per tenant.
	if _, err := inB.Create(ctx, account.Record{Login: "shared@login.example"}); err != nil {
		t.Errorf("Create in B with A's email: %v, want a separate account", err)
	}

	got, err := inA.FindByLogin(ctx, "shared@login.example")
	if err != nil {
		t.Fatalf("FindByLogin in A: %v", err)
	}
	if got.PasswordHash != hash || got.TOTPCounter != 0 {
		t.Errorf("A's account after B's writes = %+v, want it unchanged", got)
	}

	// A's secret is unconfirmed: B must not replace it.
	if err := inB.ReplaceTOTPSecret(ctx, created.ID, "OTHERSECRETOTHERSECRETOTHERSECRE"); !errors.Is(err, account.ErrNotFound) {
		t.Errorf("ReplaceTOTPSecret in B of A's account = %v, want account.ErrNotFound", err)
	}
	if err := inA.ConfirmTOTP(ctx, created.ID, secret, 100); err != nil {
		t.Fatalf("ConfirmTOTP in A: %v", err)
	}
	// Confirmed now: B must not advance its counter.
	if err := inB.AdvanceTOTPCounter(ctx, created.ID, 200); !errors.Is(err, account.ErrNotFound) {
		t.Errorf("AdvanceTOTPCounter in B of A's account = %v, want account.ErrNotFound", err)
	}
	if got, err := inA.FindByLogin(ctx, "shared@login.example"); err != nil || got.TOTPSecret != secret || got.TOTPCounter != 100 {
		t.Errorf("A's account after B's TOTP writes = %+v, %v, want secret and counter 100 unchanged", got, err)
	}

	passkeysA := authtest.ScopedPasskeyStore{Store: auth.NewPasskeyStore(store), Scope: scopeTo(tenantA.ID)}
	passkeysB := authtest.ScopedPasskeyStore{Store: auth.NewPasskeyStore(store), Scope: scopeTo(tenantB.ID)}

	handle := []byte("tenant-a-handle-" + uuid.NewString())
	credA := passkey.Credential{
		ID: []byte("tenant-a-credential-" + uuid.NewString()), AccountID: created.ID, UserHandle: handle,
		Data: []byte(`{"fake":true}`), CreatedAt: time.Now(),
	}
	if err := passkeysA.AddCredential(ctx, credA); err != nil {
		t.Fatalf("AddCredential in A: %v", err)
	}

	moved := credA
	moved.SignCount = 9
	if err := passkeysB.UpdateCredential(ctx, moved, 0); !errors.Is(err, passkey.ErrNotFound) {
		t.Errorf("UpdateCredential in B of A's passkey = %v, want passkey.ErrNotFound", err)
	}
	if err := passkeysB.DeleteCredential(ctx, created.ID, credA.ID, false); !errors.Is(err, passkey.ErrNotFound) {
		t.Errorf("DeleteCredential in B of A's passkey = %v, want passkey.ErrNotFound", err)
	}
	if creds, err := passkeysA.Credentials(ctx, created.ID); err != nil || len(creds) != 1 || creds[0].SignCount != 0 {
		t.Errorf("A's passkeys after B's writes = %+v, %v, want the one passkey unchanged", creds, err)
	}

	if _, _, err := passkeysB.FindByUserHandle(ctx, handle); !errors.Is(err, passkey.ErrNotFound) {
		t.Errorf("FindByUserHandle in B of A's handle = %v, want passkey.ErrNotFound", err)
	}
	if creds, err := passkeysB.Credentials(ctx, created.ID); err != nil || len(creds) != 0 {
		t.Errorf("Credentials in B of A's account = %d, %v, want none", len(creds), err)
	}
	if err := passkeysB.AddCredential(ctx, passkey.Credential{
		ID: []byte("tenant-b-credential-" + uuid.NewString()), AccountID: created.ID, UserHandle: handle,
		Data: []byte(`{"fake":true}`), CreatedAt: time.Now(),
	}); !errors.Is(err, passkey.ErrNotFound) {
		t.Errorf("AddCredential in B for A's account = %v, want passkey.ErrNotFound", err)
	}
}

// TestAccountUpdateNeverMovesTheCredentialsBack: AccountRepository.Update
// from a row read before a login consumed a code must not put the old
// counter back (which would let the consumed code log in again, #50), nor
// an old password hash. It writes the role, the status and the OIDC
// subject only.
func TestAccountUpdateNeverMovesTheCredentialsBack(t *testing.T) {
	store, _, tenant := loginTestStore(t)
	ctx := context.Background()

	scoped := authtest.ScopedAccountStore{Store: auth.NewAccountStore(store), Scope: scopeTo(tenant.ID)}

	const hash = "$argon2id$v=19$m=65536,t=3,p=4$ZmFrZS1zYWx0$ZmFrZS1oYXNoLW5vdC1hLXJlYWwtb25l" // gitleaks:allow
	const secret = "FAKESECRETFAKESECRETFAKESECRETFA"                                           // gitleaks:allow

	created, err := scoped.Create(ctx, account.Record{Login: "stale@login.example", PasswordHash: hash, TOTPSecret: secret})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := scoped.ConfirmTOTP(ctx, created.ID, secret, 100); err != nil {
		t.Fatalf("ConfirmTOTP: %v", err)
	}

	accountID := uuid.MustParse(created.ID)

	var stale storage.Account
	if err := store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
		stale, err = store.Accounts().FindByID(ctx, accountID)

		return err
	}); err != nil {
		t.Fatalf("FindByID: %v", err)
	}

	// A login consumes a code after the stale read.
	if err := scoped.AdvanceTOTPCounter(ctx, created.ID, 150); err != nil {
		t.Fatalf("AdvanceTOTPCounter: %v", err)
	}

	otherHash := hash + "-stale"
	stale.PasswordHash = &otherHash
	stale.Role = storage.AccountRoleOwner
	if err := store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
		_, err := store.Accounts().Update(ctx, stale)

		return err
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := scoped.FindByLogin(ctx, "stale@login.example")
	if err != nil {
		t.Fatalf("FindByLogin: %v", err)
	}
	if got.TOTPCounter != 150 || got.PasswordHash != hash {
		t.Errorf("after Update from a stale read = %+v, want counter 150 and the hash unchanged", got)
	}
	if err := scoped.AdvanceTOTPCounter(ctx, created.ID, 150); !errors.Is(err, account.ErrNotFound) {
		t.Errorf("the consumed counter advanced again after Update = %v, want account.ErrNotFound", err)
	}

	var role string
	if err := store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
		a, err := store.Accounts().FindByID(ctx, accountID)
		role = string(a.Role)

		return err
	}); err != nil || role != string(storage.AccountRoleOwner) {
		t.Errorf("role after Update = %q, %v, want owner", role, err)
	}
}

// Fixtures written by solobill's own code before issue #86: the hash by
// the removed auth.HashPassword for legacyPassword, the secret by the
// removed auth.GenerateTOTPSecret. Test values for no real account.
const (
	legacyPassword = "fixture-password-not-real-1"
	legacyHash     = "$argon2id$v=19$m=65536,t=3,p=4$ZPqFcu6OXF0i/8m8HWYpHw$zo1TVmvGRAzt+tZ1mIr4gDCSn1r6p0g2xeIELSOpn44" // gitleaks:allow
	legacySecret   = "VOIJ7NFTBBRXI5C6B62OHYB5YFHU4BV3"                                                                  // gitleaks:allow
)

// TestLegacyAccountLogsInThroughPostgres is the migration path against
// the real queries: an accounts row as invites and the seed wrote it
// before #86, with a legacy hash, a legacy secret and a NULL
// totp_last_counter, inserted by plain SQL, logs in through govue's
// service without a password reset, and its code is then consumed.
func TestLegacyAccountLogsInThroughPostgres(t *testing.T) {
	store, adminPool, tenant := loginTestStore(t)
	ctx := context.Background()

	var accountID uuid.UUID
	if err := adminPool.QueryRow(ctx, `
INSERT INTO accounts (tenant_id, email, password_hash, totp_secret, totp_last_counter, role, status)
VALUES ($1, 'legacy@login.example', $2, $3, NULL, 'member', 'active')
RETURNING id`, tenant.ID, legacyHash, legacySecret).Scan(&accountID); err != nil {
		t.Fatalf("insert legacy account: %v", err)
	}

	hasher, err := account.NewHasher(account.HasherConfig{})
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}

	service, err := account.NewService(account.ServiceConfig{
		Store:       auth.NewAccountStore(store),
		Hasher:      hasher,
		Issuer:      auth.TOTPIssuer,
		EndSessions: func(context.Context, string) error { return nil },
		Logger:      slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	code, err := account.TOTPCode(legacySecret, time.Now())
	if err != nil {
		t.Fatalf("TOTPCode: %v", err)
	}

	scoped := auth.ContextWithTenant(ctx, tenant.ID)

	id, err := service.Login(scoped, "legacy@login.example", legacyPassword, code)
	if err != nil {
		t.Fatalf("Login of a legacy account: %v", err)
	}
	if id.ID != accountID.String() {
		t.Errorf("Identity.ID = %q, want %q", id.ID, accountID)
	}

	if _, err := service.Login(scoped, "legacy@login.example", legacyPassword, code); !errors.Is(err, account.ErrInvalidCredentials) {
		t.Errorf("second Login with the same code = %v, want account.ErrInvalidCredentials", err)
	}

	var (
		counter *int64
		hash    string
	)
	if err := adminPool.QueryRow(ctx, `SELECT totp_last_counter, password_hash FROM accounts WHERE id = $1`, accountID).Scan(&counter, &hash); err != nil {
		t.Fatalf("read the legacy account back: %v", err)
	}
	if counter == nil || *counter <= 1 {
		t.Errorf("totp_last_counter = %v, want the consumed code's counter", counter)
	}
	if hash != legacyHash {
		t.Error("the login rewrote a legacy hash that already has today's parameters")
	}
}
