package pdf

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// overlapTypst writes a fake typst that fails when another copy of it
// runs at the same time: it takes a lock directory, holds it briefly,
// and prints a PDF header. Absolute paths, because Typst runs without
// PATH (minimalEnv).
// The returned directory exists once the script has run at least once.
func overlapTypst(t *testing.T) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake binary is a shell script")
	}
	dir := t.TempDir()
	lock := filepath.Join(dir, "lock")
	ran := filepath.Join(dir, "ran")
	path := filepath.Join(dir, "typst")
	script := "#!/bin/sh\n" +
		"/bin/mkdir -p '" + ran + "'\n" +
		"/bin/mkdir '" + lock + "' 2>/dev/null || exit 3\n" +
		"/bin/sleep 0.2\n" +
		"/bin/rmdir '" + lock + "'\n" +
		"printf '%%PDF-1.7\\n'\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // an executable test fixture
		t.Fatal(err)
	}

	return path, ran
}

func slotSource() Source {
	return Source{Template: []byte("x"), Data: []byte("{}"), CreationTime: time.Unix(0, 0).UTC()}
}

// TestRenderSlotsBoundTypst runs more renders at once than the renderer
// has slots: the fake typst fails on any overlap, so every render
// succeeding proves they ran one after the other.
func TestRenderSlotsBoundTypst(t *testing.T) {
	t.Parallel()

	bin, _ := overlapTypst(t)
	r := New(Options{Binary: bin, TempDir: t.TempDir(), MaxConcurrent: 1})
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = r.compile(context.Background(), slotSource())
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("render %d: %v, want every render to wait for its slot", i, err)
		}
	}
}

// TestRenderSlotWaitHonoursTheContext: with every slot taken a render
// gives up when its context ends, and starts no Typst.
func TestRenderSlotWaitHonoursTheContext(t *testing.T) {
	t.Parallel()

	bin, ran := overlapTypst(t)
	r := New(Options{Binary: bin, TempDir: t.TempDir(), MaxConcurrent: 1})
	release, err := r.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = r.compile(ctx, slotSource())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("compile with every slot taken = %v, want context.DeadlineExceeded", err)
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Errorf("waited %v, want about the context's 50 ms", waited)
	}
	if _, err := os.Stat(ran); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("typst ran while every slot was taken (stat: %v)", err)
	}
}

// TestRenderWaitsForAFreeSlot: a render started while every slot is
// taken starts no Typst until the slot is freed, then completes.
func TestRenderWaitsForAFreeSlot(t *testing.T) {
	t.Parallel()

	bin, ran := overlapTypst(t)
	r := New(Options{Binary: bin, TempDir: t.TempDir(), MaxConcurrent: 1})
	release, err := r.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := r.compile(context.Background(), slotSource())
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("compile returned %v while every slot was taken", err)
	case <-time.After(500 * time.Millisecond):
	}
	if _, err := os.Stat(ran); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("typst ran while every slot was taken (stat: %v)", err)
	}

	release()
	if err := <-done; err != nil {
		t.Fatalf("compile after the slot was freed: %v", err)
	}
	if _, err := os.Stat(ran); err != nil {
		t.Errorf("typst did not run after the slot was freed: %v", err)
	}
}

func TestNewDefaultsTheSlots(t *testing.T) {
	t.Parallel()

	if got := cap(New(Options{}).slots); got != DefaultMaxConcurrent {
		t.Errorf("slots = %d, want %d", got, DefaultMaxConcurrent)
	}
}
