package pdf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/httpapi"
)

// Renderer is the documents API's renderer.
var _ httpapi.DocumentRenderer = (*Renderer)(nil)

// hostile are customer and line texts that would execute, break the
// document or read a file if they reached Typst as markup.
var hostile = []string{
	`#panic("injected")`,
	`#read("/etc/passwd")`,
	`#import "@preview/injected:0.1.0": *`,
	`$ unclosed math`,
	`back\slash \ and #`,
	`] #set page(fill: red) [`,
	`<label> @nolabel`,
	"`raw` *strong* _emph_ = heading",
	`"); panic("x"); ("`,
}

func hostilePayload(t *testing.T) docpayload.FrozenPayloadV1 {
	t.Helper()
	p := loadFixture(t, "offer-de")
	p.Customer.Name = hostile[0]
	p.Customer.Address.Street = hostile[1]
	p.Customer.Address.Line2 = hostile[2]
	p.CustomerReference = hostile[3]
	p.Customer.Number = hostile[4]
	p.Lines[0].Description = hostile[5] + "\n" + hostile[6]
	p.Lines[0].Unit = hostile[7]
	p.Notes = hostile[8]

	return p
}

// TestInjectionStaysData proves every hostile string reaches the data
// file as an unchanged JSON string value.
func TestInjectionStaysData(t *testing.T) {
	src, err := BuildSource(hostilePayload(t), false)
	if err != nil {
		t.Fatal(err)
	}
	var in Input
	if err := json.Unmarshal(src.Data, &in); err != nil {
		t.Fatalf("data file is not JSON: %v", err)
	}
	all := strings.Join(in.RecipientLines, "\n") + "\n" + in.Lines[0].Unit + "\n" +
		strings.Join(in.Lines[0].Description, "\n") + "\n" + in.Sections[0].Lines[0]
	for _, m := range in.Meta {
		all += "\n" + m.Value
	}
	for _, h := range hostile {
		if !strings.Contains(all, h) {
			t.Errorf("hostile text %q is not in the data unchanged", h)
		}
	}
	// The template never evaluates data as code or reads a path from it.
	for i, line := range strings.Split(string(src.Template), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		for _, call := range []string{"eval(", "read(", "include", "image(", "plugin("} {
			if strings.Contains(line, call) {
				t.Errorf("template line %d calls %s: %s", i+1, call, line)
			}
		}
		if strings.Contains(line, "json(") && !strings.Contains(line, `json("data.json")`) {
			t.Errorf("template line %d reads JSON from elsewhere: %s", i+1, line)
		}
	}
}

func typstOrSkip(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("typst")
	if err != nil {
		t.Skip("typst is not on PATH: install it with mise (mise.toml pins it) to run the compile tests")
	}

	return bin
}

