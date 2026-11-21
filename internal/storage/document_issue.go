package storage

import (
	"errors"
	"time"
)

// ErrNotADraft is what DocumentRepository.Issue wraps together with
// ErrConflict when its write matched no draft of the given type: the
// row is issued already, has another type, or is gone. A unique index
// refusing the display number or the correction sequence is ErrConflict
// alone, so a caller can tell the two apart.
var ErrNotADraft = errors.New("not a draft of this type")

// DocumentIssue is everything DocumentRepository.Issue writes when it
// moves a draft to issued. Issue #70 (I4).
type DocumentIssue struct {
	// Type is the draft's own type. Issue refuses a draft of another
	// type, so a number allocated under one type never lands on a
	// document of another.
	Type DocumentType
	// Number and NumberYear are the counter key: allocated from
	// document_numbers for an invoice or an offer, the original's own
	// values for a correction.
	Number     int64
	NumberYear int
	// DisplayNumber is the number as printed: the rendered template, or
	// for a correction the original's display number plus "-" and
	// CorrectionSeq.
	DisplayNumber string
	// CorrectionSeq is set for a correction only: its place in the chain,
	// 1 or higher.
	CorrectionSeq *int32
	IssuedAt      time.Time
	// Payload is the marshalled docpayload.FrozenPayloadV1 and
	// PayloadSHA256 the lowercase hex SHA-256 of exactly these bytes.
	Payload       []byte
	PayloadSHA256 string
}
