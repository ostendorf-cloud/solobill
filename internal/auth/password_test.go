package auth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestHashPasswordAndVerifyPasswordRoundTrip(t *testing.T) {
	t.Parallel()

	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	ok, err := VerifyPassword(hash, "correct horse battery staple")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Error("VerifyPassword with the correct password = false, want true")
	}
}

func TestVerifyPasswordRejectsAWrongPassword(t *testing.T) {
	t.Parallel()

	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	ok, err := VerifyPassword(hash, "a different password entirely")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if ok {
		t.Error("VerifyPassword with a wrong password = true, want false")
	}
}

func TestHashPasswordUsesAFreshSaltEveryTime(t *testing.T) {
	t.Parallel()

	first, err := HashPassword("the same password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	second, err := HashPassword("the same password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if first == second {
		t.Error("two hashes of the same password are identical, want different salts to make them differ")
	}
}

func TestVerifyPasswordHonorsTheParametersEncodedInAnOlderHash(t *testing.T) {
	t.Parallel()

	// Built by hand with parameters that do not match this package's
	// current argon2Time/argon2MemoryKiB/argon2Threads constants, the way
	// a hash written under an earlier parameter choice would look.
	// VerifyPassword has to read these back out of the string, not
	// assume today's constants, or an operator could never change them
	// without breaking every existing password.
	const (
		oldTime    = 1
		oldMemory  = 8 * 1024
		oldThreads = 1
		oldKeyLen  = 32
	)
	salt := []byte("0123456789abcdef")
	password := "a password hashed under old parameters"
	hash := argon2.IDKey([]byte(password), salt, oldTime, oldMemory, oldThreads, oldKeyLen)

	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, oldMemory, oldTime, oldThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	)

	ok, err := VerifyPassword(encoded, password)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Error("VerifyPassword against a hash with older parameters = false, want true")
	}
}

func TestVerifyPasswordRejectsAMalformedHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		encoded string
	}{
		{name: "empty string", encoded: ""},
		{name: "not an argon2id string at all", encoded: "not a hash"},
		{name: "wrong algorithm tag", encoded: "$argon2i$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA"},
		{name: "wrong version", encoded: "$argon2id$v=1$m=65536,t=3,p=4$c2FsdA$aGFzaA"},
		{name: "unparsable parameters", encoded: "$argon2id$v=19$not-params$c2FsdA$aGFzaA"},
		{name: "salt is not base64", encoded: "$argon2id$v=19$m=65536,t=3,p=4$not base64!$aGFzaA"},
		{name: "hash is not base64", encoded: "$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$not base64!"},
		{name: "missing a field", encoded: "$argon2id$v=19$m=65536,t=3,p=4$c2FsdA"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ok, err := VerifyPassword(tt.encoded, "whatever password")
			if !errors.Is(err, ErrPasswordHashInvalid) {
				t.Errorf("err = %v, want errors.Is(err, ErrPasswordHashInvalid)", err)
			}
			if ok {
				t.Error("VerifyPassword against a malformed hash = true, want false")
			}
		})
	}
}