// TestCompile renders every fixture with the real Typst binary and
// checks the output is a reproducible PDF/A-3b. When SOLOBILL_PDF_OUT is
// set, the PDFs are written there for a visual check.
func TestCompile(t *testing.T) {
	bin := typstOrSkip(t)
	r := New(Options{Binary: bin, TempDir: t.TempDir()})
	outDir := os.Getenv("SOLOBILL_PDF_OUT")

	cases := []struct {
		name    string
		fixture string
		draft   bool
	}{
		{"invoice-standard-de", "invoice-standard-de", false},
		{"invoice-kleinunternehmer-de", "invoice-kleinunternehmer-de", false},
		{"invoice-line-discount-en", "invoice-line-discount-en", false},
		{"offer-de", "offer-de", false},
		{"offer-de-draft", "offer-de", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			payload, err := os.ReadFile(fixturePath(c.fixture))
			if err != nil {
				t.Fatal(err)
			}
			pdf, err := r.Render(context.Background(), payload, c.draft)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
				t.Fatalf("output is no PDF: %q", pdf[:min(len(pdf), 16)])
			}
			if !bytes.Contains(pdf, []byte("<pdfaid:part>3</pdfaid:part>")) {
				t.Error("output does not declare PDF/A-3")
			}
			again, err := r.Render(context.Background(), payload, c.draft)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(pdf, again) {
				t.Error("two renders of the same payload differ")
			}
			if outDir != "" {
				if err := os.WriteFile(filepath.Join(outDir, c.name+".pdf"), pdf, 0o600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// TestCompileInjection renders the hostile payload. If any string were
// evaluated, #panic would abort, the unclosed $ would be a syntax error
// and the missing reference would fail the compile.
func TestCompileInjection(t *testing.T) {
	bin := typstOrSkip(t)
	r := New(Options{Binary: bin, TempDir: t.TempDir()})
	pdf, err := r.RenderPayload(context.Background(), hostilePayload(t), false)
	if err != nil {
		t.Fatalf("hostile payload did not render as text: %v", err)
	}
	if outDir := os.Getenv("SOLOBILL_PDF_OUT"); outDir != "" {
		if err := os.WriteFile(filepath.Join(outDir, "hostile.pdf"), pdf, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// The text check needs pdftotext; the compile above already proves
	// nothing executed.
	pdftotext, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Log("pdftotext not on PATH: the printed text is not compared")

		return
	}
	in := filepath.Join(t.TempDir(), "hostile.pdf")
	if err := os.WriteFile(in, pdf, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(context.Background(), pdftotext, "-raw", in, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(out)), " ")
	for _, h := range hostile {
		if !strings.Contains(text, strings.Join(strings.Fields(h), " ")) {
			t.Errorf("printed text lacks %q", h)
		}
	}
}

// fakeTypst writes a shell script standing in for typst.
func fakeTypst(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "typst")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	return path
}

func offerPayload(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(fixturePath("offer-de"))
	if err != nil {
		t.Fatal(err)
	}

	return b
}

func TestRenderTimeout(t *testing.T) {
	bin := fakeTypst(t, "exec sleep 10")
	r := New(Options{Binary: bin, Timeout: 200 * time.Millisecond, TempDir: t.TempDir()})
	start := time.Now()
	_, err := r.Render(context.Background(), offerPayload(t), false)
	if !errors.Is(err, ErrCompile) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want ErrCompile with a deadline", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("render took %s, the timeout did not stop typst", time.Since(start))
	}
}

func TestRenderOutputTooLarge(t *testing.T) {
	// The last argument is the output path.
	bin := fakeTypst(t, `for last; do :; done; head -c 4096 /dev/zero > "$last"`)
	r := New(Options{Binary: bin, MaxOutputBytes: 1024, TempDir: t.TempDir()})
	if _, err := r.Render(context.Background(), offerPayload(t), false); !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("err = %v, want ErrOutputTooLarge", err)
	}
}

func TestRenderCompileError(t *testing.T) {
	bin := fakeTypst(t, `echo "error: boom" >&2; exit 1`)
	r := New(Options{Binary: bin, TempDir: t.TempDir()})
	_, err := r.Render(context.Background(), offerPayload(t), false)
	if !errors.Is(err, ErrCompile) || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want ErrCompile with the diagnostics", err)
	}
}

// TestRenderArguments pins the isolation flags typst is called with.
func TestRenderArguments(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	bin := fakeTypst(t, `printf '%s\n' "$@" > "`+argsFile+`"; env > "`+argsFile+`.env"; for last; do :; done; printf '%%PDF-1.7' > "$last"`)
	r := New(Options{Binary: bin, TempDir: t.TempDir()})
	if _, err := r.Render(context.Background(), offerPayload(t), false); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	joined := strings.Join(args, " ")
	for _, want := range []string{"--ignore-system-fonts", "--ignore-embedded-fonts", "--pdf-standard a-3b", "--creation-timestamp 1795132800"} {
		if !strings.Contains(joined, want) {
			t.Errorf("typst arguments lack %q: %s", want, joined)
		}
	}
	root := ""
	for i, a := range args {
		if a == "--root" && i+1 < len(args) {
			root = args[i+1]
		}
	}
	if root == "" {
		t.Fatal("no --root")
	}
	for i, a := range args {
		if strings.HasPrefix(a, "/") && !strings.HasPrefix(a, root) && (i == 0 || args[i-1] != "--root") {
			t.Errorf("argument %q is outside the root %s", a, root)
		}
	}
	env, err := os.ReadFile(argsFile + ".env")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(env), "TYPST_") {
		t.Errorf("typst inherits a TYPST_ variable:\n%s", env)
	}
}

