package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// inviteTokenBytes is 256 bits of entropy (32 bytes), drawn from
// crypto/rand. Unlike a password, this is never a human-chosen secret
// from a small practical space; it has to be large enough that guessing
// it is never cheaper than guessing a well chosen symmetric key, and 256
// bits is the conventional floor for that regardless of how fast the
// hash checking a guess is, which is the next comment's point.
const inviteTokenBytes = 32

// GenerateInviteToken returns a fresh raw invite token and the hash
// InviteManager.CreateInvite stores instead of it. raw is handed back to
// the caller exactly once, for the invite email (a later integration, not
// this package's job); it is never logged, stored or returned again.
func GenerateInviteToken() (raw, hash string, err error) {
	buf := make([]byte, inviteTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("auth: read invite token: %w", err)
	}

	raw = base64.RawURLEncoding.EncodeToString(buf)

	return raw, HashInviteToken(raw), nil
}

// HashInviteToken hashes a raw invite token with SHA-256 before it is
// looked up or compared against what InviteRepository stores.
//
// A fast, unsalted hash is the wrong choice for a password: a human
// picks a password from a small, guessable space, and a fast hash lets an
// attacker who steals the hash try that whole space in seconds, which is
// exactly what argon2id (see HashPassword) is deliberately slow to
// prevent. A token generated here carries no such guessable structure:
// it is inviteTokenBytes of uniform randomness from crypto/rand, so an
// attacker gains nothing from a hash being fast, because trying "the
// whole space" means trying 2^256 values whether the hash takes a
// nanosecond or a second. SHA-256 is also deterministic and side effect
// free in a way that makes FindByTokenHash a plain index lookup, which an
// adaptive, salted hash would not allow.
func HashInviteToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))

	return hex.EncodeToString(sum[:])
}
