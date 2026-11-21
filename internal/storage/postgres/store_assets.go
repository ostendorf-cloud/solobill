package postgres

import "github.com/ostendorf-cloud/solobill/internal/storage"

// Assets returns the asset register repository (issue #78). Every method
// on it requires a context WithTenant produced; see
// storage.AssetRepository.
func (s *Store) Assets() storage.AssetRepository {
	return &assetRepository{}
}
