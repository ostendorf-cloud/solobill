package authtest

import (
	"context"

	"github.com/ostendorf-cloud/govue/account"
	"github.com/ostendorf-cloud/govue/passkey"
)

// ScopedAccountStore puts a tenant scope on every call's context before it
// reaches Store, the job internal/auth.TenantFromPath does for a request.
// govue's conformance suite calls the store with a bare test context, and
// solobill's adapter refuses a context without a tenant.
type ScopedAccountStore struct {
	Store account.Store
	Scope func(ctx context.Context) context.Context
}

var _ account.Store = ScopedAccountStore{}

// FindByLogin implements account.Store.
func (s ScopedAccountStore) FindByLogin(ctx context.Context, login string) (account.Record, error) {
	return s.Store.FindByLogin(s.Scope(ctx), login)
}

// Create implements account.Store.
func (s ScopedAccountStore) Create(ctx context.Context, rec account.Record) (account.Record, error) {
	return s.Store.Create(s.Scope(ctx), rec)
}

// AdvanceTOTPCounter implements account.Store.
func (s ScopedAccountStore) AdvanceTOTPCounter(ctx context.Context, id string, counter int64) error {
	return s.Store.AdvanceTOTPCounter(s.Scope(ctx), id, counter)
}

// ConfirmTOTP implements account.Store.
func (s ScopedAccountStore) ConfirmTOTP(ctx context.Context, id, secret string, counter int64) error {
	return s.Store.ConfirmTOTP(s.Scope(ctx), id, secret, counter)
}

// ReplaceTOTPSecret implements account.Store.
func (s ScopedAccountStore) ReplaceTOTPSecret(ctx context.Context, id, secret string) error {
	return s.Store.ReplaceTOTPSecret(s.Scope(ctx), id, secret)
}

// SetPasswordHash implements account.Store.
func (s ScopedAccountStore) SetPasswordHash(ctx context.Context, id, oldHash, newHash string) error {
	return s.Store.SetPasswordHash(s.Scope(ctx), id, oldHash, newHash)
}

// ScopedPasskeyStore is ScopedAccountStore for passkey.Store.
type ScopedPasskeyStore struct {
	Store passkey.Store
	Scope func(ctx context.Context) context.Context
}

var _ passkey.Store = ScopedPasskeyStore{}

// AddCredential implements passkey.Store.
func (s ScopedPasskeyStore) AddCredential(ctx context.Context, cred passkey.Credential) error {
	return s.Store.AddCredential(s.Scope(ctx), cred)
}

// Credentials implements passkey.Store.
func (s ScopedPasskeyStore) Credentials(ctx context.Context, accountID string) ([]passkey.Credential, error) {
	return s.Store.Credentials(s.Scope(ctx), accountID)
}

// FindByUserHandle implements passkey.Store.
func (s ScopedPasskeyStore) FindByUserHandle(ctx context.Context, handle []byte) (account.Identity, []passkey.Credential, error) {
	return s.Store.FindByUserHandle(s.Scope(ctx), handle)
}

// UpdateCredential implements passkey.Store.
func (s ScopedPasskeyStore) UpdateCredential(ctx context.Context, cred passkey.Credential, oldSignCount uint32) error {
	return s.Store.UpdateCredential(s.Scope(ctx), cred, oldSignCount)
}

// DeleteCredential implements passkey.Store.
func (s ScopedPasskeyStore) DeleteCredential(ctx context.Context, accountID string, id []byte, keepOne bool) error {
	return s.Store.DeleteCredential(s.Scope(ctx), accountID, id, keepOne)
}
