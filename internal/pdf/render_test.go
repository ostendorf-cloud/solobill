package pdf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/httpapi"
)

// zeroWidthSpace is U+200B, written as a rune value so the source holds
// no invisible character.
const zeroWidthSpace rune = 0x200B

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
	p.Customer.VATID = hostile[7]
	p.Lines[0].Unit = `#u $ \`
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
}

// fileAccess matches every Typst name that reads a file, embeds one or
// evaluates code, whether it is called or only referenced: a reference
// is how an alias such as "#let rd = read" starts. It also matches the
// raw element's file parameters.
var fileAccess = regexp.MustCompile(
	`\b(?:json|yaml|csv|toml|xml|cbor|read|image|plugin|eval|bytes|bibliography|embed|include|import)\b(?:\s*\("[^"]*"\))?` +
		`|\b(?:syntaxes|theme)\s*:`)

// allowedFileAccess is every file access a template version may make, in
// order: data.json, and from document-v2 the logo under the fixed name
// only internal/pdf writes.
var allowedFileAccess = map[string][]string{
	"document-v1": {`json("data.json")`},
	"document-v2": {`json("data.json")`, `image("logo")`},
}

// TestTemplateFileAccess allows exactly the file accesses of
// allowedFileAccess, each with a fixed literal path. Any other
// occurrence of a file access name, called or bound to another name,
// fails, so no data value can ever name a path or become code. This is a
// second line: the real guard is the SHA-256 pin of every shipped
// template in TestTemplateHashes, which a review of any template change
// has to pass.
func TestTemplateFileAccess(t *testing.T) {
	for version, file := range templates {
		b, err := assets.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		found := fileAccessIn(string(b))
		if want := allowedFileAccess[version]; !slices.Equal(found, want) {
			t.Errorf("template %s: file access %q, want exactly %q", version, found, want)
		}
	}
}

func fileAccessIn(src string) []string {
	var found []string
	for _, line := range strings.Split(src, "\n") {
		code, _, _ := strings.Cut(line, "//")
		found = append(found, fileAccess.FindAllString(code, -1)...)
	}

	return found
}

// TestFileAccessGuard pins what the guard catches.
func TestFileAccessGuard(t *testing.T) {
	for _, src := range []string{
		`#let y = yaml("data.json")`,
		`#let rd = read`,
		`#rd("main.typ")` + "\n" + `#let rd = read`,
		`#pdf.embed("data.json")`,
		`#bibliography("refs.bib")`,
		`#raw("x", syntaxes: "a.sublime-syntax")`,
		`#raw("x", theme: "t.tmTheme")`,
		`#image("logo.png")`,
		`#include "other.typ"`,
		`#import "@preview/x:0.1.0": *`,
		`#eval("1")`,
		`#let j = json`,
	} {
		if found := fileAccessIn(src); len(found) == 0 {
			t.Errorf("guard misses %q", src)
		}
	}
}

// toolOrSkip finds a tool on PATH. Without it the test skips, except in
// CI (CI=true), where a missing tool fails the test: mise.toml pins
// typst and poppler, so CI always has both.
func toolOrSkip(t *testing.T, name string) string {
	t.Helper()
	bin, err := exec.LookPath(name)
	if err != nil {
		if os.Getenv("CI") == "true" {
			t.Fatalf("%s is not on PATH in CI: mise.toml pins it", name)
		}
		t.Skipf("%s is not on PATH: install it with mise (mise.toml pins it) to run this test", name)
	}

	return bin
}

func typstOrSkip(t *testing.T) string {
	t.Helper()

	return toolOrSkip(t, "typst")
}

