package pdf

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestRenderUsesOnlyTheTempDirectory is the unit level proof of what the
// chart relies on: with the root filesystem read only and one writable
// emptyDir at os.TempDir (/tmp), a render works and leaves nothing behind.
// The test points TMPDIR at the only writable directory and runs the
// renderer from a working directory it cannot write to.
func TestRenderUsesOnlyTheTempDirectory(t *testing.T) {
	bin := typstOrSkip(t)
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	payload, err := os.ReadFile(fixturePath("invoice-standard-de"))
	if err != nil {
		t.Fatal(err)
	}
	// An absolute binary path, so the read only working directory below
	// does not change what is looked up.
	bin, err = filepath.Abs(bin)
	if err != nil {
		t.Fatal(err)
	}

	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) })
	t.Chdir(readOnly)

	t.Run("a writable temp directory renders and is left empty", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("TMPDIR", tmp)
		r := New(Options{Binary: bin})
		pdf, err := r.Render(context.Background(), payload, false)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
			t.Fatalf("output is no PDF: %q", pdf[:min(len(pdf), 16)])
		}
		left, err := os.ReadDir(tmp)
		if err != nil {
			t.Fatal(err)
		}
		if len(left) != 0 {
			t.Errorf("the render left %d entries in the temp directory", len(left))
		}
	})

	t.Run("a read only temp directory fails at the temp directory", func(t *testing.T) {
		t.Setenv("TMPDIR", readOnly)
		r := New(Options{Binary: bin})
		_, err := r.Render(context.Background(), payload, false)
		if err == nil {
			t.Fatal("render succeeded without a writable temp directory")
		}
		if errors.Is(err, ErrUnavailable) {
			t.Errorf("want a temp dir error, got %v", err)
		}
	})
}