func TestRenderUnavailable(t *testing.T) {
	r := New(Options{Binary: filepath.Join(t.TempDir(), "no-typst")})
	if _, err := r.Render(context.Background(), offerPayload(t), false); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestBuildInputRefuses(t *testing.T) {
	base := loadFixture(t, "offer-de")

	imported := base
	imported.Imported = true
	if _, err := BuildInput(imported, false); !errors.Is(err, ErrImported) {
		t.Errorf("imported: err = %v", err)
	}

	unknown := base
	unknown.TemplateVersion = "document-v0"
	if _, err := BuildInput(unknown, false); !errors.Is(err, ErrUnknownTemplate) {
		t.Errorf("unknown template: err = %v", err)
	}

	lang := base
	lang.Language = "fr"
	if _, err := BuildInput(lang, false); !errors.Is(err, ErrUnsupported) {
		t.Errorf("language: err = %v", err)
	}

	if _, err := New(Options{}).Render(context.Background(), []byte(`{"version":2}`), false); !errors.Is(err, docpayload.ErrUnsupportedVersion) {
		t.Errorf("version 2: err = %v", err)
	}
}

func TestDraft(t *testing.T) {
	p := loadFixture(t, "offer-de")
	p.Number = ""
	p.NumberValue = 0
	p.NumberYear = 0
	p.TemplateVersion = ""
	in, err := BuildInput(p, true)
	if err != nil {
		t.Fatal(err)
	}
	if !in.Draft || in.Watermark != "Entwurf" || in.TemplateVersion != CurrentTemplateVersion {
		t.Errorf("draft input = %+v", in)
	}
	for _, m := range in.Meta {
		if m.Label == "Angebotsnummer" {
			t.Errorf("draft prints a number: %+v", m)
		}
	}

	// An issued document prints its number.
	issued := loadFixture(t, "offer-de")
	in, err = BuildInput(issued, false)
	if err != nil {
		t.Fatal(err)
	}
	if in.Meta[0].Value != "AN-20260012" {
		t.Errorf("issued meta = %+v", in.Meta)
	}

	// A draft missing data the template needs is refused with details.
	p.Customer.Name = ""
	var invalid *InvalidPayloadError
	if _, err := BuildInput(p, true); !errors.As(err, &invalid) || invalid.Details["customer.name"] == "" {
		t.Errorf("err = %v, want InvalidPayloadError naming customer.name", err)
	}
}

func TestCorrection(t *testing.T) {
	p := loadFixture(t, "invoice-line-discount-en")
	p.Type = docpayload.TypeCorrection
	p.CorrectionSeq = 1
	p.Number = "RE-20260043-1"
	p.Corrects = &docpayload.DocumentReference{ID: "x", Number: "RE-20260043", IssueDate: "2026-12-07"}
	in, err := BuildInput(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if in.Title != "Corrected invoice" || in.Meta[0].Value != "RE-20260043-1" {
		t.Errorf("title %q, meta %+v", in.Title, in.Meta)
	}
	if want := "This corrected invoice replaces invoice RE-20260043 of 7 December 2026."; in.CorrectsLine != want {
		t.Errorf("corrects line %q, want %q", in.CorrectsLine, want)
	}
}

func TestFontLicenseEmbedded(t *testing.T) {
	if !bytes.Contains(FontLicense(), []byte("The Go Authors")) {
		t.Error("the Go font licence is not embedded")
	}
}
