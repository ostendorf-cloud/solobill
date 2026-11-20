package objectstore

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// DefaultCheckInterval is how often Run checks the bucket: every minute,
// per docs/concepts/solobill.md, "Receipt storage and GoBD".
const DefaultCheckInterval = time.Minute

// checkTimeout bounds one check, so a store that hangs cannot stall the
// loop past the next interval.
const checkTimeout = 10 * time.Second

// BucketChecker asks the bucket what protects it. Client implements it.
type BucketChecker interface {
	BucketVersioning(ctx context.Context) (bool, error)
	BucketObjectLock(ctx context.Context) (bool, error)
}

// MonitorConfig is what NewMonitor needs.
type MonitorConfig struct {
	// Checker is nil when no object store is configured. Every check then
	// reports the bucket unreachable.
	Checker BucketChecker
	// RequireVersioning mirrors S3_REQUIRE_VERSIONING.
	RequireVersioning bool
	// RequireObjectLock mirrors S3_OBJECT_LOCK.
	RequireObjectLock bool
	Logger            *slog.Logger
	// Now is the clock. Nil is time.Now.
	Now func() time.Time
}

// Monitor keeps the result of the last bucket check. The process always
// starts: a missing protection only gates the file routes, at startup and
// at runtime alike.
type Monitor struct {
	cfg MonitorConfig

	mu     sync.RWMutex
	status storage.ObjectStoreStatus
}

// NewMonitor builds a Monitor. Its status gates the file routes until the
// first Check ran.
func NewMonitor(cfg MonitorConfig) *Monitor {
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	return &Monitor{
		cfg: cfg,
		status: storage.ObjectStoreStatus{
			RequireVersioning: cfg.RequireVersioning,
			RequireObjectLock: cfg.RequireObjectLock,
		},
	}
}

// Status returns the result of the last check.
func (m *Monitor) Status() storage.ObjectStoreStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.status
}

// Check asks the bucket once, stores the result and logs a warning with
// the reason when the protection is missing or the file routes are gated.
// The error of a failed call is logged, never returned: an unreachable
// bucket is a state, not a failure of the caller.
func (m *Monitor) Check(ctx context.Context) storage.ObjectStoreStatus {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	status := storage.ObjectStoreStatus{
		RequireVersioning: m.cfg.RequireVersioning,
		RequireObjectLock: m.cfg.RequireObjectLock,
		CheckedAt:         m.cfg.Now().UTC(),
	}

	reasons := m.ask(ctx, &status)

	m.mu.Lock()
	m.status = status
	m.mu.Unlock()

	if len(reasons) > 0 {
		m.cfg.Logger.WarnContext(ctx, "object store protection missing",
			"reason", strings.Join(reasons, "; "),
			"file_routes_available", status.FileRoutesAvailable(),
			"gobd_protection_active", status.GoBDProtectionActive())
	}

	return status
}

// ask fills the bucket fields of status and returns why the protection is
// missing, empty when it is complete.
func (m *Monitor) ask(ctx context.Context, status *storage.ObjectStoreStatus) []string {
	if m.cfg.Checker == nil {
		return []string{"no object store configured (S3_ENDPOINT is empty)"}
	}

	versioning, err := m.cfg.Checker.BucketVersioning(ctx)
	if err != nil {
		return []string{"bucket unreachable: " + err.Error()}
	}
	lock, err := m.cfg.Checker.BucketObjectLock(ctx)
	if err != nil {
		return []string{"bucket unreachable: " + err.Error()}
	}

	status.Reachable = true
	status.Versioning = versioning
	status.ObjectLock = lock

	var reasons []string
	if !versioning {
		reasons = append(reasons, "bucket versioning is not enabled")
	}
	if !lock {
		reasons = append(reasons, "bucket object lock is not enabled")
	}

	return reasons
}

// Run checks every interval until ctx ends. It does not check at once:
// the caller runs the startup Check itself, before it serves, so the first
// request already sees a checked status. A non-positive interval is
// DefaultCheckInterval.
func (m *Monitor) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultCheckInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Check(ctx)
		}
	}
}
