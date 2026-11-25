package pdf

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// failingTypst writes a fake typst that prints stderr and exits 1.
func failingTypst(t *testing.T, stderr string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake binary is a shell script")
	}
	dir := t.TempDir()
	msg := filepath.Join(dir, "stderr")
	if err := os.WriteFile(msg, []byte(stderr), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "typst")
	script := "#!/bin/sh\n/bin/cat '" + msg + "' >&2\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // an executable test fixture
		t.Fatal(err)
	}

	return path
}

// TestCompileErrorCarriesOnlyTheFirstDiagnosticLine: the error of a
// failed compile, which reaches the server log, holds the first line of
// Typst's diagnostics, bounded, and none of the quoted source or data.
func TestCompileErrorCarriesOnlyTheFirstDiagnosticLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    string
		notWant string
	}{
		{
			name:    "the quoted data on later lines",
			input:   "main.typ:12:3: error: assertion failed\n  12 | Mustermann Consulting GmbH, IBAN DE00\n",
			want:    "main.typ:12:3: error: assertion failed",
			notWant: "Mustermann",
		},
		{
			name:    "a first line over the bound",
			input:   "main.typ:1:1: error: " + strings.Repeat("x", 400) + "SECRET-TAIL\n",
			want:    "main.typ:1:1: error: xxx",
			notWant: "SECRET-TAIL",
		},
		{
			name:    "control characters",
			input:   "main.typ:1:1: error: a\x1b[31mb\n",
			want:    "error: a?[31mb",
			notWant: "\x1b",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := New(Options{Binary: failingTypst(t, tt.input), TempDir: t.TempDir()})
			_, err := r.compile(context.Background(), slotSource())
			if !errors.Is(err, ErrCompile) {
				t.Fatalf("compile = %v, want ErrCompile", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q lacks %q", err, tt.want)
			}
			if strings.Contains(err.Error(), tt.notWant) {
				t.Errorf("error %q carries %q", err, tt.notWant)
			}
			if len(err.Error()) > 300 {
				t.Errorf("error is %d bytes long, want it bounded", len(err.Error()))
			}
		})
	}
}
