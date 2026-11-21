package auth

import (
	"testing"
	"time"

	"github.com/ostendorf-cloud/govue/account"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// The fixtures below were written by solobill's own code before issue #86:
// fixtureLegacyHash by the removed auth.HashPassword (Argon2id, m=65536,
// t=3, p=4, 16 byte salt, 32 byte key, PHC string) for
// fixtureLegacyPassword, fixtureLegacySecret by the removed
// auth.GenerateTOTPSecret (20 random bytes, base32 without padding). They
// are test values for no real account.
const (
	fixtureLegacyPassword = "fixture-password-not-real-1"
	fixtureLegacyHash     = "$argon2id$v=19$m=65536,t=3,p=4$ZPqFcu6OXF0i/8m8HWYpHw$zo1TVmvGRAzt+tZ1mIr4gDCSn1r6p0g2xeIELSOpn44" // gitleaks:allow
	fixtureLegacySecret   = "VOIJ7NFTBBRXI5C6B62OHYB5YFHU4BV3"                                                                  // gitleaks:allow
)

// TestLegacyPasswordHashVerifiesThroughGovue proves an account hashed
// before #86 keeps its password: govue's hasher verifies the stored hash
// as it is, and does not even want to rewrite it, since the parameters are
// today's.
func TestLegacyPasswordHashVerifiesThroughGovue(t *testing.T) {
	t.Parallel()

	check, err := testHasher(t).VerifyPassword(t.Context(), fixtureLegacyHash, fixtureLegacyPassword)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !check.Match {
		t.Fatal("the legacy hash does not verify its own password through govue")
	}
	if check.NeedsRehash {
		t.Error("govue wants to rehash a legacy hash, want the same parameters as today")
	}

	wrong, err := testHasher(t).VerifyPassword(t.Context(), fixtureLegacyHash, "not the password")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if wrong.Match {
		t.Error("the legacy hash verifies a wrong password")
	}
}

// TestLegacyTOTPSecretVerifiesThroughGovue proves a secret enrolled
// before #86 keeps working: govue accepts it and the code an
// authenticator app computes from it.
func TestLegacyTOTPSecretVerifiesThroughGovue(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

	code, err := account.TOTPCode(fixtureLegacySecret, at)
	if err != nil {
		t.Fatalf("TOTPCode with the legacy secret: %v", err)
	}
	if _, ok := account.VerifyTOTPCode(fixtureLegacySecret, code, at); !ok {
		t.Error("govue refuses the code of a legacy secret")
	}
	if _, err := account.TOTPProvisioningURI(TOTPIssuer, "owner@acme.example", fixtureLegacySecret); err != nil {
		t.Errorf("TOTPProvisioningURI with the legacy secret: %v", err)
	}
}

// TestLegacyAccountLogsInWithoutAPasswordReset is the whole migration
// path in one: an account row of the current format, a legacy hash, a
// legacy secret and a NULL totp_last_counter (an account invited or
// seeded before #86 that never logged in since), logs in through govue's
// service over AccountStore. The NULL counter reads as 1, a confirmed
// secret. The integration test TestLegacyAccountLogsInThroughPostgres
// proves the same against the real queries.
func TestLegacyAccountLogsInWithoutAPasswordReset(t *testing.T) {
	t.Parallel()

	f := newLoginFixture(t, storage.AccountStatusActive)

	hash, secret := fixtureLegacyHash, fixtureLegacySecret
	legacy := f.store.addAccount(storage.Account{
		TenantID:     f.tenant.ID,
		Email:        "legacy@acme.example",
		PasswordHash: &hash,
		TOTPSecret:   &secret,
		Role:         storage.AccountRoleMember,
		Status:       storage.AccountStatusActive,
	})

	code, err := account.TOTPCode(fixtureLegacySecret, f.now)
	if err != nil {
		t.Fatalf("TOTPCode: %v", err)
	}

	id, err := f.service.Login(f.ctx(t), "legacy@acme.example", fixtureLegacyPassword, code)
	if err != nil {
		t.Fatalf("Login of a legacy account: %v", err)
	}
	if id.ID != legacy.ID.String() {
		t.Errorf("Identity.ID = %q, want %q", id.ID, legacy.ID)
	}

	stored, err := f.store.Accounts().FindByID(t.Context(), legacy.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if stored.TOTPLastCounter == nil || *stored.TOTPLastCounter <= 1 {
		t.Errorf("TOTPLastCounter = %v after the login, want the consumed code's counter", stored.TOTPLastCounter)
	}
	if stored.PasswordHash == nil || *stored.PasswordHash != fixtureLegacyHash {
		t.Error("the login rewrote a legacy hash that has today's parameters")
	}
}