// TestCompile renders every case with the real Typst binary and checks
// the output is a reproducible PDF/A-3b naming its template version.
// When SOLOBILL_PDF_OUT is set, the PDFs are written there for a
// visual check.
func TestCompile(t *testing.T) {
	bin := typstOrSkip(t)
	r := New(Options{Binary: bin, TempDir: t.TempDir()})
	if err := r.CheckVersion(context.Background()); err != nil {
		t.Fatal(err)
	}
	outDir := os.Getenv("SOLOBILL_PDF_OUT")

	for _, c := range renderCases() {
		t.Run(c.name, func(t *testing.T) {
			payload, err := os.ReadFile(fixturePath(c.fixture))
			if err != nil {
				t.Fatal(err)
			}
			pdf, err := r.RenderWithLogo(context.Background(), payload, c.draft, c.logoOf(t))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
				t.Fatalf("output is no PDF: %q", pdf[:min(len(pdf), 16)])
			}
			if !bytes.Contains(pdf, []byte("<pdfaid:part>3</pdfaid:part>")) {
				t.Error("output does not declare PDF/A-3")
			}
			version := loadFixture(t, c.fixture).TemplateVersion
			if !bytes.Contains(pdf, []byte(version)) {
				t.Errorf("metadata does not name %s", version)
			}
			again, err := r.RenderWithLogo(context.Background(), payload, c.draft, c.logoOf(t))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(pdf, again) {
				t.Error("two renders of the same payload differ")
			}
			writeOut(t, outDir, c.name, pdf)
		})
	}
}

