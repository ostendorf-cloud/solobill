package storage

import "context"

// EuerAssignmentRepository persists the year assignments of entries near
// the turn of the year. Every method must run inside a context
// TenantScope.WithTenant produced. There is no Update and no Delete.
// Implemented by issue #80 (I12).
type EuerAssignmentRepository interface {
	// Create adds an assignment. ErrConflict when the entry already has
	// an effective assignment and SupersedesID does not name it.
	Create(ctx context.Context, assignment EuerAssignment) (EuerAssignment, error)

	// ListEffective returns every assignment no later row supersedes.
	ListEffective(ctx context.Context) ([]EuerAssignment, error)
}
