package postgres

import "github.com/ostendorf-cloud/solobill/internal/storage"

// Documents returns the document repository. Every method on it
// requires a context WithTenant produced; see storage.DocumentRepository.
func (s *Store) Documents() storage.DocumentRepository {
	return &documentRepository{}
}