func writeOut(t *testing.T, dir, name string, pdf []byte) {
	t.Helper()
	if dir == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, name+".pdf"), pdf, 0o600); err != nil {
		t.Fatal(err)
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
	writeOut(t, os.Getenv("SOLOBILL_PDF_OUT"), "hostile", pdf)

	// The compile above already proves nothing executed; the text check
	// proves every string printed literally.
	pdftotext := toolOrSkip(t, "pdftotext")
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

// token is n characters without a space.
func token(n int) string { return strings.Repeat("ABCDEFGHIJ", n/10+1)[:n] }

// TestLongTokens refuses tokens beyond the limits and renders the
// longest allowed ones. The PDFs are written to SOLOBILL_PDF_OUT for a
// visual check; here the render must succeed and keep the text.
func TestLongTokens(t *testing.T) {
	lim := documentV1Limits
	long := token(5000)
	p := loadFixture(t, "invoice-standard-de")
	p.Lines = p.Lines[28:]

	// A token wider than its cell is refused with its field, whatever its
	// length.
	refused := p
	refused.Customer.Name = long
	refused.Seller.IBAN = long
	refused.Seller.FooterText = long
	refused.Lines = slices.Clone(p.Lines)
	refused.Lines[0].Unit = token(lim.UnitChars + 10)
	refused.Lines[1].Description = token(60)
	refused.Notes = "Wort " + wide(t, cellFullText, 1)
	refused.Seller.Email = wide(t, cellFooter, 1)
	var invalid *InvalidPayloadError
	if err := CheckPrintable(refused); !errors.As(err, &invalid) {
		t.Fatalf("CheckPrintable = %v, want *InvalidPayloadError", err)
	}
	for _, path := range []string{
		"customer.name", "seller.iban", "seller.footer_text", "lines[0].unit",
		"lines[1].description", "notes", "seller.email",
	} {
		if invalid.Details[path] == "" {
			t.Errorf("%s is not refused: %v", path, invalid.Details)
		}
	}

	// The widest allowed words must stay inside their cells.
	bin := typstOrSkip(t)
	r := New(Options{Binary: bin, TempDir: t.TempDir()})
	p.Lines[0].Description = wideWords(t, cellDescription(true), lim.TextChars)
	p.Lines[1].Unit = wide(t, cellUnit, 0)
	p.Customer.Name = wideWords(t, cellAddress, lim.LineChars)
	p.Seller.LegalName = wideWords(t, cellFooter, lim.SellerChars)
	p.Seller.Email = wide(t, cellFooter, 0)
	p.Seller.FooterText = wideWords(t, cellFooterText, lim.FooterTextChars)
	p.CustomerReference = wideWords(t, cellMetaValue, lim.LineChars)
	if err := CheckPrintable(p); err != nil {
		t.Fatal(err)
	}
	// Rendered as issued: the draft check would recompute the totals of
	// the shortened lines, and only the layout matters here.
	pdf, err := r.RenderPayload(context.Background(), p, false)
	if err != nil {
		t.Fatal(err)
	}
	writeOut(t, os.Getenv("SOLOBILL_PDF_OUT"), "long-tokens", pdf)
	assertNoOverlap(t, toolOrSkip(t, "pdftotext"), pdf)
}

// unprintable are characters the bundled Go fonts cannot show, or that
// are refused on purpose. Typst fails on each under PDF/A-3b.
var unprintable = []struct {
	name string
	s    string
}{
	{"cjk", "Müller 中文"},
	{"emoji", "Max \U0001F600"},
	{"arabic", "مرحبا"},
	{"capital sharp s", "STRAẞE"},
	{"non-breaking hyphen", "IT‑Beratung"},
	{"check mark", "erledigt ✓"},
	{"bell", "a\x07b"},
	{"escape", "a\x1bb"},
	{"right to left override", "IBAN " + string(rune(0x202E)) + "0000"},
	{"left to right isolate", "a" + string(rune(0x2066)) + "b"},
	{"zero width space", "a" + string(zeroWidthSpace) + "b"},
}

func TestCheckPrintableRefuses(t *testing.T) {
	base := loadFixture(t, "invoice-standard-de")
	for _, c := range unprintable {
		t.Run(c.name, func(t *testing.T) {
			p := base
			p.Customer.Name = c.s
			var invalid *InvalidPayloadError
			if err := CheckPrintable(p); !errors.As(err, &invalid) || invalid.Details["customer.name"] == "" {
				t.Fatalf("CheckPrintable = %v, want customer.name refused", err)
			}
			if _, err := BuildInput(p, false); !errors.As(err, &invalid) {
				t.Fatalf("BuildInput = %v, want *InvalidPayloadError", err)
			}

			p = base
			p.Lines = slices.Clone(base.Lines)
			p.Lines[3].Description = "ok\n" + c.s
			if err := CheckPrintable(p); !errors.As(err, &invalid) || invalid.Details["lines[3].description"] == "" {
				t.Fatalf("CheckPrintable = %v, want lines[3].description refused", err)
			}
		})
	}
}

func TestCheckPrintableControlCharacters(t *testing.T) {
	p := loadFixture(t, "invoice-standard-de")
	p.Notes = "Zeile 1\r\nZeile 2\tmit Tab"
	if err := CheckPrintable(p); err != nil {
		t.Errorf("line feed, carriage return and tab in a free text field: %v", err)
	}
	p.Customer.Name = "Zeile 1\nZeile 2"
	var invalid *InvalidPayloadError
	if err := CheckPrintable(p); !errors.As(err, &invalid) || invalid.Details["customer.name"] == "" {
		t.Errorf("line feed in a single line field: %v", err)
	}
}

// TestPrintableCompiles renders every character class the check accepts
// that the fixtures do not hold, so the check never accepts what Typst
// refuses.
func TestPrintableCompiles(t *testing.T) {
	bin := typstOrSkip(t)
	p := loadFixture(t, "offer-de")
	p.Customer.Name = "ÄÖÜäöüß àéîõç ÆØÅæøå"
	// The pre check joins punctuation with its neighbours, so each group
	// stands on a line of its own.
	p.Notes = "Tab\tdanach\r\nZeile\nŁłŚśŹźŻż ČčŠšŽž\n€£¥ „“ ‚‘ «»\n– — … · • ° ±\n× ÷ § ¶ © ® ™"
	if err := CheckPrintable(p); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{Binary: bin, TempDir: t.TempDir()}).RenderPayload(context.Background(), p, false); err != nil {
		t.Fatal(err)
	}
}

// TestLabelsPrintable checks the template's own texts against the
// bundled fonts.
func TestLabelsPrintable(t *testing.T) {
	pr, err := newPrinter(documentV1Limits)
	if err != nil {
		t.Fatal(err)
	}
	for lang, l := range labelsByLanguage {
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		pr.check(lang, string(b), false)
	}
	pr.check("separators", " · "+string(noBreakSpace)+"%", false)
	if len(pr.problems) > 0 {
		t.Errorf("labels cannot be printed: %v", pr.problems)
	}
}

