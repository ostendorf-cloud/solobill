package postgres

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres/sqlcgen"
)

// passkeyRepository satisfies storage.PasskeyRepository. Every method
// requires a context storage.TenantScope.WithTenant produced; see
// txFromContext. The statements live in queries/passkeys.sql.
//
// A unique violation aborts the scoped transaction, and the error this
// returns rolls it back in WithTenant, so a refused Add leaves no handle
// behind.
type passkeyRepository struct{}

// Add implements storage.PasskeyRepository.
func (r *passkeyRepository) Add(ctx context.Context, cred storage.PasskeyCredential) error {
	tx, err := txFromContext(ctx)
	if err != nil {
		return err
	}

	q := sqlcgen.New(tx)

	if _, err := q.LockPasskeyAccount(ctx, cred.AccountID); err != nil {
		return mapReadError(err)
	}

	handle, err := q.GetPasskeyUserHandle(ctx, cred.AccountID)
	switch {
	case err == nil:
		if !bytes.Equal(handle, cred.UserHandle) {
			return storage.ErrConflict
		}
	case errors.Is(err, pgx.ErrNoRows):
		// The first passkey of the account brings its handle. A handle
		// another account holds is a unique violation, ErrConflict.
		if err := q.CreatePasskeyUserHandle(ctx, sqlcgen.CreatePasskeyUserHandleParams{
			AccountID: cred.AccountID,
			Handle:    cred.UserHandle,
		}); err != nil {
			return mapWriteError(err)
		}
	default:
		return mapReadError(err)
	}

	if err := q.CreatePasskeyCredential(ctx, sqlcgen.CreatePasskeyCredentialParams{
		CredentialID: cred.ID,
		AccountID:    cred.AccountID,
		Name:         cred.Name,
		SignCount:    int64(cred.SignCount),
		Data:         cred.Data,
		CreatedAt:    toTimestamptz(cred.CreatedAt),
		LastUsedAt:   ptrToTimestamptz(cred.LastUsedAt),
	}); err != nil {
		return mapWriteError(err)
	}

	return nil
}

// List implements storage.PasskeyRepository.
func (r *passkeyRepository) List(ctx context.Context, accountID uuid.UUID) ([]storage.PasskeyCredential, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := sqlcgen.New(tx).ListPasskeyCredentials(ctx, accountID)
	if err != nil {
		return nil, mapReadError(err)
	}

	creds := make([]storage.PasskeyCredential, len(rows))
	for i, row := range rows {
		creds[i] = storage.PasskeyCredential{
			ID:         row.CredentialID,
			AccountID:  row.AccountID,
			UserHandle: row.Handle,
			Name:       row.Name,
			SignCount:  signCount(row.SignCount),
			Data:       row.Data,
			CreatedAt:  timeFromTimestamptz(row.CreatedAt),
			LastUsedAt: timeToPtr(row.LastUsedAt),
		}
	}

	return creds, nil
}

// FindByUserHandle implements storage.PasskeyRepository.
func (r *passkeyRepository) FindByUserHandle(ctx context.Context, handle []byte) (uuid.UUID, string, []storage.PasskeyCredential, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return uuid.Nil, "", nil, err
	}

	row, err := sqlcgen.New(tx).GetPasskeyAccountByUserHandle(ctx, handle)
	if err != nil {
		return uuid.Nil, "", nil, mapReadError(err)
	}

	creds, err := r.List(ctx, row.ID)
	if err != nil {
		return uuid.Nil, "", nil, err
	}

	return row.ID, row.Email, creds, nil
}

// Update implements storage.PasskeyRepository.
func (r *passkeyRepository) Update(ctx context.Context, cred storage.PasskeyCredential, oldSignCount uint32) error {
	tx, err := txFromContext(ctx)
	if err != nil {
		return err
	}

	n, err := sqlcgen.New(tx).UpdatePasskeyCredential(ctx, sqlcgen.UpdatePasskeyCredentialParams{
		CredentialID: cred.ID,
		AccountID:    cred.AccountID,
		SignCount:    int64(cred.SignCount),
		LastUsedAt:   ptrToTimestamptz(cred.LastUsedAt),
		Data:         cred.Data,
		OldSignCount: int64(oldSignCount),
	})

	return rowsChanged(n, err)
}

// Delete implements storage.PasskeyRepository.
func (r *passkeyRepository) Delete(ctx context.Context, accountID uuid.UUID, id []byte, keepOne bool) error {
	tx, err := txFromContext(ctx)
	if err != nil {
		return err
	}

	q := sqlcgen.New(tx)

	// The same lock Add takes: two deletes racing for the last two
	// passkeys run one after the other, and the second sees one left.
	if _, err := q.LockPasskeyAccount(ctx, accountID); err != nil {
		return mapReadError(err)
	}

	count, err := q.CountPasskeyCredentials(ctx, accountID)
	if err != nil {
		return mapReadError(err)
	}
	if keepOne && count < 2 {
		return storage.ErrNotFound
	}

	n, err := q.DeletePasskeyCredential(ctx, sqlcgen.DeletePasskeyCredentialParams{
		CredentialID: id,
		AccountID:    accountID,
	})
	if err := rowsChanged(n, err); err != nil {
		return err
	}

	// The handle goes with the last credential, so the next registration
	// may bring a new one.
	if count == 1 {
		if err := q.DeletePasskeyUserHandle(ctx, accountID); err != nil {
			return mapWriteError(err)
		}
	}

	return nil
}

// signCount narrows the stored counter back to the uint32 WebAuthn uses.
// The column's CHECK keeps it in range, so the clamp never applies to a
// row this package wrote.
func signCount(v int64) uint32 {
	switch {
	case v < 0:
		return 0
	case v > int64(^uint32(0)):
		return ^uint32(0)
	default:
		return uint32(v)
	}
}

// Compile time proof of the interfaces, next to the one place that
// otherwise only a caller would notice.
var (
	_ storage.LoginAccountRepository = (*loginAccountRepository)(nil)
	_ storage.PasskeyRepository      = (*passkeyRepository)(nil)
)
