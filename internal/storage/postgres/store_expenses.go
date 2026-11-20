package postgres

import "github.com/ostendorf-cloud/solobill/internal/storage"

// Expenses returns the expense repository. Every method on it requires a
// context WithTenant produced; see storage.ExpenseRepository.
func (s *Store) Expenses() storage.ExpenseRepository {
	return &expenseRepository{}
}
