package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/config"
	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/pdf"
)

// fakeTypst writes a shell script that prints version as "typst
// --version" would, and returns its path.
func fakeTypst(t *testing.T, version string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake binary is a shell script")
	}
	path := filepath.Join(t.TempDir(), "typst")
	script := "#!/bin/sh\necho '" + version + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // an executable test fixture
		t.Fatal(err)
	}

	return path
}

func TestBuildRenderer(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   func(t *testing.T) config.PDFConfig
		wantNil bool
		wantErr error
	}{
		{
			name:    "not configured",
			input:   func(*testing.T) config.PDFConfig { return config.PDFConfig{} },
			wantNil: true,
		},
		{
			name: "binary missing",
			input: func(t *testing.T) config.PDFConfig {
				return config.PDFConfig{Enabled: true, TypstBinary: filepath.Join(t.TempDir(), "typst")}
			},
			wantErr: pdf.ErrUnavailable,
		},
		{
			name: "another typst version",
			input: func(t *testing.T) config.PDFConfig {
				return config.PDFConfig{Enabled: true, TypstBinary: fakeTypst(t, "typst 0.13.1 (8ace67d9)")}
			},
			wantErr: pdf.ErrTypstVersion,
		},
		{
			name: "the pinned typst version",
			input: func(t *testing.T) config.PDFConfig {
				return config.PDFConfig{Enabled: true, TypstBinary: fakeTypst(t, "typst "+pdf.TypstVersion+" (b33de9de)")}
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, err := buildRenderer(context.Background(), tt.input(t))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("buildRenderer() error = %v, want %v", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("buildRenderer() error = %v", err)
			}
			if (r == nil) != tt.wantNil {
				t.Errorf("buildRenderer() = %v, want nil %v", r, tt.wantNil)
			}
		})
	}
}

// TestWirePDF: without a binary the renderer stays nil, never a typed
// nil, and the log says issuing is off. With the real Typst of mise.toml
// it is wired.
func TestWirePDF(t *testing.T) {
	t.Parallel()

	t.Run("not configured", func(t *testing.T) {
		t.Parallel()

		var logs bytes.Buffer
		env := wireEnv{logger: slog.New(slog.NewTextHandler(&logs, nil))}
		var repos httpapi.Repositories
		if err := wirePDF(context.Background(), env, &repos); err != nil {
			t.Fatalf("wirePDF() error = %v", err)
		}
		if repos.Renderer != nil {
			t.Errorf("Renderer = %#v, want nil", repos.Renderer)
		}
		if !strings.Contains(logs.String(), "PDF_TYPST_BINARY is not set") {
			t.Errorf("log does not say why issuing is off: %s", logs.String())
		}
	})

	t.Run("configured", func(t *testing.T) {
		t.Parallel()

		bin, err := exec.LookPath("typst")
		if err != nil {
			t.Skip("typst is not on PATH: install it with mise (mise.toml pins it) to run this test")
		}
		env := wireEnv{
			cfg:    config.Config{PDF: config.PDFConfig{Enabled: true, TypstBinary: bin}},
			logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		}
		var repos httpapi.Repositories
		if err := wirePDF(context.Background(), env, &repos); err != nil {
			t.Fatalf("wirePDF() error = %v", err)
		}
		if _, ok := repos.Renderer.(*pdf.Renderer); !ok {
			t.Errorf("Renderer = %#v, want a *pdf.Renderer", repos.Renderer)
		}
	})
}
