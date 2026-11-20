package postgres

import "github.com/ostendorf-cloud/solobill/internal/storage"

// OtherIncome returns the other income repository. Every method on it
// requires a context WithTenant produced; see storage.OtherIncomeRepository.
func (s *Store) OtherIncome() storage.OtherIncomeRepository {
	return &otherIncomeRepository{}
}
