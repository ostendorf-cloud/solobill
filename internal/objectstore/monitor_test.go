package objectstore_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ostendorf-cloud/solobill/internal/objectstore"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// fakeChecker answers the two bucket checks with fixed values.
type fakeChecker struct {
	versioning, lock bool
	err              error
}

func (c *fakeChecker) BucketVersioning(context.Context) (bool, error) { return c.versioning, c.err }
func (c *fakeChecker) BucketObjectLock(context.Context) (bool, error) { return c.lock, c.err }

func fixedNow() time.Time { return time.Date(2026, time.December, 1, 10, 0, 0, 0, time.UTC) }

func TestMonitorCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		checker           objectstore.BucketChecker
		requireVersioning bool
		requireLock       bool
		wantReachable     bool
		wantAvailable     bool
		wantProtected     bool
		wantWarning       string
	}{
		{
			name:    "versioned and locked",
			checker: &fakeChecker{versioning: true, lock: true}, requireVersioning: true, requireLock: true,
			wantReachable: true, wantAvailable: true, wantProtected: true,
		},
		{
			name:    "no versioning while required",
			checker: &fakeChecker{lock: true}, requireVersioning: true,
			wantReachable: true, wantWarning: "versioning is not enabled",
		},
		{
			name:    "no versioning, not required",
			checker: &fakeChecker{}, requireVersioning: false,
			wantReachable: true, wantAvailable: true, wantWarning: "versioning is not enabled",
		},
		{
			name:    "no lock while requested",
			checker: &fakeChecker{versioning: true}, requireVersioning: true, requireLock: true,
			wantReachable: true, wantWarning: "object lock is not enabled",
		},
		{
			name:    "no lock, not requested",
			checker: &fakeChecker{versioning: true}, requireVersioning: true,
			wantReachable: true, wantAvailable: true, wantWarning: "object lock is not enabled",
		},
		{
			name:    "unreachable",
			checker: &fakeChecker{err: errors.New("dial tcp: connection refused")}, requireVersioning: false,
			wantWarning: "bucket unreachable",
		},
		{
			name:    "not configured",
			checker: nil, requireVersioning: false,
			wantWarning: "no object store configured",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer
			m := objectstore.NewMonitor(objectstore.MonitorConfig{
				Checker:           tt.checker,
				RequireVersioning: tt.requireVersioning,
				RequireObjectLock: tt.requireLock,
				Logger:            slog.New(slog.NewTextHandler(&logs, nil)),
				Now:               fixedNow,
			})

			if m.Status().FileRoutesAvailable() {
				t.Fatal("FileRoutesAvailable() before the first check = true, want false")
			}

			got := m.Check(context.Background())
			if got != m.Status() {
				t.Errorf("Check() = %+v, Status() = %+v, want equal", got, m.Status())
			}
			if got.Reachable != tt.wantReachable {
				t.Errorf("Reachable = %v, want %v", got.Reachable, tt.wantReachable)
			}
			if got.FileRoutesAvailable() != tt.wantAvailable {
				t.Errorf("FileRoutesAvailable() = %v, want %v", got.FileRoutesAvailable(), tt.wantAvailable)
			}
			if got.GoBDProtectionActive() != tt.wantProtected {
				t.Errorf("GoBDProtectionActive() = %v, want %v", got.GoBDProtectionActive(), tt.wantProtected)
			}
			if !got.CheckedAt.Equal(fixedNow()) {
				t.Errorf("CheckedAt = %v, want %v", got.CheckedAt, fixedNow())
			}

			out := logs.String()
			switch {
			case tt.wantWarning == "" && out != "":
				t.Errorf("log = %q, want nothing", out)
			case tt.wantWarning != "" && (!strings.Contains(out, "level=WARN") || !strings.Contains(out, tt.wantWarning)):
				t.Errorf("log = %q, want a WARN line with %q", out, tt.wantWarning)
			}
		})
	}
}

// TestMonitorLostAtRuntime proves that protection lost after a good check
// gates the file routes again on the next check.
func TestMonitorLostAtRuntime(t *testing.T) {
	t.Parallel()

	checker := &fakeChecker{versioning: true, lock: true}
	m := objectstore.NewMonitor(objectstore.MonitorConfig{Checker: checker, RequireVersioning: true, RequireObjectLock: true, Now: fixedNow})

	if !m.Check(context.Background()).FileRoutesAvailable() {
		t.Fatal("first check: file routes gated, want available")
	}

	checker.lock = false
	if m.Check(context.Background()).FileRoutesAvailable() {
		t.Fatal("second check without lock: file routes available, want gated")
	}
}

// fakeStore records whether a call reached it.
type fakeStore struct{ calls int }

func (s *fakeStore) Put(context.Context, storage.PutObject) (storage.ObjectInfo, error) {
	s.calls++

	return storage.ObjectInfo{}, nil
}

func (s *fakeStore) Get(context.Context, string) (io.ReadCloser, storage.ObjectInfo, error) {
	s.calls++

	return io.NopCloser(strings.NewReader("")), storage.ObjectInfo{}, nil
}

func (s *fakeStore) Stat(context.Context, string) (storage.ObjectInfo, error) {
	s.calls++

	return storage.ObjectInfo{}, nil
}

func (s *fakeStore) BucketVersioning(context.Context) (bool, error) {
	s.calls++

	return true, nil
}

func TestGuardedRefusesWhileGated(t *testing.T) {
	t.Parallel()

	checker := &fakeChecker{versioning: false}
	store := &fakeStore{}
	m := objectstore.NewMonitor(objectstore.MonitorConfig{Checker: checker, RequireVersioning: true, Now: fixedNow})
	g := &objectstore.Guarded{Store: store, Monitor: m}

	ctx := context.Background()
	call := func() []error {
		_, err1 := g.Put(ctx, storage.PutObject{})
		_, _, err2 := g.Get(ctx, "k")
		_, err3 := g.Stat(ctx, "k")

		return []error{err1, err2, err3}
	}

	// Before the first check and after a failing one, every call is refused
	// and never reaches the store.
	for _, err := range call() {
		if !errors.Is(err, storage.ErrObjectStoreUnavailable) {
			t.Errorf("unchecked: error = %v, want ErrObjectStoreUnavailable", err)
		}
	}
	m.Check(ctx)
	for _, err := range call() {
		if !errors.Is(err, storage.ErrObjectStoreUnavailable) {
			t.Errorf("gated: error = %v, want ErrObjectStoreUnavailable", err)
		}
	}
	if store.calls != 0 {
		t.Fatalf("store got %d calls while gated, want 0", store.calls)
	}

	checker.versioning = true
	m.Check(ctx)
	for _, err := range call() {
		if err != nil {
			t.Errorf("open: error = %v, want nil", err)
		}
	}
	if store.calls != 3 {
		t.Errorf("store got %d calls while open, want 3", store.calls)
	}
}
