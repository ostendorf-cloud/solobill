// Package depsync checks that a dependency released as more than one artifact under one
// version stays pinned to that same version everywhere this repository names it.
package depsync

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

const (
	// goModulePath is the Go module half of govue, required in go.mod.
	goModulePath = "github.com/ostendorf-cloud/govue"
	// packageJSONName is the npm package half of govue, pinned in each package.json below.
	packageJSONName = "@ostendorf-cloud/govue"
)

// packageJSONPaths are the package.json files that pin packageJSONName, relative to the
// repository root: the app and the marketing site.
var packageJSONPaths = []string{"web/package.json", "landing/package.json"}

// pnpmGitSpec matches the pnpm git dependency specifier web/package.json uses to pin
// govue's Vue package: "ostendorf-cloud/govue#<tag>&path:/web". That shape is pnpm
// specific, not a semver range, so the tag has to be pulled out of it rather than
// compared as a whole string the way go.mod's resolved version can be.
var pnpmGitSpec = regexp.MustCompile(`^ostendorf-cloud/govue#([^&]+)&path:/web$`)

// govueTag extracts the git tag from a pnpm git dependency specifier for govue's Vue
// package.
func govueTag(spec string) (string, error) {
	match := pnpmGitSpec.FindStringSubmatch(spec)
	if match == nil {
		return "", fmt.Errorf("specifier %q is not an %q pnpm git dependency", spec, "ostendorf-cloud/govue#<tag>&path:/web")
	}
	return match[1], nil
}

// TestGovueTag covers the specifier shapes web/package.json's govue dependency can take.
func TestGovueTag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		spec    string
		want    string
		wantErr bool
	}{
		{name: "pinned tag", spec: "ostendorf-cloud/govue#v0.3.1&path:/web", want: "v0.3.1"},
		{name: "different tag", spec: "ostendorf-cloud/govue#v0.2.0&path:/web", want: "v0.2.0"},
		{name: "missing path suffix", spec: "ostendorf-cloud/govue#v0.3.1", wantErr: true},
		{name: "plain semver range", spec: "^0.3.1", wantErr: true},
		{name: "empty", spec: "", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := govueTag(tc.spec)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("govueTag(%q) = %q, want error", tc.spec, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("govueTag(%q) returned unexpected error: %v", tc.spec, err)
			}
			if got != tc.want {
				t.Fatalf("govueTag(%q) = %q, want %q", tc.spec, got, tc.want)
			}
		})
	}
}

// TestGovueVersionsMatch fails when go.mod and web/package.json disagree on which govue
// version they pin. govue ships a Go module and a Vue package under one tag; a mismatch
// here means the repository took half of a govue release and not the other half.
//
// Both sides are read from the files themselves rather than from a third, restated
// expectation: go.mod's side comes from "go list -m", the same resolution "go build"
// itself uses, not a re-parse of go.mod's text; web/package.json's side comes from its
// own dependencies map. web/pnpm-lock.yaml is deliberately not checked here: its
// specifier field already has to match web/package.json's for "pnpm install
// --frozen-lockfile" to pass, and its resolved entry pins a commit hash, not a tag, so it
// cannot be compared to a version directly.
//
// Every Vue project of the repository is checked: web/ and landing/.
func TestGovueVersionsMatch(t *testing.T) {
	t.Parallel()

	goVersion, err := goModGovueVersion(t)
	if err != nil {
		t.Fatalf("resolve go.mod's %s version: %v", goModulePath, err)
	}

	for _, path := range packageJSONPaths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			pkgVersion, err := packageJSONGovueTag(t, path)
			if err != nil {
				t.Fatalf("resolve %s's version in %s: %v", packageJSONName, path, err)
			}

			if goVersion != pkgVersion {
				t.Fatalf(
					"govue version mismatch: go.mod requires %s@%s, %s pins %s to %s. "+
						"govue ships its Go module and Vue package under one tag; pin both files to the same version.",
					goModulePath, goVersion, path, packageJSONName, pkgVersion,
				)
			}
		})
	}
}

// goModGovueVersion asks the go command for the version of goModulePath that go.mod
// actually resolves to.
func goModGovueVersion(t *testing.T) (string, error) {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "go", "list", "-m", "-json", goModulePath).Output()
	if err != nil {
		return "", fmt.Errorf("go list -m %s: %w", goModulePath, err)
	}

	var mod struct {
		Version string
	}
	if err := json.Unmarshal(out, &mod); err != nil {
		return "", fmt.Errorf("parse go list -m output: %w", err)
	}
	if mod.Version == "" {
		return "", fmt.Errorf("go list -m %s reported no version", goModulePath)
	}
	return mod.Version, nil
}

// packageJSONGovueTag reads the git tag the package.json at path, relative to the
// repository root, pins packageJSONName to.
func packageJSONGovueTag(t *testing.T, path string) (string, error) {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(path)))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}

	spec, ok := pkg.Dependencies[packageJSONName]
	if !ok {
		return "", fmt.Errorf("%s has no %s dependency", path, packageJSONName)
	}

	return govueTag(spec)
}
