package pdf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"golang.org/x/image/font/sfnt"
)

// Typst is the authority on the layout: the template fails the compile
// with "overflow: <path>" when a marked character lies outside its cell.
// These tests prove the check catches every kind of cell, that the Go pre
// check never accepts what Typst refuses, and that an accepted payload
// is never clipped.

// compileData renders src with the data changed by edit, bypassing the
// Go pre check, so only Typst decides.
func compileData(t *testing.T, r *Renderer, src Source, edit func(in *Input)) error {
	t.Helper()
	var in Input
	if err := json.Unmarshal(src.Data, &in); err != nil {
		t.Fatal(err)
	}
	edit(&in)
	data, err := MarshalInput(in)
	if err != nil {
		t.Fatal(err)
	}
	src.Data = data
	_, err = r.compile(context.Background(), src)

	return err
}

// TestTypstRefusesOverflow puts text too wide for its cell straight into
// the template data, past the Go pre check, and expects Typst to fail
// with the cell's field path for every kind of cell.
func TestTypstRefusesOverflow(t *testing.T) {
	r := New(Options{Binary: typstOrSkip(t), TempDir: t.TempDir()})
	src, err := BuildSource(loadFixture(t, "invoice-standard-de"), false)
	if err != nil {
		t.Fatal(err)
	}
	w := strings.Repeat("W", 60)
	cases := []struct {
		name, path string
		edit       func(in *Input)
	}{
		{"quote and bracket", "lines[0].description", func(in *Input) {
			in.Lines[0].Description = []string{"Lieferung „" + w[:14] + "“ (" + w[:14] + " 4711)"}
		}},
		{"long word", "lines[1].description", func(in *Input) { in.Lines[1].Description = []string{w} }},
		{"unit", "lines[0].unit", func(in *Input) { in.Lines[0].Unit = w[:12] }},
		{"amount", "lines[2].net_after_line_discount_cents", func(in *Input) { in.Lines[2].Amount = "99.999.999.999.999,99" }},
		{"discount", "lines[0].line_discount", func(in *Input) { in.Lines[0].Discount = "Rabatt " + w }},
		{"address", "customer.address.city", func(in *Input) { in.RecipientLines[3] = w[:8] + "( )" + w[:30] }},
		{"meta value", "number", func(in *Input) { in.Meta[0].Value = w[:30] }},
		{"footer", "seller.iban", func(in *Input) {
			for i, paths := range in.FooterPaths {
				for j, p := range paths {
					if p == "seller.iban" {
						in.Footer[i][j] = "IBAN " + w
					}
				}
			}
		}},
		{"footer text", "seller.footer_text", func(in *Input) { in.FooterText = []string{w + w + w} }},
		{"letterhead", "seller.legal_name", func(in *Input) { in.SellerName = w + w }},
		{"note", "notes", func(in *Input) { in.Sections[len(in.Sections)-1].Lines = []string{w + w + w} }},
		{"total", "totals.grand_total_cents", func(in *Input) { in.Totals[len(in.Totals)-1].Value = w[:25] }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := compileData(t, r, src, c.edit)
			var invalid *InvalidPayloadError
			if !errors.Is(err, ErrLayoutOverflow) || !errors.As(err, &invalid) || invalid.Details[c.path] == "" {
				t.Fatalf("err = %v, want ErrLayoutOverflow at %s", err, c.path)
			}
		})
	}
	// The unchanged data renders.
	if err := compileData(t, r, src, func(*Input) {}); err != nil {
		t.Fatalf("unchanged data: %v", err)
	}
}

// uncheckedSource builds the template data of p without any check, so
// only Typst decides.
func uncheckedSource(t *testing.T, p docpayload.FrozenPayloadV1) Source {
	t.Helper()
	pr, err := newPrinter(documentV1Limits)
	if err != nil {
		t.Fatal(err)
	}
	in := build(p, labelsByLanguage[p.Language], false, CurrentTemplateVersion, pr)
	data, err := MarshalInput(in)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := assets.ReadFile(templates[CurrentTemplateVersion])
	if err != nil {
		t.Fatal(err)
	}

	return Source{Template: tmpl, Data: data, MaxPages: MaxPages, CreationTime: time.Unix(0, 0).UTC()}
}

