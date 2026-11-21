package postgres

import "github.com/ostendorf-cloud/solobill/internal/storage"

// Passkeys returns the passkey repository, which internal/auth adapts to
// govue's passkey.Store. Every method on it requires a context WithTenant
// produced; see storage.PasskeyRepository.
func (s *Store) Passkeys() storage.PasskeyRepository {
	return &passkeyRepository{}
}
