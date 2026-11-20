package pdf

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// TestRealisticValuesFit measures realistic values in their cells: they
// must fit, so the fixed widths of document-v1 refuse only the absurd.
func TestRealisticValuesFit(t *testing.T) {
	m, err := loadMetrics()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		s    string
		c    cell
	}{
		{"position", "1000", cellPosition},
		{"quantity", "1.234,5", cellQuantity},
		{"quantity", "999,125", cellQuantity},
		{"unit", "Pauschale", cellUnit},
		{"unit", "Kilogramm", cellUnit},
		{"unit price", "99.999,99 EUR", cellUnitPrice},
		{"vat", "10,5" + string(noBreakSpace) + "%", cellVAT},
		{"amount", "12.345.670,00 EUR", cellAmount},
		{"amount negative", "-1.234.567,00 EUR", cellAmount},
		{"description word", "Donaudampfschifffahrtsgesellschaft", cellDescription(true)},
		{"meta word", "RE-2026-000001-1", cellMetaValue},
		{"total value", "99.999.999,99 EUR", cellTotalStrong},
		{"footer word", "buchhaltung@mustermann-consulting.de", cellFooter},
		{"address word", "Rechtsanwaltsgesellschaft", cellAddress},
	}
	for _, c := range cases {
		w := m.widthMM(c.s, c.c)
		t.Logf("%-18s %-40q %5.1f mm of %5.1f mm", c.name, c.s, w, c.c.widthMM)
		if w > c.c.widthMM {
			t.Errorf("%s: %q needs %.1f mm, the cell has %.1f mm", c.name, c.s, w, c.c.widthMM)
		}
	}
}

// TestLineHeightFactor checks the row height the page estimate uses
// against the fonts: ascender plus descender plus the template's
// leading.
func TestLineHeightFactor(t *testing.T) {
	cov, err := loadCoverage()
	if err != nil {
		t.Fatal(err)
	}
	var buf sfnt.Buffer
	for i, f := range cov.fonts {
		upem := f.UnitsPerEm()
		m, err := f.Metrics(&buf, fixed.I(int(upem)), font.HintingNone)
		if err != nil {
			t.Fatal(err)
		}
		row := float64(m.Ascent+m.Descent)/64/float64(upem) + leadingEm
		t.Logf("font %d: %.3f em a row", i, row)
		if row > lineHeightFactor {
			t.Errorf("font %d needs %.3f em a row, the estimate uses %.2f", i, row, lineHeightFactor)
		}
	}
}

// TestLabelsFit checks the template's own texts against their cells.
func TestLabelsFit(t *testing.T) {
	m, err := loadMetrics()
	if err != nil {
		t.Fatal(err)
	}
	for lang, l := range labelsByLanguage {
		heads := []string{l.Position, l.Description, l.Quantity, l.Unit, l.UnitPrice, l.VATRate, l.Amount}
		for i, c := range tableHeaderCells(true) {
			if w := m.tooWide(heads[i], c); w != "" {
				t.Errorf("%s: table header %q is too wide", lang, w)
			}
		}
		for _, label := range []string{
			l.Date, l.ServiceDate, l.ServicePeriod, l.CustomerNumber, l.CustomerReference,
			l.CustomerVATID, l.ValidUntil, l.DueDate, l.Number["invoice"], l.Number["offer"],
		} {
			if m.rows(label, cellMetaLabel) > 1 || m.tooWide(label, cellMetaLabel) != "" {
				t.Errorf("%s: meta label %q does not fit one line", lang, label)
			}
		}
	}
}

