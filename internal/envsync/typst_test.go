// Package envsync checks that the files which describe the build environment of solobill
// stay in step with each other.
package envsync

import (
	"os"
	"regexp"
	"testing"
)

var (
	// dockerfileTypst is the version argument Renovate updates. dockerfileTypstURL is the
	// one download, which must take that argument and no literal version.
	dockerfileTypst    = regexp.MustCompile(`(?m)^ARG TYPST_VERSION=(\S+)$`)
	dockerfileTypstURL = regexp.MustCompile(`https://github\.com/typst/typst/releases/download/([^/]+)/`)
	// dockerfileTypstSum is a pinned checksum argument: 64 lowercase hex digits, never empty.
	dockerfileTypstSum = regexp.MustCompile(`(?m)^ARG TYPST_SHA256_(AMD64|ARM64)=([0-9a-f]{64})$`)
	miseTypst          = regexp.MustCompile(`(?m)^typst = "([^"]+)"`)
)

// TestDockerfileTypstMatchesMise fails when the image's typst differs from the one mise.toml
// pins for the tests. An issued document is rendered from its frozen payload and must look
// the same in the image as in the golden tests, so both must run the same typst.
func TestDockerfileTypstMatchesMise(t *testing.T) {
	t.Parallel()

	docker, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	mise, err := os.ReadFile("../../mise.toml")
	if err != nil {
		t.Fatal(err)
	}

	d := dockerfileTypst.FindAllStringSubmatch(string(docker), -1)
	if len(d) != 1 {
		t.Fatalf("Dockerfile must have exactly one 'ARG TYPST_VERSION=<version>' line, got %q", d)
	}
	urls := dockerfileTypstURL.FindAllStringSubmatch(string(docker), -1)
	if len(urls) != 1 || urls[0][1] != "v${TYPST_VERSION}" {
		t.Fatalf("Dockerfile must download typst once, from the release tag v${TYPST_VERSION}, got %q", urls)
	}
	sums := dockerfileTypstSum.FindAllStringSubmatch(string(docker), -1)
	if len(sums) != 2 || sums[0][1] == sums[1][1] {
		t.Fatalf("Dockerfile must pin TYPST_SHA256_AMD64 and TYPST_SHA256_ARM64 to a SHA-256 each, got %q", sums)
	}
	m := miseTypst.FindStringSubmatch(string(mise))
	if m == nil {
		t.Fatal(`mise.toml has no 'typst = "<version>"' line`)
	}
	if d[0][1] != m[1] {
		t.Fatalf("Dockerfile installs typst %s but mise.toml pins typst %s; they must match", d[0][1], m[1])
	}
}
