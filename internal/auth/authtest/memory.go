// Package authtest holds in memory implementations of
// storage.LoginAccountRepository and storage.PasskeyRepository for the
// unit tests of internal/auth and internal/httpapi. They sit on top of a
// test's own storage.AccountRepository fake, so an account an invite or a
// fixture created is the account a login finds.
//
// They ignore the tenant, like the fakes they sit on. Tenant isolation and
// the atomicity of the conditional writes are proved against PostgreSQL
// by the integration tests (accounttest.Run and passkeytest.Run in
// internal/storage/postgres). Here one mutex makes every read compare and
// write atomic, which is enough for a single process.
package authtest

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// LoginAccounts is an in memory storage.LoginAccountRepository over
// Accounts. Put writes a whole account back into the test's fake: the
// fake's Update writes only the role, the status and the OIDC subject,
// like the real one, so the credential writes need their own way in.
type LoginAccounts struct {
	Accounts storage.AccountRepository
	Put      func(storage.Account)

	mu sync.Mutex
}

var _ storage.LoginAccountRepository = (*LoginAccounts)(nil)

func loginAccount(a storage.Account) storage.LoginAccount {
	la := storage.LoginAccount{ID: a.ID, Email: a.Email, TOTPCounter: 1}
	if a.PasswordHash != nil {
		la.PasswordHash = *a.PasswordHash
	}
	if a.TOTPSecret != nil {
		la.TOTPSecret = *a.TOTPSecret
	}
	if a.TOTPLastCounter != nil {
		la.TOTPCounter = *a.TOTPLastCounter
	}

	return la
}

// FindActiveByEmail implements storage.LoginAccountRepository.
func (r *LoginAccounts) FindActiveByEmail(ctx context.Context, email string) (storage.LoginAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	a, err := r.Accounts.FindByEmail(ctx, email)
	if err != nil {
		return storage.LoginAccount{}, err
	}
	if a.Status != storage.AccountStatusActive || !strings.EqualFold(a.Email, email) {
		return storage.LoginAccount{}, storage.ErrNotFound
	}

	return loginAccount(a), nil
}

// CreateMember implements storage.LoginAccountRepository.
func (r *LoginAccounts) CreateMember(ctx context.Context, email, passwordHash, totpSecret string) (storage.LoginAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	zero := int64(0)
	a, err := r.Accounts.Create(ctx, storage.Account{
		Email:           email,
		PasswordHash:    nonEmpty(passwordHash),
		TOTPSecret:      nonEmpty(totpSecret),
		TOTPLastCounter: &zero,
		Role:            storage.AccountRoleMember,
		Status:          storage.AccountStatusActive,
	})
	if err != nil {
		return storage.LoginAccount{}, err
	}

	return loginAccount(a), nil
}

// AdvanceTOTPCounter implements storage.LoginAccountRepository.
func (r *LoginAccounts) AdvanceTOTPCounter(ctx context.Context, id uuid.UUID, counter int64) error {
	return r.update(ctx, id, func(a *storage.Account) bool {
		stored := loginAccount(*a).TOTPCounter
		if stored <= 0 || stored >= counter {
			return false
		}
		a.TOTPLastCounter = &counter

		return true
	})
}

// ConfirmTOTP implements storage.LoginAccountRepository.
func (r *LoginAccounts) ConfirmTOTP(ctx context.Context, id uuid.UUID, secret string, counter int64) error {
	return r.update(ctx, id, func(a *storage.Account) bool {
		if a.TOTPSecret == nil || *a.TOTPSecret != secret || secret == "" ||
			a.TOTPLastCounter == nil || *a.TOTPLastCounter != 0 || counter <= 0 {
			return false
		}
		a.TOTPLastCounter = &counter

		return true
	})
}

// ReplaceTOTPSecret implements storage.LoginAccountRepository.
func (r *LoginAccounts) ReplaceTOTPSecret(ctx context.Context, id uuid.UUID, secret string) error {
	return r.update(ctx, id, func(a *storage.Account) bool {
		if a.TOTPLastCounter == nil || *a.TOTPLastCounter != 0 {
			return false
		}
		a.TOTPSecret = nonEmpty(secret)

		return true
	})
}

// SetPasswordHash implements storage.LoginAccountRepository.
func (r *LoginAccounts) SetPasswordHash(ctx context.Context, id uuid.UUID, oldHash, newHash string) error {
	return r.update(ctx, id, func(a *storage.Account) bool {
		if a.PasswordHash == nil || *a.PasswordHash == "" || *a.PasswordHash != oldHash {
			return false
		}
		a.PasswordHash = nonEmpty(newHash)

		return true
	})
}

