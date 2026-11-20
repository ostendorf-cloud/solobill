package pdf

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
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

// TestFixtures keeps the payload fixtures in step with internal/money
// and proves each one is a valid frozen payload.
func TestFixtures(t *testing.T) {
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			p := f.build(t)
			if problems := docpayload.Validate(p); len(problems) > 0 {
				t.Fatalf("fixture is not a valid payload: %v", problems)
			}
			data, err := docpayload.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			buf.Write(indentJSON(t, data))
			writeOrCompare(t, fixturePath(f.name), buf.Bytes())
		})
	}
}

// TestGolden compares the data file Typst reads, which holds every text
// the PDF prints, with a golden file per fixture. The template is fixed
// per version, so the data file is the whole variable part of a render.
func TestGolden(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		draft   bool
	}{
		{name: "invoice-standard-de", fixture: "invoice-standard-de"},
		{name: "invoice-kleinunternehmer-de", fixture: "invoice-kleinunternehmer-de"},
		{name: "invoice-line-discount-en", fixture: "invoice-line-discount-en"},
		{name: "offer-de", fixture: "offer-de"},
		{name: "offer-de-draft", fixture: "offer-de", draft: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := loadFixture(t, c.fixture)
			src, err := BuildSource(p, c.draft)
			if err != nil {
				t.Fatal(err)
			}
			writeOrCompare(t, goldenPath(c.name), src.Data)
		})
	}
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
