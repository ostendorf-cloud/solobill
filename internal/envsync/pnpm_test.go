// Package envsync checks that the files which describe the build environment of solobill
// stay in step with each other.
package envsync

import (
	"os"
	"regexp"
	"testing"
)

var (
	// dockerfilePnpm is the version argument Renovate updates; dockerfileCorepack
	// is the one install line, which must take that argument and no literal.
	dockerfilePnpm     = regexp.MustCompile(`(?m)^ARG PNPM_VERSION=(\S+)$`)
	dockerfileCorepack = regexp.MustCompile(`corepack prepare (\S+) --activate`)
	misePnpm           = regexp.MustCompile(`(?m)^pnpm = "([^"]+)"`)
)

// TestDockerfilePnpmMatchesMise fails when the Dockerfile's web stage installs a
// different pnpm than mise.toml pins. mise.toml says the two must match: the lockfile the
// image reads with --frozen-lockfile has to come from the same pnpm a local install uses.
func TestDockerfilePnpmMatchesMise(t *testing.T) {
	t.Parallel()

	docker, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	mise, err := os.ReadFile("../../mise.toml")
	if err != nil {
		t.Fatal(err)
	}

	d := dockerfilePnpm.FindStringSubmatch(string(docker))
	if d == nil {
		t.Fatal("Dockerfile has no 'ARG PNPM_VERSION=<version>' line")
	}
	installs := dockerfileCorepack.FindAllStringSubmatch(string(docker), -1)
	if len(installs) != 1 || installs[0][1] != `"pnpm@${PNPM_VERSION}"` {
		t.Fatalf(`Dockerfile must install pnpm once, as corepack prepare "pnpm@${PNPM_VERSION}" --activate, got %q`, installs)
	}
	m := misePnpm.FindStringSubmatch(string(mise))
	if m == nil {
		t.Fatal(`mise.toml has no 'pnpm = "<version>"' line`)
	}
	if d[1] != m[1] {
		t.Fatalf("Dockerfile installs pnpm %s but mise.toml pins pnpm %s; they must match", d[1], m[1])
	}
}