// TestLayoutConstantsMatchTemplate compares layout.go's widths with the
// template's.
func TestLayoutConstantsMatchTemplate(t *testing.T) {
	b, err := assets.ReadFile(templates[CurrentTemplateVersion])
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	mm := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + "mm" }
	for _, want := range []string{
		fmt.Sprintf("columns: (%s, 1fr, %s, %s, %s) + vat-col(%s) + (%s,)",
			mm(colPositionMM), mm(colQuantityMM), mm(colUnitMM), mm(colUnitPriceMM), mm(colVATMM), mm(colAmountMM)),
		fmt.Sprintf("inset: (x: %s, y: %s)", mm(cellInsetMM), mm(cellInsetYMM)),
		fmt.Sprintf("set text(size: %spt)", strconv.FormatFloat(tableSizePt, 'f', -1, 64)),
		fmt.Sprintf("text(size: %spt, fill: muted, mark(path(\"line_discount\")", strconv.FormatFloat(discountSizePt, 'f', -1, 64)),
		fmt.Sprintf("columns: (1fr, %s)", mm(metaWidthMM)),
		fmt.Sprintf("column-gutter: %s,\n  fit(mark => text(size: 10pt", mm(addressGutterMM)),
		fmt.Sprintf("columns: (%s, 1fr),\n    column-gutter: %s", mm(metaLabelMM), mm(metaGutterMM)),
		fmt.Sprintf("width: %s, grid(\n  columns: (1fr, %s),\n  column-gutter: %s", mm(totalsWidthMM), mm(totalsValueMM), mm(totalsGutterMM)),
		fmt.Sprintf("column-gutter: %s,\n    row-gutter: 1.5mm", mm(footerGutterMM)),
		fmt.Sprintf("#let text-width = %s", mm(textWidthMM)),
		fmt.Sprintf("#set par(leading: %sem", strconv.FormatFloat(leadingEm, 'f', -1, 64)),
		"top-edge: \"ascender\",\n  bottom-edge: \"descender\",\n  overhang: false,",
		fmt.Sprintf("top: %s, bottom: footer-height + %s", mm(topMarginMM), mm(bottomExtraMM)),
		"margin: (left: 25mm, right: 20mm",
		// Every cell clips as the last resort, and the overflow check runs.
		"block(width: 100%, clip: true, {",
		`assert(x <= c.x + c.w + 0.05pt and x >= c.x - 0.05pt, message: "overflow: " + m.value.path)`,
		"paper: \"a4\"",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("template %s lacks %q", CurrentTemplateVersion, want)
		}
	}
	if strings.Contains(src, "200B") || strings.Contains(src, "zws") {
		t.Error("the template inserts zero width spaces")
	}
}

// TestLimitsPinned pins document-v1's limits: a shipped version's limits
// never tighten, because issued payloads must stay renderable.
func TestLimitsPinned(t *testing.T) {
	want := Limits{
		Lines: 1000, LineChars: 200, UnitChars: 16, SellerChars: 80,
		FooterTextChars: 300, FooterTextLines: 4, TextChars: 1000, TextLines: 20,
		Pages: 200, DataBytes: 2 << 20,
	}
	if got, ok := TemplateLimits("document-v1"); !ok || got != want {
		t.Errorf("document-v1 limits = %+v, want %+v: a shipped version's limits never change", got, want)
	}
	for version := range templates {
		if _, ok := limitsByTemplate[version]; !ok {
			t.Errorf("template %s has no limits", version)
		}
	}
}

func TestUnknownVersionFailsClosed(t *testing.T) {
	p := loadFixture(t, "offer-de")
	p.TemplateVersion = "document-v0"
	if err := CheckPrintable(p); !errors.Is(err, ErrUnknownTemplate) {
		t.Errorf("CheckPrintable = %v, want ErrUnknownTemplate", err)
	}
	if _, err := BuildInput(p, false); !errors.Is(err, ErrUnknownTemplate) {
		t.Errorf("BuildInput = %v, want ErrUnknownTemplate", err)
	}

	// A template without limits fails closed too.
	templates["document-vtest"] = templates[CurrentTemplateVersion]
	defer delete(templates, "document-vtest")
	p.TemplateVersion = "document-vtest"
	if err := CheckPrintable(p); !errors.Is(err, ErrUnknownTemplate) {
		t.Errorf("CheckPrintable without limits = %v, want ErrUnknownTemplate", err)
	}
	if _, err := BuildInput(p, false); !errors.Is(err, ErrUnknownTemplate) {
		t.Errorf("BuildInput without limits = %v, want ErrUnknownTemplate", err)
	}
}

func TestPageCountEnforced(t *testing.T) {
	bin := fakeTypst(t, `printf '%%PDF-1.7\n1 0 obj << /Type /Pages /Count 201 /Kids [] >> endobj\n'`)
	r := New(Options{Binary: bin, TempDir: t.TempDir()})
	if _, err := r.Render(context.Background(), offerPayload(t), false); !errors.Is(err, ErrTooManyPages) {
		t.Fatalf("err = %v, want ErrTooManyPages", err)
	}
	if n, err := pageCount([]byte("%PDF-1.7 << /Count 3 /Type /Pages >> << /Type /Pages /Count 2 >>")); err != nil || n != 3 {
		t.Errorf("pageCount = %d, %v, want 3", n, err)
	}
	if _, err := pageCount([]byte("%PDF-1.7")); err == nil {
		t.Error("pageCount of a PDF without a page tree did not fail")
	}
}