func withLines(p docpayload.FrozenPayloadV1, n int, desc string) docpayload.FrozenPayloadV1 {
	line := p.Lines[len(p.Lines)-1]
	p.Lines = make([]docpayload.Line, n)
	for i := range p.Lines {
		p.Lines[i] = line
		p.Lines[i].Position = int32(i + 1)
		p.Lines[i].Description = desc
	}

	return p
}

func TestCaps(t *testing.T) {
	lim := documentV1Limits
	base := loadFixture(t, "offer-de")
	cases := []struct {
		name string
		p    docpayload.FrozenPayloadV1
		path string
	}{
		{"too many lines", withLines(base, lim.Lines+1, "x"), "lines"},
		// 1000 lines of 20 rows each need far more than MaxPages pages.
		{"too many pages", withLines(base, lim.Lines, strings.TrimSuffix(strings.Repeat("x\n", lim.TextLines), "\n")), "lines"},
		{"too many description lines", withLines(base, 1, strings.Repeat("x\n", lim.TextLines)+"x"), "lines[0].description"},
		{"description too long", withLines(base, 1, strings.Repeat("ab ", lim.TextChars/3+1)), "lines[0].description"},
		{"description word too wide", withLines(base, 1, "ok "+strings.Repeat("W", 40)), "lines[0].description"},
	}
	notes := base
	notes.Notes = strings.Repeat("Zeile\n", lim.TextLines) + "Zeile"
	cases = append(cases, struct {
		name string
		p    docpayload.FrozenPayloadV1
		path string
	}{"too many note lines", notes, "notes"})
	footer := base
	footer.Seller.FooterText = strings.Repeat("Zeile\n", lim.FooterTextLines) + "Zeile"
	cases = append(cases, struct {
		name string
		p    docpayload.FrozenPayloadV1
		path string
	}{"too many footer lines", footer, "seller.footer_text"})

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var invalid *InvalidPayloadError
			if err := CheckPrintable(c.p); !errors.As(err, &invalid) || invalid.Details[c.path] == "" {
				t.Errorf("CheckPrintable = %v, want %s refused", err, c.path)
			}
			if _, err := BuildSource(c.p, false); !errors.As(err, &invalid) || invalid.Details[c.path] == "" {
				t.Errorf("BuildSource = %v, want %s refused", err, c.path)
			}
		})
	}

	// The data limit is a backstop the other limits keep out of reach; it
	// is tested with a tiny limit.
	tiny := documentV1Limits
	tiny.DataBytes = 100
	pr, err := newPrinter(tiny)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := finish(base, labelsByLanguage["de"], false, CurrentTemplateVersion, pr); !errors.Is(err, ErrPayloadTooLarge) {
		t.Errorf("data limit: %v, want ErrPayloadTooLarge", err)
	}
}

// pagesOf reads a PDF's page count.
func pagesOf(t *testing.T, pdf []byte) int {
	t.Helper()
	n, err := pageCount(pdf)
	if err != nil {
		t.Fatal(err)
	}

	return n
}

