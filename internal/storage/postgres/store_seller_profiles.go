package postgres

import "github.com/ostendorf-cloud/solobill/internal/storage"

// SellerProfiles returns the seller profile repository. Every method on
// it requires a context WithTenant produced; see
// storage.SellerProfileRepository.
func (s *Store) SellerProfiles() storage.SellerProfileRepository {
	return &sellerProfileRepository{}
}