// TestReviewClippedPayloads renders, past the pre check, payloads the
// review found clipped under an earlier layout (testdata/overflow, and
// every *.json in SOLOBILL_OVERFLOW_DIR when set). Each must now fail
// in Typst with ErrLayoutOverflow, or render without a clipped word.
func TestReviewClippedPayloads(t *testing.T) {
	bin := typstOrSkip(t)
	pdftotext := toolOrSkip(t, "pdftotext")
	r := New(Options{Binary: bin, TempDir: t.TempDir()})
	files, err := filepath.Glob(filepath.Join("testdata", "overflow", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("SOLOBILL_OVERFLOW_DIR"); dir != "" {
		more, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, more...)
	}
	if len(files) == 0 {
		t.Fatal("no payloads")
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			b, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			p, err := docpayload.Unmarshal(b)
			if err != nil {
				t.Fatal(err)
			}
			src := uncheckedSource(t, p)
			pdf, err := r.compile(context.Background(), src)
			var invalid *InvalidPayloadError
			switch {
			case errors.Is(err, ErrLayoutOverflow) && errors.As(err, &invalid):
				t.Logf("Typst refuses: %v; the pre check: %v", invalid.Details, CheckPrintable(p))
			case err != nil:
				t.Fatal(err)
			default:
				t.Logf("fits the new layout; the pre check: %v", CheckPrintable(p))
				assertNotClipped(t, bin, pdftotext, src, pdf)
			}
		})
	}
}

// TestRealisticQuotes renders descriptions with quotes and brackets: the
// pre check may accept or refuse them, but an accepted one renders
// without overflow and without a clipped word.
func TestRealisticQuotes(t *testing.T) {
	r := New(Options{Binary: typstOrSkip(t), TempDir: t.TempDir()})
	pdftotext := toolOrSkip(t, "pdftotext")
	for name, c := range map[string]struct{ fixture, desc string }{
		"de": {"invoice-standard-de", "Lieferung „Netzwerkkomponenten“ (Bestellnummer 4711)"},
		"en": {"invoice-line-discount-en", "Delivery “network components” (order number 4711)"},
	} {
		t.Run(name, func(t *testing.T) {
			p := loadFixture(t, c.fixture)
			p.Lines = append([]docpayload.Line(nil), p.Lines...)
			p.Lines[0].Description = c.desc
			var invalid *InvalidPayloadError
			if err := CheckPrintable(p); errors.As(err, &invalid) {
				if invalid.Details["lines[0].description"] == "" {
					t.Fatalf("refused without the description's path: %v", invalid.Details)
				}
				t.Logf("refused by the pre check: %v", invalid.Details)

				return
			} else if err != nil {
				t.Fatal(err)
			}
			renderChecked(t, r, pdftotext, "quotes-"+name, p)
		})
	}
}

// coveredNonLetters are the runes both fonts show that are neither
// letters nor digits nor spaces nor controls.
func coveredNonLetters(t *testing.T) []rune {
	t.Helper()
	cov, err := loadCoverage()
	if err != nil {
		t.Fatal(err)
	}
	var buf sfnt.Buffer
	var out []rune
	for r := rune(0x21); r < 0x3000; r++ {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) || unicode.IsControl(r) || isBidiControl(r) {
			continue
		}
		if cov.covers(&buf, r) {
			out = append(out, r)
		}
	}

	return out
}

// TestDifferentialBreaks puts every covered non letter rune into twelve
// space contexts between runs of "W" that fit the description cell alone
// but not together. Whatever the Go pre check accepts must render
// without overflow: Typst may break less than the pre check assumes,
// never the pre check less than Typst.
func TestDifferentialBreaks(t *testing.T) {
	if testing.Short() {
		t.Skip("renders thousands of lines")
	}
	r := New(Options{Binary: typstOrSkip(t), TempDir: t.TempDir()})
	runs := wide(t, cellDescription(true), 0)
	a := runs[:len(runs)*3/5]
	contexts := []string{
		"A %s", "%s A", "A %s A", "A %sA", "A%s A", "A %s  A", "A%s  %sA", "A  %sA", "A%s  A", "A%s %sA",
		// Two contexts the pre check splits: a letter on both sides.
		"A%sB A", "A B%sA",
	}
	base := loadFixture(t, "invoice-standard-de")
	var accepted []string
	total := 0
	for _, r := range coveredNonLetters(t) {
		for _, f := range contexts {
			s := strings.ReplaceAll(fmt.Sprintf(strings.ReplaceAll(f, "%s", "%[1]s"), string(r)), "A", a)
			total++
			if CheckPrintable(withLines(base, 1, s)) == nil {
				accepted = append(accepted, s)
			}
		}
	}
	t.Logf("%d cases, %d accepted by the pre check", total, len(accepted))
	if len(accepted) == 0 {
		t.Fatal("no case accepted: the test proves nothing")
	}
	const batch = 120
	t.Run("render", func(t *testing.T) {
		for start := 0; start < len(accepted); start += batch {
			cases := accepted[start:min(start+batch, len(accepted))]
			t.Run(fmt.Sprintf("batch-%04d", start), func(t *testing.T) {
				t.Parallel()
				p := withLines(base, len(cases), "x")
				for i, s := range cases {
					p.Lines[i].Description = s
				}
				if err := CheckPrintable(p); err != nil {
					t.Fatalf("batch: %v", err)
				}
				_, err := r.RenderPayload(context.Background(), p, false)
				var invalid *InvalidPayloadError
				if errors.As(err, &invalid) {
					for path := range invalid.Details {
						var i int
						if _, scanErr := fmt.Sscanf(path, "lines[%d]", &i); scanErr == nil && i < len(cases) {
							t.Errorf("accepted by the pre check, overflows in Typst: %q", cases[i])
						} else {
							t.Errorf("overflow at %s", path)
						}
					}
				} else if err != nil {
					t.Fatal(err)
				}
			})
		}
	})
}