// TestWorstCaseRender renders the largest payloads document-v1's limits
// allow: for each kind of description, as many lines as CheckPrintable
// accepts under MaxPages, with every seller field and note at its limit.
// Each must render to at most MaxPages pages, at most the estimated
// pages, and below 5 MiB. Skipped with -short.
func TestWorstCaseRender(t *testing.T) {
	if testing.Short() {
		t.Skip("renders up to MaxPages pages per case")
	}
	bin := typstOrSkip(t)
	lim := documentV1Limits
	r := New(Options{Binary: bin, TempDir: t.TempDir(), Timeout: 5 * time.Minute})
	base := maxSeller(t, loadFixture(t, "invoice-standard-de"))
	base.CustomerReference = wideWords(t, cellMetaValue, lim.LineChars)
	base.Customer.Name = wideWords(t, cellAddress, lim.LineChars)
	base.TaxNote = maxNote(t)
	base.PaymentTerms = maxNote(t)
	base.Notes = maxNote(t)

	cases := map[string]string{
		// Short lines: the most rows for the fewest bytes.
		"newlines": strings.TrimSuffix(strings.Repeat("x\n", lim.TextLines), "\n"),
		// Dense text: the most bytes a row.
		"dense": wideWords(t, cellDescription(true), lim.TextChars),
		// One short line: the most item lines.
		"short": "x",
	}
	for name, desc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// The most lines CheckPrintable accepts.
			lo, hi := 1, lim.Lines
			for lo < hi {
				mid := (lo + hi + 1) / 2
				if CheckPrintable(withLines(base, mid, desc)) == nil {
					lo = mid
				} else {
					hi = mid - 1
				}
			}
			p := withLines(base, lo, desc)
			if err := CheckPrintable(p); err != nil {
				t.Fatal(err)
			}
			src, err := BuildSource(p, false)
			if err != nil {
				t.Fatal(err)
			}
			var in Input
			if err := json.Unmarshal(src.Data, &in); err != nil {
				t.Fatal(err)
			}
			m, err := loadMetrics()
			if err != nil {
				t.Fatal(err)
			}
			estimate, _ := m.estimatePages(in)
			pdf, err := r.RenderPayload(context.Background(), p, false)
			if err != nil {
				t.Fatal(err)
			}
			pages := pagesOf(t, pdf)
			t.Logf("%s: %d lines, %d bytes, %d pages, estimated %d", name, lo, len(pdf), pages, estimate)
			if len(pdf) > 5<<20 {
				t.Errorf("PDF is %d bytes, over 5 MiB", len(pdf))
			}
			if pages > MaxPages || pages > estimate {
				t.Errorf("PDF has %d pages, estimated %d, limit %d", pages, estimate, MaxPages)
			}
			writeOut(t, os.Getenv("SOLOBILL_PDF_OUT"), "worst-"+name, pdf)
		})
	}
}

// maxNote is a free text note at its limits: the most lines and
// characters, in the widest words its cell takes.
func maxNote(t *testing.T) string {
	t.Helper()
	lim := documentV1Limits
	line := wideWords(t, cellFullText, lim.TextChars/lim.TextLines)
	lines := make([]string, lim.TextLines)
	for i := range lines {
		lines[i] = line
	}

	return strings.Join(lines, "\n")[:lim.TextChars]
}

// maxSeller sets every seller field to its limit, each with a marker the
// footer tests look for.
func maxSeller(t *testing.T, p docpayload.FrozenPayloadV1) docpayload.FrozenPayloadV1 {
	t.Helper()
	lim := documentV1Limits
	field := func(marker string) string {
		return marker + " " + wideWords(t, cellFooter, lim.SellerChars-len(marker)-1)
	}
	s := &p.Seller
	s.LegalName = field("MKNAME")
	s.Address.Street = field("MKSTREET")
	s.Address.Line2 = field("MKLINE2")
	s.Address.PostalCode = field("MKPOSTAL")
	s.Address.City = field("MKCITY")
	s.Phone = field("MKPHONE")
	s.Email = field("MKEMAIL")
	s.Website = field("MKWEB")
	s.TaxNumber = field("MKTAXNO")
	s.VATID = field("MKVATID")
	s.KleinunternehmerID = field("MKKUID")
	s.BankName = field("MKBANK")
	s.IBAN = "DE00" + strings.Repeat("0", lim.SellerChars-8) + "9999"
	s.BIC = field("MKBIC")
	ft := make([]string, lim.FooterTextLines)
	for i := range ft {
		ft[i] = wideWords(t, cellFooterText, lim.FooterTextChars/lim.FooterTextLines-1)
	}
	ft[0] = "MKFOOT" + ft[0][6:]
	s.FooterText = strings.Join(ft, "\n")

	return p
}

// sellerMarkers are the markers maxSeller sets, each of which the footer
// must print on every page.
var sellerMarkers = []string{
	"MKNAME", "MKSTREET", "MKLINE2", "MKPOSTAL", "MKCITY", "MKPHONE", "MKEMAIL", "MKWEB",
	"MKTAXNO", "MKVATID", "MKKUID", "MKBANK", "9999", "MKBIC", "MKFOOT",
}

