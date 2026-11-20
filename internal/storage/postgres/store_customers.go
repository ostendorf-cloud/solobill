package postgres

import "github.com/ostendorf-cloud/solobill/internal/storage"

// Customers returns the customer repository. Every method on it
// requires a context WithTenant produced; see storage.CustomerRepository.
func (s *Store) Customers() storage.CustomerRepository {
	return &customerRepository{}
}
