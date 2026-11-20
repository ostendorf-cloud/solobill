package postgres

import "github.com/ostendorf-cloud/solobill/internal/storage"

// DocumentNumbers returns the document numbering repository. Every
// method on it requires a context WithTenant produced; see
// storage.DocumentNumberRepository.
func (s *Store) DocumentNumbers() storage.DocumentNumberRepository {
	return &documentNumberRepository{}
}