// update applies change under the mutex and writes the account back when
// change reports it changed something. No change is storage.ErrNotFound.
func (r *LoginAccounts) update(ctx context.Context, id uuid.UUID, change func(a *storage.Account) bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	a, err := r.Accounts.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if !change(&a) {
		return storage.ErrNotFound
	}

	r.Put(a)

	return nil
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

// Passkeys is an in memory storage.PasskeyRepository. It reads the status
// and the email of an account from Accounts.
type Passkeys struct {
	Accounts storage.AccountRepository

	mu      sync.Mutex
	creds   []storage.PasskeyCredential
	handles map[uuid.UUID][]byte
}

var _ storage.PasskeyRepository = (*Passkeys)(nil)

// Add implements storage.PasskeyRepository.
func (r *Passkeys) Add(ctx context.Context, cred storage.PasskeyCredential) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := r.Accounts.FindByID(ctx, cred.AccountID); err != nil {
		return err
	}
	if r.handles == nil {
		r.handles = map[uuid.UUID][]byte{}
	}

	for _, c := range r.creds {
		if bytes.Equal(c.ID, cred.ID) {
			return storage.ErrConflict
		}
	}

	if handle, ok := r.handles[cred.AccountID]; ok {
		if !bytes.Equal(handle, cred.UserHandle) {
			return storage.ErrConflict
		}
	} else {
		for _, h := range r.handles {
			if bytes.Equal(h, cred.UserHandle) {
				return storage.ErrConflict
			}
		}
		r.handles[cred.AccountID] = bytes.Clone(cred.UserHandle)
	}

	r.creds = append(r.creds, clone(cred))

	return nil
}

// List implements storage.PasskeyRepository.
func (r *Passkeys) List(_ context.Context, accountID uuid.UUID) ([]storage.PasskeyCredential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.list(accountID), nil
}

func (r *Passkeys) list(accountID uuid.UUID) []storage.PasskeyCredential {
	out := []storage.PasskeyCredential{}
	for _, c := range r.creds {
		if c.AccountID == accountID {
			c = clone(c)
			c.UserHandle = bytes.Clone(r.handles[accountID])
			out = append(out, c)
		}
	}

	return out
}

// FindByUserHandle implements storage.PasskeyRepository.
func (r *Passkeys) FindByUserHandle(ctx context.Context, handle []byte) (uuid.UUID, string, []storage.PasskeyCredential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for accountID, h := range r.handles {
		if !bytes.Equal(h, handle) {
			continue
		}

		a, err := r.Accounts.FindByID(ctx, accountID)
		if err != nil {
			return uuid.Nil, "", nil, err
		}
		if a.Status != storage.AccountStatusActive {
			return uuid.Nil, "", nil, storage.ErrNotFound
		}

		return accountID, a.Email, r.list(accountID), nil
	}

	return uuid.Nil, "", nil, storage.ErrNotFound
}

// Update implements storage.PasskeyRepository.
func (r *Passkeys) Update(_ context.Context, cred storage.PasskeyCredential, oldSignCount uint32) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, c := range r.creds {
		if bytes.Equal(c.ID, cred.ID) && c.AccountID == cred.AccountID && c.SignCount == oldSignCount {
			r.creds[i].SignCount = cred.SignCount
			r.creds[i].LastUsedAt = cred.LastUsedAt
			r.creds[i].Data = bytes.Clone(cred.Data)

			return nil
		}
	}

	return storage.ErrNotFound
}

// Delete implements storage.PasskeyRepository.
func (r *Passkeys) Delete(_ context.Context, accountID uuid.UUID, id []byte, keepOne bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	count := len(r.list(accountID))
	if keepOne && count < 2 {
		return storage.ErrNotFound
	}

	i := slices.IndexFunc(r.creds, func(c storage.PasskeyCredential) bool {
		return c.AccountID == accountID && bytes.Equal(c.ID, id)
	})
	if i < 0 {
		return storage.ErrNotFound
	}

	r.creds = slices.Delete(r.creds, i, i+1)
	if count == 1 {
		delete(r.handles, accountID)
	}

	return nil
}

func clone(c storage.PasskeyCredential) storage.PasskeyCredential {
	c.ID = bytes.Clone(c.ID)
	c.UserHandle = bytes.Clone(c.UserHandle)
	c.Data = bytes.Clone(c.Data)

	return c
}
