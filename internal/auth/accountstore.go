package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/account"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// AccountStore adapts solobill's accounts table to govue's account.Store,
// scoped to the tenant on ctx (see scope.go). The record id is the
// accounts.id UUID as a string, the login is the email, compared case
// insensitively. Only an active account is found, so a pending or disabled
// one cannot log in. accounttest.Run proves it against PostgreSQL in the
// integration job (internal/storage/postgres's TestAccountStoreConformance).
type AccountStore struct {
	store Store
}

var _ account.Store = (*AccountStore)(nil)

// NewAccountStore returns the adapter over store.
func NewAccountStore(store Store) *AccountStore {
	return &AccountStore{store: store}
}

// FindByLogin implements account.Store.
func (s *AccountStore) FindByLogin(ctx context.Context, login string) (account.Record, error) {
	var found storage.LoginAccount

	err := withReadScope(ctx, s.store, func(ctx context.Context) error {
		var err error
		found, err = s.store.LoginAccounts().FindActiveByEmail(ctx, login)

		return err
	})
	if err != nil {
		return account.Record{}, accountError("find account", err)
	}

	return account.Record{
		ID:           found.ID.String(),
		Login:        found.Email,
		PasswordHash: found.PasswordHash,
		TOTPSecret:   found.TOTPSecret,
		TOTPCounter:  found.TOTPCounter,
	}, nil
}

// Create implements account.Store. It creates an active member of the
// scoped tenant. solobill mounts no signup route: accounts arrive through
// invites and the startup seed. Create exists for the contract and its
// conformance suite.
func (s *AccountStore) Create(ctx context.Context, rec account.Record) (account.Record, error) {
	var created storage.LoginAccount

	err := withScope(ctx, s.store, func(ctx context.Context) error {
		var err error
		created, err = s.store.LoginAccounts().CreateMember(ctx, rec.Login, rec.PasswordHash, rec.TOTPSecret)

		return err
	})
	switch {
	case err == nil:
	case errors.Is(err, storage.ErrConflict):
		return account.Record{}, account.ErrConflict
	default:
		// An unknown tenant lands here too: there is nowhere to create
		// the account, which is a failure, not a taken login.
		return account.Record{}, fmt.Errorf("auth: create account: %w", err)
	}

	return account.Record{
		ID:           created.ID.String(),
		Login:        created.Email,
		PasswordHash: created.PasswordHash,
		TOTPSecret:   created.TOTPSecret,
		TOTPCounter:  created.TOTPCounter,
	}, nil
}

// AdvanceTOTPCounter implements account.Store.
func (s *AccountStore) AdvanceTOTPCounter(ctx context.Context, id string, counter int64) error {
	return s.write(ctx, id, "advance totp counter", func(ctx context.Context, repo storage.LoginAccountRepository, accountID uuid.UUID) error {
		return repo.AdvanceTOTPCounter(ctx, accountID, counter)
	})
}

// ConfirmTOTP implements account.Store.
func (s *AccountStore) ConfirmTOTP(ctx context.Context, id, secret string, counter int64) error {
	return s.write(ctx, id, "confirm totp", func(ctx context.Context, repo storage.LoginAccountRepository, accountID uuid.UUID) error {
		return repo.ConfirmTOTP(ctx, accountID, secret, counter)
	})
}

// ReplaceTOTPSecret implements account.Store.
func (s *AccountStore) ReplaceTOTPSecret(ctx context.Context, id, secret string) error {
	return s.write(ctx, id, "replace totp secret", func(ctx context.Context, repo storage.LoginAccountRepository, accountID uuid.UUID) error {
		return repo.ReplaceTOTPSecret(ctx, accountID, secret)
	})
}

// SetPasswordHash implements account.Store.
func (s *AccountStore) SetPasswordHash(ctx context.Context, id, oldHash, newHash string) error {
	return s.write(ctx, id, "set password hash", func(ctx context.Context, repo storage.LoginAccountRepository, accountID uuid.UUID) error {
		return repo.SetPasswordHash(ctx, accountID, oldHash, newHash)
	})
}

// write runs one conditional write. An id that is not a UUID names no
// account, which is a refusal like any other.
func (s *AccountStore) write(
	ctx context.Context,
	id, what string,
	fn func(ctx context.Context, repo storage.LoginAccountRepository, accountID uuid.UUID) error,
) error {
	accountID, err := uuid.Parse(id)
	if err != nil {
		return account.ErrNotFound
	}

	err = withScope(ctx, s.store, func(ctx context.Context) error {
		return fn(ctx, s.store.LoginAccounts(), accountID)
	})

	return accountError(what, err)
}

// accountError maps a storage outcome to the account package's errors:
// not found and an unknown tenant are account.ErrNotFound, anything else
// is a store failure.
func accountError(what string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, storage.ErrNotFound), errors.Is(err, errUnknownTenant):
		return account.ErrNotFound
	default:
		return fmt.Errorf("auth: %s: %w", what, err)
	}
}
