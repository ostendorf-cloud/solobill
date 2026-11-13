package auth

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestHOTPMatchesRFC6238AppendixB checks this package's HMAC-SHA-1 HOTP
// core (hotp) against RFC 6238 Appendix B's own published SHA-1 test
// vectors, fetched from the RFC text while writing this test rather than
// retyped from memory. The RFC's table uses 8 digit codes and the ASCII
// seed "12345678901234567890"; this package's exported API always uses 6
// digits (see totpDigits), so the vectors are checked against the
// unexported hotp function directly, at the same digit count the RFC
// table uses.
func TestHOTPMatchesRFC6238AppendixB(t *testing.T) {
	t.Parallel()

	const seed = "12345678901234567890" // RFC 6238 Appendix B, SHA-1 mode.

	tests := []struct {
		name     string
		timeUnix int64
		wantCode string
	}{
		{name: "T=1 (59 seconds)", timeUnix: 59, wantCode: "94287082"},
		{name: "T=23523EC (1111111109 seconds)", timeUnix: 1111111109, wantCode: "07081804"},
		{name: "T=23523ED (1111111111 seconds)", timeUnix: 1111111111, wantCode: "14050471"},
		{name: "T=273EF07 (1234567890 seconds)", timeUnix: 1234567890, wantCode: "89005924"},
		{name: "T=3F940AA (2000000000 seconds)", timeUnix: 2000000000, wantCode: "69279037"},
		{name: "T=27BC86AA (20000000000 seconds)", timeUnix: 20000000000, wantCode: "65353130"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			at := time.Unix(tt.timeUnix, 0).UTC()
			got := hotp([]byte(seed), counterAt(at, 0), 8)
			if got != tt.wantCode {
				t.Errorf("hotp(seed, counterAt(%s), 8) = %q, want %q", at, got, tt.wantCode)
			}
		})
	}
}

func TestGenerateTOTPSecretProducesAUsableBase32Secret(t *testing.T) {
	t.Parallel()

	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	if secret == "" {
		t.Fatal("GenerateTOTPSecret returned an empty secret")
	}
	if _, err := base32Encoding.DecodeString(secret); err != nil {
		t.Errorf("GenerateTOTPSecret returned %q, not valid base32: %v", secret, err)
	}

	other, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	if secret == other {
		t.Error("two generated secrets are identical, want independently random secrets")
	}
}

func TestGenerateThenVerifyTOTPCodeRoundTrips(t *testing.T) {
	t.Parallel()

	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}

	at := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

	code, err := GenerateTOTPCode(secret, at)
	if err != nil {
		t.Fatalf("GenerateTOTPCode: %v", err)
	}
	if len(code) != totpDigits {
		t.Errorf("GenerateTOTPCode returned %q, want %d digits", code, totpDigits)
	}

	if !VerifyTOTPCode(secret, code, at) {
		t.Error("VerifyTOTPCode(secret, GenerateTOTPCode(secret, at), at) = false, want true")
	}
}

func TestVerifyTOTPCodeAcceptsAdjacentStepsAndRejectsFurtherDrift(t *testing.T) {
	t.Parallel()

	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}

	issuedAt := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	code, err := GenerateTOTPCode(secret, issuedAt)
	if err != nil {
		t.Fatalf("GenerateTOTPCode: %v", err)
	}

	tests := []struct {
		name string
		at   time.Time
		want bool
	}{
		{name: "same step", at: issuedAt, want: true},
		{name: "one step later, within skew", at: issuedAt.Add(totpStep), want: true},
		{name: "one step earlier, within skew", at: issuedAt.Add(-totpStep), want: true},
		{name: "two steps later, outside skew", at: issuedAt.Add(2 * totpStep), want: false},
		{name: "two steps earlier, outside skew", at: issuedAt.Add(-2 * totpStep), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := VerifyTOTPCode(secret, code, tt.at)
			if got != tt.want {
				t.Errorf("VerifyTOTPCode at %s = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestVerifyTOTPCodeRejectsAWrongCode(t *testing.T) {
	t.Parallel()

	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}

	at := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	code, err := GenerateTOTPCode(secret, at)
	if err != nil {
		t.Fatalf("GenerateTOTPCode: %v", err)
	}

	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}

	if VerifyTOTPCode(secret, wrong, at) {
		t.Error("VerifyTOTPCode with a wrong code = true, want false")
	}
}

func TestVerifyTOTPCodeRejectsAMalformedSecret(t *testing.T) {
	t.Parallel()

	if VerifyTOTPCode("not valid base32!!", "123456", time.Now()) {
		t.Error("VerifyTOTPCode with a malformed secret = true, want false")
	}
}

func TestTOTPProvisioningURI(t *testing.T) {
	t.Parallel()

	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}

	raw := TOTPProvisioningURI(secret, "owner@tenant.example", "solobill")

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("TOTPProvisioningURI returned an unparsable URI %q: %v", raw, err)
	}
	if u.Scheme != "otpauth" {
		t.Errorf("scheme = %q, want %q", u.Scheme, "otpauth")
	}
	if u.Host != "totp" {
		t.Errorf("host = %q, want %q", u.Host, "totp")
	}
	if !strings.Contains(u.Path, "owner@tenant.example") {
		t.Errorf("path %q does not contain the account name", u.Path)
	}
	if got := u.Query().Get("secret"); got != secret {
		t.Errorf("secret query parameter = %q, want %q", got, secret)
	}
	if got := u.Query().Get("issuer"); got != "solobill" {
		t.Errorf("issuer query parameter = %q, want %q", got, "solobill")
	}
}
