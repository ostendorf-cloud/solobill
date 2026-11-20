package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/account"
	"github.com/ostendorf-cloud/govue/passkey"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// PasskeyStore adapts solobill's passkey tables to govue's passkey.Store,
// scoped to the tenant on ctx (see scope.go), like AccountStore. A passkey
// login therefore finds an account only in the tenant its login page
// named. passkeytest.Run proves it against PostgreSQL in the integration
// job (internal/storage/postgres's TestPasskeyStoreConformance).
type PasskeyStore struct {
	store Store
}

var _ passkey.Store = (*PasskeyStore)(nil)

// NewPasskeyStore returns the adapter over store.
func NewPasskeyStore(store Store) *PasskeyStore {
	return &PasskeyStore{store: store}
}

// AddCredential implements passkey.Store.
func (s *PasskeyStore) AddCredential(ctx context.Context, cred passkey.Credential) error {
	accountID, err := uuid.Parse(cred.AccountID)
	if err != nil {
		return passkey.ErrNotFound
	}

	err = withScope(ctx, s.store, func(ctx context.Context) error {
		return s.store.Passkeys().Add(ctx, storage.PasskeyCredential{
			ID:         cred.ID,
			AccountID:  accountID,
			UserHandle: cred.UserHandle,
			Name:       cred.Name,
			SignCount:  cred.SignCount,
			Data:       cred.Data,
			CreatedAt:  cred.CreatedAt,
			LastUsedAt: timePtr(cred.LastUsedAt),
		})
	})

	return passkeyError("add passkey", err)
}

// Credentials implements passkey.Store.
func (s *PasskeyStore) Credentials(ctx context.Context, accountID string) ([]passkey.Credential, error) {
	id, err := uuid.Parse(accountID)
	if err != nil {
		return []passkey.Credential{}, nil
	}

	var found []storage.PasskeyCredential

	err = withReadScope(ctx, s.store, func(ctx context.Context) error {
		var err error
		found, err = s.store.Passkeys().List(ctx, id)

		return err
	})
	if errors.Is(err, errUnknownTenant) {
		return []passkey.Credential{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("auth: list passkeys: %w", err)
	}

	return passkeyCredentials(found), nil
}

// FindByUserHandle implements passkey.Store.
func (s *PasskeyStore) FindByUserHandle(ctx context.Context, handle []byte) (account.Identity, []passkey.Credential, error) {
	var (
		accountID uuid.UUID
		email     string
		found     []storage.PasskeyCredential
	)

	err := withReadScope(ctx, s.store, func(ctx context.Context) error {
		var err error
		accountID, email, found, err = s.store.Passkeys().FindByUserHandle(ctx, handle)

		return err
	})
	if err != nil {
		return account.Identity{}, nil, passkeyError("find passkey account", err)
	}

	return account.Identity{ID: accountID.String(), Login: email}, passkeyCredentials(found), nil
}

// UpdateCredential implements passkey.Store.
func (s *PasskeyStore) UpdateCredential(ctx context.Context, cred passkey.Credential, oldSignCount uint32) error {
	accountID, err := uuid.Parse(cred.AccountID)
	if err != nil {
		return passkey.ErrNotFound
	}

	err = withScope(ctx, s.store, func(ctx context.Context) error {
		return s.store.Passkeys().Update(ctx, storage.PasskeyCredential{
			ID:         cred.ID,
			AccountID:  accountID,
			SignCount:  cred.SignCount,
			Data:       cred.Data,
			LastUsedAt: timePtr(cred.LastUsedAt),
		}, oldSignCount)
	})

	return passkeyError("update passkey", err)
}

// DeleteCredential implements passkey.Store.
func (s *PasskeyStore) DeleteCredential(ctx context.Context, accountID string, id []byte, keepOne bool) error {
	parsed, err := uuid.Parse(accountID)
	if err != nil {
		return passkey.ErrNotFound
	}

	err = withScope(ctx, s.store, func(ctx context.Context) error {
		return s.store.Passkeys().Delete(ctx, parsed, id, keepOne)
	})

	return passkeyError("delete passkey", err)
}

func passkeyCredentials(found []storage.PasskeyCredential) []passkey.Credential {
	creds := make([]passkey.Credential, len(found))
	for i, c := range found {
		creds[i] = passkey.Credential{
			ID:         c.ID,
			AccountID:  c.AccountID.String(),
			UserHandle: c.UserHandle,
			Name:       c.Name,
			SignCount:  c.SignCount,
			Data:       c.Data,
			CreatedAt:  c.CreatedAt,
		}
		if c.LastUsedAt != nil {
			creds[i].LastUsedAt = *c.LastUsedAt
		}
	}

	return creds
}

// timePtr maps govue's zero time, "never", to nil, a NULL column.
func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}

	return &t
}

// passkeyError maps a storage outcome to the passkey package's errors.
func passkeyError(what string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, storage.ErrNotFound), errors.Is(err, errUnknownTenant):
		return passkey.ErrNotFound
	case errors.Is(err, storage.ErrConflict):
		return passkey.ErrConflict
	default:
		return fmt.Errorf("auth: %s: %w", what, err)
	}
}