// TestFooterNeverClips renders every seller field at its limit and a
// realistic seller with a 291 character footer text, and checks through
// pdftotext that every seller value is printed on every page.
func TestFooterNeverClips(t *testing.T) {
	bin := typstOrSkip(t)
	pdftotext := toolOrSkip(t, "pdftotext")
	r := New(Options{Binary: bin, TempDir: t.TempDir()})

	maxed := maxSeller(t, loadFixture(t, "invoice-standard-de"))
	realistic := loadFixture(t, "invoice-standard-de")
	realistic.Seller.LegalName = "Mustermann IT-Beratung und Softwareentwicklung"
	realistic.Seller.Address.Line2 = "Hinterhaus, 2. Obergeschoss"
	realistic.Seller.KleinunternehmerID = "DE000000000-00001"
	realistic.Seller.BankName = "Sparkasse Musterstadt-Beispielhausen"
	realistic.Seller.FooterText = "Geschäftsführer: Max Mustermann, Erika Musterfrau · Sitz der Gesellschaft: Musterstadt · " +
		"Registergericht: Amtsgericht Musterstadt HRB 000000 · Bitte geben Sie bei Zahlung stets die Rechnungsnummer an. " +
		"Es gelten unsere Allgemeinen Geschäftsbedingungen in der jeweils gültigen Fassung. ENDMARK"
	if n := len([]rune(realistic.Seller.FooterText)); n != 291 {
		t.Fatalf("realistic footer text has %d characters, want 291", n)
	}
	realisticMarkers := []string{
		"Mustermann IT-Beratung und Softwareentwicklung", "Hinterhaus, 2. Obergeschoss", "DE000000000-00001",
		"Sparkasse Musterstadt-Beispielhausen", "HRB 000000", "ENDMARK", "12/345/67890", "DE000000000",
		"DE00 0000 0000 0000 0000 00", "MUSTDEXXXXX",
	}

	for name, c := range map[string]struct {
		p       docpayload.FrozenPayloadV1
		markers []string
	}{
		"max":       {maxed, sellerMarkers},
		"realistic": {realistic, realisticMarkers},
	} {
		t.Run(name, func(t *testing.T) {
			if err := CheckPrintable(c.p); err != nil {
				t.Fatal(err)
			}
			pdf, err := r.RenderPayload(context.Background(), c.p, false)
			if err != nil {
				t.Fatal(err)
			}
			writeOut(t, os.Getenv("SOLOBILL_PDF_OUT"), "footer-"+name, pdf)
			pages := pagesOf(t, pdf)
			for page := 1; page <= pages; page++ {
				text := pageText(t, pdftotext, pdf, page)
				for _, m := range c.markers {
					if !strings.Contains(text, m) {
						t.Errorf("page %d of %d lacks %q", page, pages, m)
					}
				}
			}
		})
	}
}

