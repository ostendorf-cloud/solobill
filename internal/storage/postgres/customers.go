package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres/sqlcgen"
)

// customerRepository satisfies storage.CustomerRepository. Every method
// requires a context storage.TenantScope.WithTenant produced; see
// txFromContext.
type customerRepository struct{}

// Create implements storage.CustomerRepository.
func (r *customerRepository) Create(ctx context.Context, customer storage.Customer) (storage.Customer, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Customer{}, err
	}

	row, err := sqlcgen.New(tx).CreateCustomer(ctx, sqlcgen.CreateCustomerParams{
		TenantID:    customer.TenantID,
		Name:        customer.Name,
		Email:       ptrToText(customer.Email),
		VatID:       ptrToText(customer.VATID),
		Street:      ptrToText(customer.Street),
		PostalCode:  ptrToText(customer.PostalCode),
		City:        ptrToText(customer.City),
		CountryCode: ptrToText(customer.CountryCode),
	})
	if err != nil {
		return storage.Customer{}, mapWriteError(err)
	}

	return customerFromRow(row), nil
}

// FindByID implements storage.CustomerRepository. It does not filter on
// a soft-deleted row; see storage.CustomerRepository.FindByID for why.
func (r *customerRepository) FindByID(ctx context.Context, id uuid.UUID) (storage.Customer, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Customer{}, err
	}

	row, err := sqlcgen.New(tx).GetCustomerByID(ctx, id)
	if err != nil {
		return storage.Customer{}, mapReadError(err)
	}

	return customerFromRow(row), nil
}

// List implements storage.CustomerRepository. It returns only active
// (not soft-deleted) customers; see storage.CustomerRepository.List for
// why.
func (r *customerRepository) List(ctx context.Context) ([]storage.Customer, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := sqlcgen.New(tx).ListCustomers(ctx)
	if err != nil {
		return nil, mapReadError(err)
	}

	customers := make([]storage.Customer, len(rows))
	for i, row := range rows {
		customers[i] = customerFromRow(row)
	}

	return customers, nil
}

// Update implements storage.CustomerRepository. The underlying query is
// guarded by "WHERE ... AND deleted_at IS NULL" (see
// internal/storage/postgres/queries/customers.sql), so a call against a
// row that does not exist, or is already soft deleted, matches zero
// rows. A :one sqlc query with zero rows answers pgx.ErrNoRows on Scan;
// that is checked for explicitly and mapped to storage.ErrNotFound, not
// storage.ErrConflict, because there is no concurrent-redeem race to
// report here, only "no such active row" (see
// storage.CustomerRepository.Update). Any other error, including a
// unique violation from a row that would otherwise exist, still goes
// through mapWriteError.
func (r *customerRepository) Update(ctx context.Context, customer storage.Customer) (storage.Customer, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Customer{}, err
	}

	row, err := sqlcgen.New(tx).UpdateCustomer(ctx, sqlcgen.UpdateCustomerParams{
		ID:          customer.ID,
		Name:        customer.Name,
		Email:       ptrToText(customer.Email),
		VatID:       ptrToText(customer.VATID),
		Street:      ptrToText(customer.Street),
		PostalCode:  ptrToText(customer.PostalCode),
		City:        ptrToText(customer.City),
		CountryCode: ptrToText(customer.CountryCode),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return storage.Customer{}, storage.ErrNotFound
		}

		return storage.Customer{}, mapWriteError(err)
	}

	return customerFromRow(row), nil
}

// Delete implements storage.CustomerRepository: it soft deletes, never
// hard deletes. The underlying query is an :execrows UPDATE, not a
// RETURNING :one, so the affected-row count is read directly rather than
// by scanning and discarding a row nothing needs; zero rows affected
// means the row does not exist or is already deleted, both of which map
// to storage.ErrNotFound.
func (r *customerRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tx, err := txFromContext(ctx)
	if err != nil {
		return err
	}

	affected, err := sqlcgen.New(tx).DeleteCustomer(ctx, id)
	if err != nil {
		return mapWriteError(err)
	}

	if affected == 0 {
		return storage.ErrNotFound
	}

	return nil
}

func customerFromRow(row sqlcgen.Customer) storage.Customer {
	return storage.Customer{
		ID:          row.ID,
		TenantID:    row.TenantID,
		Name:        row.Name,
		Email:       textToPtr(row.Email),
		VATID:       textToPtr(row.VatID),
		Street:      textToPtr(row.Street),
		PostalCode:  textToPtr(row.PostalCode),
		City:        textToPtr(row.City),
		CountryCode: textToPtr(row.CountryCode),
		DeletedAt:   timeToPtr(row.DeletedAt),
		CreatedAt:   timeFromTimestamptz(row.CreatedAt),
		UpdatedAt:   timeFromTimestamptz(row.UpdatedAt),
	}
}
