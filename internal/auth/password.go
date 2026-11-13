package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. golang.org/x/crypto/argon2's own package doc,
// citing RFC 9106 Section 4, gives two recommended options: time=1,
// memory=2 GiB, threads=4 when that much memory can be spared per login
// attempt, or time=3, memory=64 MiB, threads=4 "if much less memory is
// available". solobill runs as a small, single-replica service rather
// than on hardware provisioned for a password hasher alone, so the
// second option is the one that fits: enough cost to make an offline
// attempt expensive, without a login spiking the process to 2 GiB of RSS
// per concurrent attempt.
const (
	argon2Time      = 3
	argon2MemoryKiB = 64 * 1024
	argon2Threads   = 4
	// argon2KeyLen is the derived key (hash) length. RFC 9106 Section 4
	// calls 128 bits "sufficient for most applications"; 32 bytes (256
	// bits) is the extra margin most argon2id implementations default to
	// regardless, at a cost that does not show up next to the memory
	// cost above.
	argon2KeyLen = 32
	// argon2SaltLen is 128 bits, RFC 9106 Section 4's own recommendation
	// ("sufficient for all applications").
	argon2SaltLen = 16
)

// ErrPasswordHashInvalid is returned by VerifyPassword when the stored
// value is not a hash this package wrote: the "$argon2id$v=...$m=...$...$..."
// encoding does not parse. It is an operational signal (a corrupted
// column, a hash from something else entirely), not a login outcome: see
// LocalAuthenticator.Login, which never lets this surface differently
// from an ordinary wrong password.
var ErrPasswordHashInvalid = errors.New("auth: password hash is not a recognized argon2id encoding")

// HashPassword derives an Argon2id hash of password with a fresh random
// salt, and encodes the algorithm, its parameters, the salt and the hash
// into one self-describing string: "$argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>",
// the same shape most argon2id libraries use (the PHC string format).
// Encoding the parameters alongside the hash, rather than assuming
// whatever argon2Time/argon2MemoryKiB/argon2Threads currently are, is what
// lets VerifyPassword still check an old hash correctly after those
// constants change: see VerifyPassword.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read password salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argon2Time, argon2MemoryKiB, argon2Threads, argon2KeyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argon2MemoryKiB, argon2Time, argon2Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// argon2idParams is what HashPassword's encoding carries about how a
// particular hash was computed, read back by VerifyPassword rather than
// assumed from the current argon2Time/argon2MemoryKiB/argon2Threads
// constants.
type argon2idParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

// VerifyPassword reports whether password hashes to encoded, recomputing
// the hash with the parameters, salt and output length encoded's own
// string carries rather than today's constants, so a hash written under
// an older parameter choice still verifies correctly.
//
// The comparison is constant time (crypto/subtle), and the two outcomes
// this function can report, a parse failure and a hash mismatch, are
// deliberately distinguished by return value only for an operator's log,
// never for a caller to answer a login differently: LocalAuthenticator.Login
// collapses both, and "no such account" besides, into one identical
// failure before anything reaches outside this package.
func VerifyPassword(encoded, password string) (bool, error) {
	params, salt, hash, err := decodeArgon2id(encoded)
	if err != nil {
		return false, err
	}

	candidate := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(hash)))

	return subtle.ConstantTimeCompare(candidate, hash) == 1, nil
}

// decodeArgon2id parses the string HashPassword produces. Every failure,
// whatever the reason, is ErrPasswordHashInvalid: the detail belongs in
// whatever logged the operational problem, not in a typed error a caller
// might branch on.
func decodeArgon2id(encoded string) (argon2idParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return argon2idParams{}, nil, nil, ErrPasswordHashInvalid
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return argon2idParams{}, nil, nil, ErrPasswordHashInvalid
	}

	var params argon2idParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.memory, &params.time, &params.threads); err != nil {
		return argon2idParams{}, nil, nil, ErrPasswordHashInvalid
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argon2idParams{}, nil, nil, ErrPasswordHashInvalid
	}

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return argon2idParams{}, nil, nil, ErrPasswordHashInvalid
	}

	return params, salt, hash, nil
}
