package postgres

import "github.com/ostendorf-cloud/solobill/internal/storage"

// CatalogueItems returns the catalogue item repository. Every method on
// it requires a context WithTenant produced; see
// storage.CatalogueItemRepository.
func (s *Store) CatalogueItems() storage.CatalogueItemRepository {
	return &catalogueItemRepository{}
}