// cellRect is one fit cell as Typst laid it out, in points.
type cellRect struct {
	page       int
	x, y, w, h float64
}

func points(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case string:
		f, err := strconv.ParseFloat(strings.TrimSuffix(n, "pt"), 64)
		if err == nil {
			return f
		}
	}

	return 0
}

// fitCells compiles src with the template's fit-debug switch on and
// queries every cell's rectangle.
func fitCells(t *testing.T, bin string, src Source) []cellRect {
	t.Helper()
	const off, on = "#let fit-debug = false", "#let fit-debug = true"
	if !strings.Contains(string(src.Template), off) {
		t.Fatal("the template has no fit-debug switch")
	}
	src.Template = []byte(strings.Replace(string(src.Template), off, on, 1))
	dir := t.TempDir()
	if err := writeProject(dir, src); err != nil {
		t.Fatal(err)
	}
	args := []string{
		"query", "--root", dir, "--font-path", filepath.Join(dir, "fonts"),
		"--ignore-system-fonts", "--ignore-embedded-fonts", filepath.Join(dir, "main.typ"), "<fit-cell>", "--field", "value",
	}
	out, err := exec.CommandContext(context.Background(), bin, args...).Output()
	if err != nil {
		t.Fatalf("typst query: %v", err)
	}
	var raw []map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatal(err)
	}
	cells := make([]cellRect, 0, len(raw))
	for _, c := range raw {
		cells = append(cells, cellRect{int(points(c["page"])), points(c["x"]), points(c["y"]), points(c["w"]), points(c["h"])})
	}

	return cells
}

// assertNotClipped compares every word pdftotext finds with the fit
// cells: a word that touches a cell must lie inside one horizontally.
// Clipped glyphs stay in the text layer, so this sees what the overflow
// check would have to catch.
func assertNotClipped(t *testing.T, bin, pdftotext string, src Source, pdf []byte) {
	t.Helper()
	const tolerance = 0.3
	var in Input
	if err := json.Unmarshal(src.Data, &in); err != nil {
		t.Fatal(err)
	}
	// The only texts outside a fit cell: the title, the page number line
	// and the draft watermark.
	allowed := map[string]bool{in.PageLabel: true, in.PageOfLabel: true, in.Watermark: true}
	for _, w := range strings.Fields(in.Title) {
		allowed[w] = true
	}
	cells := fitCells(t, bin, src)
	reported := 0
	report := func(format string, args ...any) {
		t.Errorf(format, args...)
		reported++
	}
	for _, b := range wordBoxes(t, pdftotext, pdf) {
		if reported > 5 {
			return
		}
		touched, inside := false, false
		for _, c := range cells {
			if c.page != b.page+1 {
				continue
			}
			dx := min(b.xMax, c.x+c.w) - max(b.xMin, c.x)
			dy := min(b.yMax, c.y+c.h) - max(b.yMin, c.y)
			if dx <= tolerance || dy <= 1 {
				continue
			}
			touched = true
			if b.xMin >= c.x-tolerance && b.xMax <= c.x+c.w+tolerance {
				inside = true
			}
		}
		if touched {
			if !inside {
				report("page %d: %q at %.1f..%.1f crosses its cell's edge", b.page+1, shortWord(b.text), b.xMin, b.xMax)
			}

			continue
		}
		// Outside every cell: allowed for the fixed texts and the page
		// numbers, and for the continuation of a cell an earlier page
		// records (a description broken over a page), within its columns.
		if allowed[b.text] || strings.Trim(b.text, "0123456789") == "" {
			continue
		}
		continued := false
		for _, c := range cells {
			// The cell reaches below the earlier page's body: it broke.
			if c.page < b.page+1 && c.y+c.h > 600 && b.xMin >= c.x-tolerance && b.xMax <= c.x+c.w+tolerance {
				continued = true

				break
			}
		}
		if !continued {
			report("page %d: %q at %.1f..%.1f lies outside every cell", b.page+1, shortWord(b.text), b.xMin, b.xMax)
		}
	}
}