func TestWidthCheck(t *testing.T) {
	m, err := loadMetrics()
	if err != nil {
		t.Fatal(err)
	}
	c := cellDescription(true)
	nbsp := string(noBreakSpace)
	for _, s := range []string{
		strings.Repeat("W", 16) + nbsp + strings.Repeat("W", 16),
		strings.Repeat("W", 30) + "-1" + strings.Repeat("W", 10),
		"a@" + strings.Repeat("W", 30),
		"https://" + strings.Repeat("W", 30),
		strings.Repeat("W", 12) + string(rune(0x202F)) + strings.Repeat("W", 12),
		strings.Repeat("W", 12) + string(rune(0x2007)) + strings.Repeat("W", 12),
	} {
		if m.tooWide(s, c) == "" {
			t.Errorf("%q is accepted as fitting %.0f mm", s, c.widthMM)
		}
	}
	if w := m.tooWide("ein ganz normaler Satz", c); w != "" {
		t.Errorf("ordinary words refused: %q", w)
	}
	if got := m.rows("aaa bbb", cell{m.widthMM("aaa", cell{1, 8, regular}) / widthMargin * 1.1, 8, regular}); got != 2 {
		t.Errorf("rows = %d, want 2", got)
	}
}

// wide is the longest run of "W" that fits c, plus extra more.
func wide(t *testing.T, c cell, extra int) string {
	t.Helper()
	m, err := loadMetrics()
	if err != nil {
		t.Fatal(err)
	}
	n := 1
	for m.widthMM(strings.Repeat("W", n+1), c) <= c.widthMM {
		n++
	}

	return strings.Repeat("W", n+extra)
}

// wideWords is n characters of the widest words c takes, one space
// apart.
func wideWords(t *testing.T, c cell, n int) string {
	t.Helper()
	w := wide(t, c, 0) + " "

	return strings.TrimRight(strings.Repeat(w, n/len(w)+1)[:n], " ")
}

// wordBox is one word pdftotext found, in points.
type wordBox struct {
	page                   int
	xMin, yMin, xMax, yMax float64
	text                   string
}

var (
	bboxPage = regexp.MustCompile(`<page `)
	bboxWord = regexp.MustCompile(`<word xMin="([\d.]+)" yMin="([\d.]+)" xMax="([\d.]+)" yMax="([\d.]+)">([^<]*)</word>`)
)

