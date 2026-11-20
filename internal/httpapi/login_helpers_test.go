package httpapi_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/ostendorf-cloud/govue/account"
	"github.com/ostendorf-cloud/govue/session"

	"github.com/ostendorf-cloud/solobill/internal/auth"
)

// testHasher is a govue hasher with enough slots that the parallel
// subtests of one app do not answer each other with 503.
func testHasher(t *testing.T) *account.Hasher {
	t.Helper()

	h, err := account.NewHasher(account.HasherConfig{MaxConcurrentHashes: 8})
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}

	return h
}

// newTestLogin builds the govue login over store and sessions, the way
// cmd/solobill does, without passkeys.
func newTestLogin(t *testing.T, store auth.Store, sessions *session.Manager) *auth.Login {
	t.Helper()

	login, err := auth.NewLogin(auth.LoginConfig{
		Store:    store,
		Sessions: sessions,
		Hasher:   testHasher(t),
		Logger:   slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("auth.NewLogin: %v", err)
	}

	return login
}

// newTestInvites is the invite manager over store with its own hasher.
func newTestInvites(t *testing.T, store auth.Store) *auth.InviteManager {
	t.Helper()

	return auth.NewInviteManager(store, testHasher(t), nil)
}

// hashPassword, totpSecret and totpCode are govue's, the ones the login
// verifies against.
func hashPassword(t *testing.T, password string) string {
	t.Helper()

	hash, err := testHasher(t).HashPassword(t.Context(), password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	return hash
}

func totpSecret(t *testing.T) string {
	t.Helper()

	secret, err := account.GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}

	return secret
}

func totpCode(t *testing.T, secret string) string {
	t.Helper()

	code, err := account.TOTPCode(secret, time.Now())
	if err != nil {
		t.Fatalf("TOTPCode: %v", err)
	}

	return code
}

// loginPath is the password login route of a tenant.
func loginPath(tenantSlug string) string {
	return "/api/v1/tenants/" + tenantSlug + "/auth/login"
}

// loginBody is govue's account.CredentialsRequest.
func loginBody(email, password, code string) map[string]string {
	return map[string]string{"login": email, "password": password, "code": code}
}
