package auth

import (
	"testing"
	"time"

	"github.com/ostendorf-cloud/govue/account"
)

// testHasher is a govue hasher with enough slots that parallel tests do
// not wait on each other's derivations. Production runs govue's default
// of one slot.
func testHasher(t *testing.T) *account.Hasher {
	t.Helper()

	h, err := account.NewHasher(account.HasherConfig{MaxConcurrentHashes: 8})
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}

	return h
}

// testTOTPSecret and testTOTPCode are govue's TOTP functions, the ones
// the login and the invite accept verify against.
func testTOTPSecret() (string, error) { return account.GenerateTOTPSecret() }

func testTOTPCode(secret string, at time.Time) (string, error) { return account.TOTPCode(secret, at) }
