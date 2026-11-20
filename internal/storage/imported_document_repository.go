package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ImportedIssue is everything that turns an imported draft into an
// issued, imported document. See ImportedDocumentRepository.
type ImportedIssue struct {
	// Number and NumberYear are the numeric key that keeps
	// documents_number_year_number_unique holding. The import derives
	// them from OriginalNumber by the rule docs/import.md states.
	Number     int64
	NumberYear int
	// OriginalNumber is the number exactly as the customer holds it.
	OriginalNumber string
	IssuedAt       time.Time
	// Payload is a FrozenPayloadV1 with Imported set, and PayloadSHA256
	// its lowercase hex SHA-256, the same precondition
	// DocumentRepository.Issue has.
	Payload       []byte
	PayloadSHA256 string
}

// ImportedDocumentRepository is the one controlled path an imported,
// already issued document enters the ledger by. It never weakens
// documents_block_issued_mutation: the import first creates a draft
// with its lines through DocumentRepository.Create, then calls
// IssueImported, which moves that draft to issued in one UPDATE while
// the row is still a draft. The trigger only refuses a change of a row
// that is already issued, so it stays exactly as strict as today.
//
// Every method must run inside a context TenantScope.WithTenant
// produced, inside the same transaction as the import commit.
// Implemented by issue #81 (I13).
type ImportedDocumentRepository interface {
	// IssueImported sets imported = true, the given number, the original
	// number and the frozen payload, and moves the draft to issued. It
	// never touches document_numbers. ErrConflict when the row is not a
	// draft or the number is taken.
	IssueImported(ctx context.Context, id uuid.UUID, issue ImportedIssue) (Document, error)

	// RaiseCounter sets the counter of docType and year to atLeast when
	// it is lower, so the next issued number continues after the highest
	// imported one. It never lowers a counter.
	RaiseCounter(ctx context.Context, docType DocumentType, year int, atLeast int64) error
}
