package objectstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
)

// ErrIntegrity is returned when the bytes read from the store do not match
// the SHA-256 or the size of the files row.
var ErrIntegrity = errors.New("object integrity check failed")

// retentionYears is the retention of every stored file, counted from the
// end of the calendar year of the upload: section 147 Abs. 3 AO gives ten
// years for books and eight for Buchungsbelege, and the concept takes ten
// for everything.
const retentionYears = 10

// Key is the object key of a file: "<tenant id>/<file id>". It is
// generated, never derived from a client file name. The files table
// enforces the same shape with a CHECK constraint.
func Key(tenantID, fileID uuid.UUID) string {
	return tenantID.String() + "/" + fileID.String()
}

// RetainUntil is the object lock retention date for a file uploaded at
// uploadedAt: the end of that calendar year (UTC) plus ten years.
func RetainUntil(uploadedAt time.Time) time.Time {
	return time.Date(uploadedAt.UTC().Year()+retentionYears+1, time.January, 1, 0, 0, 0, 0, time.UTC)
}

// Verify reads r to the end, at most wantSize plus one byte, and compares
// the size and the SHA-256 with the files row. It returns ErrIntegrity,
// wrapped with the reason, on a mismatch.
func Verify(r io.Reader, wantSHA256 string, wantSize int64) error {
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(r, wantSize+1))
	if err != nil {
		return fmt.Errorf("objectstore: read object: %w", err)
	}

	return Compare(hash.Sum(nil), n, wantSHA256, wantSize)
}

// Compare checks a computed SHA-256 sum and byte count against the files
// row, the step Verify ends with, for a caller that hashes while it
// streams.
func Compare(sum []byte, size int64, wantSHA256 string, wantSize int64) error {
	if size != wantSize {
		return fmt.Errorf("%w: read %d bytes, want %d", ErrIntegrity, size, wantSize)
	}
	if got := hex.EncodeToString(sum); got != wantSHA256 {
		return fmt.Errorf("%w: sha256 %s, want %s", ErrIntegrity, got, wantSHA256)
	}

	return nil
}
