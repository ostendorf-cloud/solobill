package pdf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
)

var update = flag.Bool("update", false, "rewrite the fixtures and golden files from the current code")

func writeOrCompare(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (run go test ./internal/pdf -update to create it): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the generated output (run go test ./internal/pdf -update and review the diff)\ngot:\n%s", path, got)
	}
}

// renderCase is one golden render: a fixture, issued or as a draft.
type renderCase struct {
	name    string
	fixture string
	draft   bool
}

func renderCases() []renderCase {
	cases := make([]renderCase, 0, len(fixtures)+1)
	for _, f := range fixtures {
		cases = append(cases, renderCase{name: f.name, fixture: f.name})
	}

	return append(cases, renderCase{name: "offer-de-draft", fixture: "offer-de", draft: true})
}

// TestFixtures keeps the payload fixtures in step with internal/money
// and proves each one is a valid frozen payload.
func TestFixtures(t *testing.T) {
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			p := f.build(t)
			if problems := docpayload.Validate(p); len(problems) > 0 {
				t.Fatalf("fixture is not a valid payload: %v", problems)
			}
			if err := CheckPrintable(p); err != nil {
				t.Fatalf("fixture is not printable: %v", err)
			}
			data, err := docpayload.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			writeOrCompare(t, fixturePath(f.name), indentJSON(t, data))
		})
	}
}

// TestGolden compares the data file Typst reads, which holds every text
// the PDF prints, with a golden file per case.
func TestGolden(t *testing.T) {
	for _, c := range renderCases() {
		t.Run(c.name, func(t *testing.T) {
			src, err := BuildSource(loadFixture(t, c.fixture), c.draft)
			if err != nil {
				t.Fatal(err)
			}
			writeOrCompare(t, goldenPath(c.name), src.Data)
		})
	}
}

// templateHashes pins every shipped template version. A stored payload
// names its version forever, so a shipped template never changes.
var templateHashes = map[string]string{
	"document-v1": "4fdf18d4d1145ae9444a533cad7a41ac5a1dd7fb42373f6aa40b20a9c6d3f10e",
}

func TestTemplateHashes(t *testing.T) {
	for version, file := range templates {
		b, err := assets.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		got := hex.EncodeToString(sum[:])
		want, ok := templateHashes[version]
		if !ok {
			t.Errorf("template %s has no pinned hash: add %q to templateHashes", version, got)

			continue
		}
		if got != want {
			t.Errorf("template %s changed (sha256 %s, pinned %s). A shipped template version never changes: "+
				"add the change as a new version (document-v2) and keep %s as it is", version, got, want, version)
		}
	}
}

// renderAll renders every case once with the real Typst binary.
func renderAll(t *testing.T) map[string][]byte {
	t.Helper()
	bin := typstOrSkip(t)
	r := New(Options{Binary: bin, TempDir: t.TempDir()})
	out := map[string][]byte{}
	for _, c := range renderCases() {
		payload, err := os.ReadFile(fixturePath(c.fixture))
		if err != nil {
			t.Fatal(err)
		}
		pdf, err := r.Render(context.Background(), payload, c.draft)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		out[c.name] = pdf
	}

	return out
}

// TestTextGolden compares the text pdftotext extracts in layout mode
// with a golden file per case: what a reader sees, not only the data.
func TestTextGolden(t *testing.T) {
	pdftotext := toolOrSkip(t, "pdftotext")
	pdfs := renderAll(t)
	dir := t.TempDir()
	for _, c := range renderCases() {
		t.Run(c.name, func(t *testing.T) {
			in := filepath.Join(dir, c.name+".pdf")
			if err := os.WriteFile(in, pdfs[c.name], 0o600); err != nil {
				t.Fatal(err)
			}
			text, err := exec.CommandContext(context.Background(), pdftotext, "-layout", "-enc", "UTF-8", in, "-").Output()
			if err != nil {
				t.Fatal(err)
			}
			writeOrCompare(t, filepath.Join("testdata", "golden", c.name+".txt"), text)
		})
	}
}

// TestPDFHashes pins the bytes of every case to the Typst version, so a
// Typst bump, a font change or a layout change shows as a diff of
// testdata/golden/pdf-sha256.txt.
func TestPDFHashes(t *testing.T) {
	pdfs := renderAll(t)
	version, err := exec.CommandContext(context.Background(), typstOrSkip(t), "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", strings.TrimSpace(string(version)))
	for _, c := range renderCases() {
		sum := sha256.Sum256(pdfs[c.name])
		fmt.Fprintf(&b, "%s  %s.pdf\n", hex.EncodeToString(sum[:]), c.name)
	}
	writeOrCompare(t, filepath.Join("testdata", "golden", "pdf-sha256.txt"), []byte(b.String()))
}

func loadFixture(t *testing.T, name string) docpayload.FrozenPayloadV1 {
	t.Helper()
	data, err := os.ReadFile(fixturePath(name))
	if err != nil {
		t.Fatal(err)
	}
	p, err := docpayload.Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}

	return p
}