// pageText extracts one page's text, line breaks joined into spaces.
func pageText(t *testing.T, pdftotext string, pdf []byte, page int) string {
	t.Helper()
	in := filepath.Join(t.TempDir(), "page.pdf")
	if err := os.WriteFile(in, pdf, 0o600); err != nil {
		t.Fatal(err)
	}
	n := fmt.Sprint(page)
	out, err := exec.CommandContext(context.Background(), pdftotext, "-raw", "-f", n, "-l", n, in, "-").Output()
	if err != nil {
		t.Fatal(err)
	}

	return strings.Join(strings.Fields(string(out)), " ")
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

// TestRenderOutputTooLarge streams an endless output: the cap must stop
// the process long before the timeout.
func TestRenderOutputTooLarge(t *testing.T) {
	bin := fakeTypst(t, `printf '%%PDF-1.7'; exec yes`)
	r := New(Options{Binary: bin, MaxOutputBytes: 1 << 20, Timeout: 20 * time.Second, TempDir: t.TempDir()})
	start := time.Now()
	if _, err := r.Render(context.Background(), offerPayload(t), false); !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("err = %v, want ErrOutputTooLarge", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("render took %s, the cap did not stop typst", time.Since(start))
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

// TestRenderArguments pins the whole typst command line and environment.
func TestRenderArguments(t *testing.T) {
	// A host variable that would add a font path must not reach typst.
	t.Setenv("TYPST_FONT_PATHS", "/injected/fonts")
	t.Setenv("TYPST_ROOT", "/")
	argsFile := filepath.Join(t.TempDir(), "args")
	bin := fakeTypst(t, `printf '%s\n' "$@" > "`+argsFile+`"; env > "`+argsFile+`.env"; printf '%%PDF-1.7 << /Type /Pages /Count 1 >>'`)
	r := New(Options{Binary: bin, TempDir: t.TempDir()})
	if _, err := r.Render(context.Background(), offerPayload(t), false); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(args) < 3 || args[1] != "--root" {
		t.Fatalf("args = %q", args)
	}
	root := args[2]
	want := []string{
		"compile",
		"--root", root,
		"--font-path", filepath.Join(root, "fonts"),
		"--ignore-system-fonts",
		"--ignore-embedded-fonts",
		"--package-path", filepath.Join(root, "packages"),
		"--package-cache-path", filepath.Join(root, "package-cache"),
		"--pdf-standard", "a-3b",
		"--creation-timestamp", "1795132800",
		"--diagnostic-format", "short",
		"--jobs", "1",
		"--format", "pdf",
		filepath.Join(root, "main.typ"),
		"-",
	}
	if !slices.Equal(args, want) {
		t.Errorf("typst arguments\n got %q\nwant %q", args, want)
	}

	envRaw, err := os.ReadFile(argsFile + ".env")
	if err != nil {
		t.Fatal(err)
	}
	var env []string
	for _, kv := range strings.Split(strings.TrimSpace(string(envRaw)), "\n") {
		// The shell itself adds PWD, SHLVL and "_".
		name, _, _ := strings.Cut(kv, "=")
		if name != "PWD" && name != "SHLVL" && name != "_" && name != "OLDPWD" {
			env = append(env, kv)
		}
	}
	slices.Sort(env)
	wantEnv := minimalEnv(root)
	slices.Sort(wantEnv)
	if !slices.Equal(env, wantEnv) {
		t.Errorf("typst environment\n got %q\nwant %q", env, wantEnv)
	}
}

func TestRenderUnavailable(t *testing.T) {
	r := New(Options{Binary: filepath.Join(t.TempDir(), "no-typst")})
	if _, err := r.Render(context.Background(), offerPayload(t), false); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if err := r.CheckVersion(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("CheckVersion = %v, want ErrUnavailable", err)
	}
}

func TestCheckVersion(t *testing.T) {
	ok := New(Options{Binary: fakeTypst(t, `echo "typst `+TypstVersion+` (b33de9de)"`)})
	if err := ok.CheckVersion(context.Background()); err != nil {
		t.Errorf("pinned version: %v", err)
	}
	other := New(Options{Binary: fakeTypst(t, `echo "typst 0.13.1 (8ace67d9)"`)})
	if err := other.CheckVersion(context.Background()); !errors.Is(err, ErrTypstVersion) {
		t.Errorf("other version: %v, want ErrTypstVersion", err)
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
	in, err := BuildInput(loadFixture(t, "correction-de"), false)
	if err != nil {
		t.Fatal(err)
	}
	if in.Title != "Rechnungskorrektur" || in.Meta[0].Value != "RE-20260042-1" {
		t.Errorf("title %q, meta %+v", in.Title, in.Meta)
	}
	if want := "Diese Rechnungskorrektur ersetzt die Rechnung RE-20260042 vom 05.12.2026."; in.CorrectsLine != want {
		t.Errorf("corrects line %q, want %q", in.CorrectsLine, want)
	}
}

func TestFontLicenseEmbedded(t *testing.T) {
	if !bytes.Contains(FontLicense(), []byte("The Go Authors")) {
		t.Error("the Go font licence is not embedded")
	}
}
