package storage

import (
	"errors"
	"time"
)

// ErrObjectStoreUnavailable is returned by an ObjectStore that refuses a
// call because the bucket is gated: unreachable, without versioning while
// versioning is required, or without object lock while object lock is
// requested. The HTTP layer answers it with 503 object_store_unavailable.
// See docs/concepts/solobill.md, "Receipt storage and GoBD".
var ErrObjectStoreUnavailable = errors.New("object store unavailable")

// ObjectStoreStatus is the result of the last periodic check of the
// bucket. The zero value is "never checked", which gates the file routes.
type ObjectStoreStatus struct {
	// Reachable is true when the last check could talk to the bucket.
	Reachable bool
	// Versioning is true when the bucket reports versioning enabled.
	Versioning bool
	// ObjectLock is true when the bucket reports object lock enabled.
	ObjectLock bool
	// RequireVersioning mirrors S3_REQUIRE_VERSIONING.
	RequireVersioning bool
	// RequireObjectLock mirrors S3_OBJECT_LOCK.
	RequireObjectLock bool
	// CheckedAt is when the check ran, zero when it never ran.
	CheckedAt time.Time
}

// FileRoutesAvailable reports whether the file routes may run: the bucket
// was checked, is reachable, and carries what the configuration requires.
func (s ObjectStoreStatus) FileRoutesAvailable() bool {
	if s.CheckedAt.IsZero() || !s.Reachable {
		return false
	}
	if s.RequireVersioning && !s.Versioning {
		return false
	}
	if s.RequireObjectLock && !s.ObjectLock {
		return false
	}

	return true
}

// GoBDProtectionActive reports whether the store side protection the
// concept requires is in place: reachable, versioned and locked. It is
// independent of the configuration: a bucket without lock is never
// protected, whatever S3_OBJECT_LOCK says.
func (s ObjectStoreStatus) GoBDProtectionActive() bool {
	return !s.CheckedAt.IsZero() && s.Reachable && s.Versioning && s.ObjectLock
}
