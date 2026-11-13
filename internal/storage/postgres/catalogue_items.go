package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres/sqlcgen"
)

// catalogueItemRepository satisfies storage.CatalogueItemRepository.
// Every method requires a context storage.TenantScope.WithTenant
// produced; see txFromContext.
type catalogueItemRepository struct{}

// catalogueItemDefaultUnit mirrors the database's own DEFAULT 'piece' on
// catalogue_items.unit (see db/migrations), so a caller that leaves
// storage.CatalogueItem.Unit as the Go zero value gets the same default
// through either path.
const catalogueItemDefaultUnit = "piece"

// Create implements storage.CatalogueItemRepository.
func (r *catalogueItemRepository) Create(ctx context.Context, item storage.CatalogueItem) (storage.CatalogueItem, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.CatalogueItem{}, err
	}

	unit := item.Unit
	if unit == "" {
		unit = catalogueItemDefaultUnit
	}

	row, err := sqlcgen.New(tx).CreateCatalogueItem(ctx, sqlcgen.CreateCatalogueItemParams{
		TenantID:       item.TenantID,
		Name:           item.Name,
		Description:    ptrToText(item.Description),
		Sku:            ptrToText(item.SKU),
		UnitPriceCents: item.UnitPriceCents,
		Unit:           unit,
	})
	if err != nil {
		return storage.CatalogueItem{}, mapWriteError(err)
	}

	return catalogueItemFromRow(row), nil
}

// FindByID implements storage.CatalogueItemRepository. It does not
// filter on a soft-deleted row; see
// storage.CatalogueItemRepository.FindByID for why.
func (r *catalogueItemRepository) FindByID(ctx context.Context, id uuid.UUID) (storage.CatalogueItem, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.CatalogueItem{}, err
	}

	row, err := sqlcgen.New(tx).GetCatalogueItemByID(ctx, id)
	if err != nil {
		return storage.CatalogueItem{}, mapReadError(err)
	}

	return catalogueItemFromRow(row), nil
}

// List implements storage.CatalogueItemRepository. It returns only
// active (not soft-deleted) items; see
// storage.CatalogueItemRepository.List for why.
func (r *catalogueItemRepository) List(ctx context.Context) ([]storage.CatalogueItem, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := sqlcgen.New(tx).ListCatalogueItems(ctx)
	if err != nil {
		return nil, mapReadError(err)
	}

	items := make([]storage.CatalogueItem, len(rows))
	for i, row := range rows {
		items[i] = catalogueItemFromRow(row)
	}

	return items, nil
}

// Update implements storage.CatalogueItemRepository. The underlying
// query is guarded by "WHERE ... AND deleted_at IS NULL" (see
// internal/storage/postgres/queries/catalogue_items.sql), so a call
// against a row that does not exist, or is already soft deleted,
// matches zero rows. A :one sqlc query with zero rows answers
// pgx.ErrNoRows on Scan; that is checked for explicitly and mapped to
// storage.ErrNotFound, not storage.ErrConflict, for the same reason
// given on storage.CustomerRepository.Update: there is no
// concurrent-redeem race, only "no such active row". Any other error,
// including a unique violation on sku from a row that would otherwise
// exist, still goes through mapWriteError.
func (r *catalogueItemRepository) Update(ctx context.Context, item storage.CatalogueItem) (storage.CatalogueItem, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.CatalogueItem{}, err
	}

	unit := item.Unit
	if unit == "" {
		unit = catalogueItemDefaultUnit
	}

	row, err := sqlcgen.New(tx).UpdateCatalogueItem(ctx, sqlcgen.UpdateCatalogueItemParams{
		ID:             item.ID,
		Name:           item.Name,
		Description:    ptrToText(item.Description),
		Sku:            ptrToText(item.SKU),
		UnitPriceCents: item.UnitPriceCents,
		Unit:           unit,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return storage.CatalogueItem{}, storage.ErrNotFound
		}

		return storage.CatalogueItem{}, mapWriteError(err)
	}

	return catalogueItemFromRow(row), nil
}

// Delete implements storage.CatalogueItemRepository: it soft deletes,
// never hard deletes. The underlying query is an :execrows UPDATE, not
// a RETURNING :one, so the affected-row count is read directly rather
// than by scanning and discarding a row nothing needs; zero rows
// affected means the row does not exist or is already deleted, both of
// which map to storage.ErrNotFound.
func (r *catalogueItemRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tx, err := txFromContext(ctx)
	if err != nil {
		return err
	}

	affected, err := sqlcgen.New(tx).DeleteCatalogueItem(ctx, id)
	if err != nil {
		return mapWriteError(err)
	}

	if affected == 0 {
		return storage.ErrNotFound
	}

	return nil
}

func catalogueItemFromRow(row sqlcgen.CatalogueItem) storage.CatalogueItem {
	return storage.CatalogueItem{
		ID:             row.ID,
		TenantID:       row.TenantID,
		Name:           row.Name,
		Description:    textToPtr(row.Description),
		SKU:            textToPtr(row.Sku),
		UnitPriceCents: row.UnitPriceCents,
		Unit:           row.Unit,
		DeletedAt:      timeToPtr(row.DeletedAt),
		CreatedAt:      timeFromTimestamptz(row.CreatedAt),
		UpdatedAt:      timeFromTimestamptz(row.UpdatedAt),
	}
}
