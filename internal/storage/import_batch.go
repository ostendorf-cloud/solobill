package storage

import (
	"time"

	"github.com/google/uuid"
)

// ImportBatchState is the lifecycle of one historic import.
type ImportBatchState string

const (
	// ImportBatchStateDryRun was parsed and checked, nothing written.
	ImportBatchStateDryRun ImportBatchState = "dry_run"
	// ImportBatchStateCommitted was written in one transaction.
	ImportBatchStateCommitted ImportBatchState = "committed"
)

// ImportBatch is one historic import (issue #81, I13). The import format
// is decided later by the owner, so this type carries only what every
// format needs. Issue #81 extends it in this file.
type ImportBatch struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	State       ImportBatchState
	CreatedBy   *uuid.UUID
	CreatedAt   time.Time
	CommittedAt *time.Time
}