// wordBoxes runs pdftotext -bbox-layout and returns every word.
func wordBoxes(t *testing.T, pdftotext string, pdf []byte) []wordBox {
	t.Helper()
	dir := t.TempDir()
	in := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(in, pdf, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(context.Background(), pdftotext, "-bbox-layout", in, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	var boxes []wordBox
	pages := bboxPage.FindAllIndex(out, -1)
	for _, m := range bboxWord.FindAllSubmatchIndex(out, -1) {
		page := 0
		for i, p := range pages {
			if p[0] < m[0] {
				page = i
			}
		}
		f := func(i int) float64 {
			v, err := strconv.ParseFloat(string(out[m[2*i]:m[2*i+1]]), 64)
			if err != nil {
				t.Fatal(err)
			}

			return v
		}
		boxes = append(boxes, wordBox{page, f(1), f(2), f(3), f(4), string(out[m[10]:m[11]])})
	}

	return boxes
}

// assertNoOverlap fails when two words on a page overlap by more than a
// point in both directions: text running over a neighbour cell.
func assertNoOverlap(t *testing.T, pdftotext string, pdf []byte) {
	t.Helper()
	const tolerance = 1.0
	boxes := wordBoxes(t, pdftotext, pdf)
	if len(boxes) == 0 {
		t.Fatal("pdftotext found no words")
	}
	reported := 0
	for i := range boxes {
		for j := i + 1; j < len(boxes); j++ {
			a, b := boxes[i], boxes[j]
			if a.page != b.page {
				continue
			}
			dx := min(a.xMax, b.xMax) - max(a.xMin, b.xMin)
			dy := min(a.yMax, b.yMax) - max(a.yMin, b.yMin)
			if dx > tolerance && dy > tolerance {
				t.Errorf("page %d: %q at %.0f,%.0f overlaps %q at %.0f,%.0f",
					a.page+1, shortWord(a.text), a.xMin, a.yMin, shortWord(b.text), b.xMin, b.yMin)
				reported++
				if reported > 5 {
					return
				}
			}
		}
	}
}

// renderChecked renders an accepted payload and checks it for overlaps
// and its page count.
func renderChecked(t *testing.T, r *Renderer, pdftotext, name string, p docpayload.FrozenPayloadV1) {
	t.Helper()
	pdf, err := r.RenderPayload(context.Background(), p, false)
	if err != nil {
		t.Fatalf("%s: accepted payload does not render: %v", name, err)
	}
	writeOut(t, os.Getenv("SOLOBILL_PDF_OUT"), name, pdf)
	if pages := pagesOf(t, pdf); pages > MaxPages {
		t.Errorf("%s: %d pages, over %d", name, pages, MaxPages)
	}
	assertNoOverlap(t, pdftotext, pdf)
	src, err := BuildSource(p, false)
	if err != nil {
		t.Fatal(err)
	}
	assertNotClipped(t, r.opts.Binary, pdftotext, src, pdf)
}

// TestOverprintCases are the reviews' overprint payloads. Each must be
// refused at a field path or render without two words overlapping.
func TestOverprintCases(t *testing.T) {
	bin := typstOrSkip(t)
	pdftotext := toolOrSkip(t, "pdftotext")
	r := New(Options{Binary: bin, TempDir: t.TempDir()})
	wm := func(n int) string { return strings.Repeat("WM", n)[:n] }
	nb := string(noBreakSpace)
	base := loadFixture(t, "invoice-standard-de")
	base.Lines = base.Lines[:6]

	cases := map[string]func(p *docpayload.FrozenPayloadV1){
		"nbsp chain": func(p *docpayload.FrozenPayloadV1) {
			w := strings.Repeat("W", 16)
			p.Lines[0].Description = w + nb + w + nb + w + nb + w
			p.Customer.Name = w + nb + w + nb + w
			p.Seller.Email = strings.Repeat("W", 30) + nb + strings.Repeat("W", 30)
		},
		"at sign name": func(p *docpayload.FrozenPayloadV1) { p.Customer.Name = "a@" + strings.Repeat("W", 60) },
		"url": func(p *docpayload.FrozenPayloadV1) {
			p.Lines[1].Description = "Siehe https://" + strings.Repeat("W", 60)
		},
		"hyphen digit": func(p *docpayload.FrozenPayloadV1) {
			p.Notes = strings.Repeat("W", 46) + "-1" + strings.Repeat("W", 46)
		},
		"hyphen digit 2": func(p *docpayload.FrozenPayloadV1) {
			p.Seller.Email = strings.Repeat("W", 30) + "-1" + strings.Repeat("W", 29)
		},
		"wide unit and figures": func(p *docpayload.FrozenPayloadV1) {
			for i := range p.Lines {
				p.Lines[i].Description = strings.Repeat("W", 17+3*i)
				p.Lines[i].Unit = wm(16)
				p.Lines[i].QuantityThousandths = 999_999_999_999
				p.Lines[i].UnitPriceCents = 99_999_999_999_999
				p.Lines[i].NetAfterLineDiscountCents = 9_999_999_999_999_999
			}
		},
		"ww words": func(p *docpayload.FrozenPayloadV1) {
			for i := range p.Lines {
				p.Lines[i].Description = strings.Repeat("WW WW WW WW\n", 3) + "WW WW WW WW"
			}
		},
		"long number": func(p *docpayload.FrozenPayloadV1) { p.Number = strings.Repeat("W", 36) },
		"realistic big figures": func(p *docpayload.FrozenPayloadV1) {
			for i := range p.Lines {
				p.Lines[i].Description = "Rindfleischetikettierungsueberwachung"[:30]
				p.Lines[i].Unit = "Pauschale"
				p.Lines[i].QuantityThousandths = 1_000_000
				p.Lines[i].UnitPriceCents = 1_234_567
				p.Lines[i].NetAfterLineDiscountCents = 1_234_567_000
			}
		},
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			p := base
			p.Lines = append([]docpayload.Line(nil), base.Lines...)
			set(&p)
			var invalid *InvalidPayloadError
			err := CheckPrintable(p)
			if errors.As(err, &invalid) {
				t.Logf("refused: %v", invalid.Details)

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			renderChecked(t, r, pdftotext, "overprint-"+strings.ReplaceAll(name, " ", "-"), p)
		})
	}
}

// fuzzText draws up to n characters of words: mostly short, sometimes
// long, of wide, narrow and special characters, joined by spaces, no
// break spaces and, for free text, line feeds.
func fuzzText(rng *rand.Rand, n int, multiline bool) string {
	letters := []string{"W", "M", "i", "l", "a", "e", "n", "ä", "ß", "0", "9"}
	specials := []string{"-", "/", "@", "://", ".", ",", "€", "§", "(", ")", "„", "“"}
	var b strings.Builder
	length := rng.IntN(n + 1)
	for b.Len() < length {
		word := 1 + rng.IntN(8)
		if rng.IntN(25) == 0 {
			word = 10 + rng.IntN(30)
		}
		for range word {
			if rng.IntN(7) == 0 {
				b.WriteString(specials[rng.IntN(len(specials))])
			} else {
				b.WriteString(letters[rng.IntN(len(letters))])
			}
		}
		switch r := rng.IntN(20); {
		case r == 0:
			b.WriteString(string(noBreakSpace))
		case r == 1 && multiline:
			b.WriteString("\n")
		default:
			b.WriteString(" ")
		}
	}

	return strings.TrimSpace(b.String())
}

// TestFuzzLayout draws 300 payloads from a fixed seed. Every accepted
// one up to 60 is rendered and checked: no two words overlap, at most
// MaxPages pages. Skipped with -short.
func TestFuzzLayout(t *testing.T) {
	if testing.Short() {
		t.Skip("renders 60 documents")
	}
	bin := typstOrSkip(t)
	pdftotext := toolOrSkip(t, "pdftotext")
	r := New(Options{Binary: bin, TempDir: t.TempDir()})
	rng := rand.New(rand.NewPCG(74, 91))
	base := loadFixture(t, "invoice-standard-de")
	figures := []int64{0, 1, 99, 12_345, 1_234_567, 99_999_999, -5_000, 12_000, 450_000}
	// SOLOBILL_FUZZ_N draws more payloads and renders every accepted one,
	// for a longer local run.
	draws, renders := 300, 60
	if n, err := strconv.Atoi(os.Getenv("SOLOBILL_FUZZ_N")); err == nil && n > 0 {
		draws, renders = n, n
	}
	figure := func() int64 {
		// One in 40 is far too wide for its cell.
		if rng.IntN(40) == 0 {
			return 999_999_999_999_999
		}

		return figures[rng.IntN(len(figures))]
	}

	accepted, refused, rendered := 0, 0, 0
	for i := range draws {
		p := base
		p.Lines = make([]docpayload.Line, 1+rng.IntN(12))
		for j := range p.Lines {
			l := base.Lines[j%len(base.Lines)]
			l.Position = int32(j + 1)
			l.Description = fuzzText(rng, 160, true)
			if l.Description == "" {
				l.Description = "x"
			}
			l.Unit = fuzzText(rng, 10, false)
			l.QuantityThousandths = 1 + figure()%100_000_000
			l.UnitPriceCents = figure()
			l.NetAfterLineDiscountCents = figure()
			p.Lines[j] = l
		}
		p.Customer.Name = fuzzText(rng, 80, false) + "x"
		p.Customer.Address.Street = fuzzText(rng, 60, false) + "x"
		p.CustomerReference = fuzzText(rng, 60, false)
		p.Seller.LegalName = fuzzText(rng, 70, false) + "x"
		p.Seller.Email = fuzzText(rng, 60, false)
		p.Seller.BankName = fuzzText(rng, 60, false)
		p.Seller.FooterText = fuzzText(rng, 300, false)
		p.Notes = fuzzText(rng, 400, true)

		var invalid *InvalidPayloadError
		err := CheckPrintable(p)
		switch {
		case errors.As(err, &invalid):
			refused++
			if refused <= 3 {
				t.Logf("payload %d refused: %v", i, invalid.Details)
			}

			continue
		case err != nil:
			t.Fatalf("payload %d: %v", i, err)
		}
		accepted++
		if rendered < renders {
			rendered++
			renderChecked(t, r, pdftotext, fmt.Sprintf("fuzz-%03d", i), p)
		}
	}
	t.Logf("%d payloads: %d accepted, %d refused, %d rendered", draws, accepted, refused, rendered)
	if accepted < 30 {
		t.Errorf("only %d payloads accepted: the generator is too hostile to test the layout", accepted)
	}
}
