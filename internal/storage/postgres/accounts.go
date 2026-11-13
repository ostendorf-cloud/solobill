package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres/sqlcgen"
)

// accountRepository satisfies storage.AccountRepository. Every method
// requires a context storage.TenantScope.WithTenant produced; see
// txFromContext.
type accountRepository struct{}

// Create implements storage.AccountRepository.
func (r *accountRepository) Create(ctx context.Context, account storage.Account) (storage.Account, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Account{}, err
	}

	row, err := sqlcgen.New(tx).CreateAccount(ctx, sqlcgen.CreateAccountParams{
		TenantID:     account.TenantID,
		Email:        account.Email,
		PasswordHash: ptrToText(account.PasswordHash),
		TotpSecret:   ptrToText(account.TOTPSecret),
		Role:         string(account.Role),
		Status:       string(account.Status),
	})
	if err != nil {
		return storage.Account{}, mapWriteError(err)
	}

	return accountFromRow(row), nil
}

// FindByID implements storage.AccountRepository.
func (r *accountRepository) FindByID(ctx context.Context, id uuid.UUID) (storage.Account, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Account{}, err
	}

	row, err := sqlcgen.New(tx).GetAccountByID(ctx, id)
	if err != nil {
		return storage.Account{}, mapReadError(err)
	}

	return accountFromRow(row), nil
}

// FindByEmail implements storage.AccountRepository.
func (r *accountRepository) FindByEmail(ctx context.Context, email string) (storage.Account, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Account{}, err
	}

	row, err := sqlcgen.New(tx).GetAccountByEmail(ctx, email)
	if err != nil {
		return storage.Account{}, mapReadError(err)
	}

	return accountFromRow(row), nil
}

// List implements storage.AccountRepository.
func (r *accountRepository) List(ctx context.Context) ([]storage.Account, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := sqlcgen.New(tx).ListAccounts(ctx)
	if err != nil {
		return nil, mapReadError(err)
	}

	accounts := make([]storage.Account, len(rows))
	for i, row := range rows {
		accounts[i] = accountFromRow(row)
	}

	return accounts, nil
}

// Update implements storage.AccountRepository.
func (r *accountRepository) Update(ctx context.Context, account storage.Account) (storage.Account, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Account{}, err
	}

	row, err := sqlcgen.New(tx).UpdateAccount(ctx, sqlcgen.UpdateAccountParams{
		ID:           account.ID,
		PasswordHash: ptrToText(account.PasswordHash),
		TotpSecret:   ptrToText(account.TOTPSecret),
		Role:         string(account.Role),
		Status:       string(account.Status),
	})
	if err != nil {
		return storage.Account{}, mapWriteError(err)
	}

	return accountFromRow(row), nil
}

func accountFromRow(row sqlcgen.Account) storage.Account {
	return storage.Account{
		ID:           row.ID,
		TenantID:     row.TenantID,
		Email:        row.Email,
		PasswordHash: textToPtr(row.PasswordHash),
		TOTPSecret:   textToPtr(row.TotpSecret),
		Role:         storage.AccountRole(row.Role),
		Status:       storage.AccountStatus(row.Status),
		CreatedAt:    timeFromTimestamptz(row.CreatedAt),
		UpdatedAt:    timeFromTimestamptz(row.UpdatedAt),
	}
}
