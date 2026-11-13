package auth

import (
	"encoding/base64"
	"testing"
)

func TestGenerateInviteTokenReturnsTheRawTokenHashedConsistently(t *testing.T) {
	t.Parallel()

	raw, hash, err := GenerateInviteToken()
	if err != nil {
		t.Fatalf("GenerateInviteToken: %v", err)
	}
	if raw == "" {
		t.Fatal("GenerateInviteToken returned an empty raw token")
	}

	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("raw token %q is not valid base64url: %v", raw, err)
	}
	if len(decoded) != inviteTokenBytes {
		t.Errorf("raw token decodes to %d bytes, want %d", len(decoded), inviteTokenBytes)
	}

	if got := HashInviteToken(raw); got != hash {
		t.Errorf("HashInviteToken(raw) = %q, want the hash GenerateInviteToken already returned, %q", got, hash)
	}
}

func TestGenerateInviteTokenProducesIndependentTokens(t *testing.T) {
	t.Parallel()

	first, firstHash, err := GenerateInviteToken()
	if err != nil {
		t.Fatalf("GenerateInviteToken: %v", err)
	}

	second, secondHash, err := GenerateInviteToken()
	if err != nil {
		t.Fatalf("GenerateInviteToken: %v", err)
	}

	if first == second {
		t.Error("two generated invite tokens are identical, want independently random tokens")
	}
	if firstHash == secondHash {
		t.Error("two different tokens hashed to the same value")
	}
}

func TestHashInviteTokenIsDeterministic(t *testing.T) {
	t.Parallel()

	raw, _, err := GenerateInviteToken()
	if err != nil {
		t.Fatalf("GenerateInviteToken: %v", err)
	}

	first := HashInviteToken(raw)
	second := HashInviteToken(raw)
	if first != second {
		t.Errorf("HashInviteToken(raw) is not deterministic: %q != %q", first, second)
	}
}

func TestHashInviteTokenDiffersForDifferentTokens(t *testing.T) {
	t.Parallel()

	if HashInviteToken("token-a") == HashInviteToken("token-b") {
		t.Error("HashInviteToken produced the same hash for two different tokens")
	}
}
