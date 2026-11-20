package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres/sqlcgen"
)

// loginAccountRepository satisfies storage.LoginAccountRepository. Every
// method requires a context storage.TenantScope.WithTenant produced; see
// txFromContext. The statements live in queries/login_accounts.sql.
type loginAccountRepository struct{}

// FindActiveByEmail implements storage.LoginAccountRepository.
func (r *loginAccountRepository) FindActiveByEmail(ctx context.Context, email string) (storage.LoginAccount, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.LoginAccount{}, err
	}

	row, err := sqlcgen.New(tx).GetActiveLoginAccountByEmail(ctx, email)
	if err != nil {
		return storage.LoginAccount{}, mapReadError(err)
	}

	return storage.LoginAccount{
		ID:           row.ID,
		Email:        row.Email,
		PasswordHash: row.PasswordHash.String,
		TOTPSecret:   row.TotpSecret.String,
		TOTPCounter:  row.TotpCounter,
	}, nil
}

// CreateMember implements storage.LoginAccountRepository.
func (r *loginAccountRepository) CreateMember(ctx context.Context, email, passwordHash, totpSecret string) (storage.LoginAccount, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.LoginAccount{}, err
	}

	row, err := sqlcgen.New(tx).CreateLoginMember(ctx, sqlcgen.CreateLoginMemberParams{
		Email:        email,
		PasswordHash: emptyToNullText(passwordHash),
		TotpSecret:   emptyToNullText(totpSecret),
	})
	if err != nil {
		return storage.LoginAccount{}, mapWriteError(err)
	}

	return storage.LoginAccount{
		ID:           row.ID,
		Email:        row.Email,
		PasswordHash: row.PasswordHash.String,
		TOTPSecret:   row.TotpSecret.String,
		TOTPCounter:  row.TotpCounter,
	}, nil
}

// AdvanceTOTPCounter implements storage.LoginAccountRepository.
func (r *loginAccountRepository) AdvanceTOTPCounter(ctx context.Context, id uuid.UUID, counter int64) error {
	tx, err := txFromContext(ctx)
	if err != nil {
		return err
	}

	n, err := sqlcgen.New(tx).AdvanceLoginTOTPCounter(ctx, sqlcgen.AdvanceLoginTOTPCounterParams{
		ID:      id,
		Counter: pgtype.Int8{Int64: counter, Valid: true},
	})

	return rowsChanged(n, err)
}

// ConfirmTOTP implements storage.LoginAccountRepository.
func (r *loginAccountRepository) ConfirmTOTP(ctx context.Context, id uuid.UUID, secret string, counter int64) error {
	tx, err := txFromContext(ctx)
	if err != nil {
		return err
	}

	n, err := sqlcgen.New(tx).ConfirmLoginTOTP(ctx, sqlcgen.ConfirmLoginTOTPParams{
		ID:      id,
		Secret:  emptyToNullText(secret),
		Counter: pgtype.Int8{Int64: counter, Valid: true},
	})

	return rowsChanged(n, err)
}

// ReplaceTOTPSecret implements storage.LoginAccountRepository.
func (r *loginAccountRepository) ReplaceTOTPSecret(ctx context.Context, id uuid.UUID, secret string) error {
	tx, err := txFromContext(ctx)
	if err != nil {
		return err
	}

	n, err := sqlcgen.New(tx).ReplaceLoginTOTPSecret(ctx, sqlcgen.ReplaceLoginTOTPSecretParams{
		ID:     id,
		Secret: emptyToNullText(secret),
	})

	return rowsChanged(n, err)
}

// SetPasswordHash implements storage.LoginAccountRepository.
func (r *loginAccountRepository) SetPasswordHash(ctx context.Context, id uuid.UUID, oldHash, newHash string) error {
	tx, err := txFromContext(ctx)
	if err != nil {
		return err
	}

	n, err := sqlcgen.New(tx).SetLoginPasswordHash(ctx, sqlcgen.SetLoginPasswordHashParams{
		ID:      id,
		OldHash: emptyToNullText(oldHash),
		NewHash: emptyToNullText(newHash),
	})

	return rowsChanged(n, err)
}

// emptyToNullText maps the empty string, govue's "none" for a hash or a
// secret, to SQL NULL, the way accounts has always stored "none". A
// comparison against NULL is never true, so an empty old hash or secret
// can never match a stored one either.
func emptyToNullText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

// rowsChanged maps the outcome of a conditional write: no row changed is
// storage.ErrNotFound, the refusal every such write answers with.
func rowsChanged(n int64, err error) error {
	if err != nil {
		return mapWriteError(err)
	}
	if n == 0 {
		return storage.ErrNotFound
	}

	return nil
}
