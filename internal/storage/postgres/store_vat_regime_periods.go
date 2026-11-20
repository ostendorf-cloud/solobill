package postgres

import "github.com/ostendorf-cloud/solobill/internal/storage"

// VATRegimePeriods returns the VAT regime period repository. Every method
// on it requires a context WithTenant produced; see
// storage.VATRegimePeriodRepository.
func (s *Store) VATRegimePeriods() storage.VATRegimePeriodRepository {
	return &vatRegimePeriodRepository{}
}
