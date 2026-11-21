package postgres

import "github.com/ostendorf-cloud/solobill/internal/storage"

// EuerAssignments returns the year assignment repository of the EÜR
// report (issue #80). Every method on it requires a context WithTenant
// produced; see storage.EuerAssignmentRepository.
func (s *Store) EuerAssignments() storage.EuerAssignmentRepository {
	return &euerAssignmentRepository{}
}
