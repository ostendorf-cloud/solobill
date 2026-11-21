package storage

import "github.com/google/uuid"

// LoginAccount is the part of an Account a password and TOTP login reads:
// govue's account.Record, in solobill's types. It is read only for an
// active account; a pending or disabled one is not found.
//
// An empty PasswordHash or TOTPSecret means the column is NULL. TOTPCounter
// is accounts.totp_last_counter with a NULL read as 1: an account invited
// or seeded before issue #86 has a confirmed secret, whether or not it has
// logged in since. 0 is an unconfirmed secret.
type LoginAccount struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	TOTPSecret   string
	TOTPCounter  int64
}
