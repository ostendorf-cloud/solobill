package postgres

import "github.com/ostendorf-cloud/solobill/internal/storage"

// Accounts returns the account repository. Every method on it requires a
// context WithTenant produced; see storage.AccountRepository.
func (s *Store) Accounts() storage.AccountRepository {
	return &accountRepository{}
}
