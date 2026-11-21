package postgres

import "github.com/ostendorf-cloud/solobill/internal/storage"

// Payments returns the payment repository. Every method on it requires a
// context WithTenant produced; see storage.PaymentRepository.
func (s *Store) Payments() storage.PaymentRepository {
	return &paymentRepository{}
}
