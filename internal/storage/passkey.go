package storage

import (
	"time"

	"github.com/google/uuid"
)

// PasskeyCredential is one registered passkey of a local account: govue's
// passkey.Credential, in solobill's types. UserHandle is the account's one
// random WebAuthn user handle, the same on every credential of the
// account. LastUsedAt is nil before the first login with it.
type PasskeyCredential struct {
	ID         []byte
	AccountID  uuid.UUID
	UserHandle []byte
	Name       string
	SignCount  uint32
	Data       []byte
	CreatedAt  time.Time
	LastUsedAt *time.Time
}
