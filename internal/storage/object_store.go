package storage

import (
	"context"
	"io"
	"time"
)

// ObjectInfo is what the object store reports about one stored object.
type ObjectInfo struct {
	SizeBytes   int64
	ContentType string
	// VersionID is empty when the bucket has no versioning.
	VersionID string
	// RetainUntil is nil when the object carries no object lock
	// retention.
	RetainUntil *time.Time
}

// PutObject is one upload into the object store.
type PutObject struct {
	Key         string
	Body        io.Reader
	SizeBytes   int64
	ContentType string
	// SHA256 is the lowercase hex SHA-256 of Body. The implementation
	// sends it as the S3 checksum so the store refuses a corrupted body.
	SHA256 string
	// RetainUntil requests object lock retention in compliance mode. nil
	// requests none. The implementation ignores it, and reports that in
	// the returned ObjectInfo, when the bucket has no object lock.
	RetainUntil *time.Time
}

// ObjectStore is the S3 compatible store receipts, logos and imported
// originals live in. Implemented by issue #75 (I9) against SeaweedFS in
// the cluster and in docker compose.
//
// There is deliberately no Delete: the application has no delete path
// for a stored object. A failed metadata insert after a successful Put
// leaves an orphaned object, which is accepted: it is never referenced
// and costs only space.
type ObjectStore interface {
	Put(ctx context.Context, object PutObject) (ObjectInfo, error)

	// Get opens the object for reading. The caller verifies the SHA-256
	// of what it reads against File.SHA256. ErrNotFound when the key
	// does not exist.
	Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)

	// Stat reports an object without reading it. ErrNotFound when the
	// key does not exist.
	Stat(ctx context.Context, key string) (ObjectInfo, error)

	// BucketVersioning reports whether versioning is enabled on the
	// configured bucket, checked once at startup.
	BucketVersioning(ctx context.Context) (bool, error)
}
