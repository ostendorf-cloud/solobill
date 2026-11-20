package postgres

import "github.com/ostendorf-cloud/solobill/internal/storage"

// LoginAccounts returns the login side of the account repository, which
// internal/auth adapts to govue's account.Store. Every method on it
// requires a context WithTenant produced; see storage.LoginAccountRepository.
func (s *Store) LoginAccounts() storage.LoginAccountRepository {
	return &loginAccountRepository{}
}
