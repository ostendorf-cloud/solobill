package postgres

import "github.com/ostendorf-cloud/solobill/internal/storage"

// Invites returns the invite repository. Every method on it requires a
// context WithTenant produced; see storage.InviteRepository.
func (s *Store) Invites() storage.InviteRepository {
	return &inviteRepository{}
}
