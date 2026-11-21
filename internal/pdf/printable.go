package pdf

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/font/sfnt"
	"golang.org/x/text/language"
)

// The bundled fonts are the only fonts Typst may use, and PDF/A-3b
// refuses a character none of them can show. A payload string is
// therefore checked rune by rune before it reaches Typst, so a payload
// is refused with the field that holds the character instead of failing
// the compile.
//
// The rules:
//   - Every rune needs a glyph in every bundled font (regular and bold).
//     A string the renderer transforms before printing (the upper-cased
//     country name) is checked as printed, and the whole template data
//     is checked once more before Typst runs.
//   - Control characters are refused. A free text field (description,
//     notes, payment terms, tax note, footer text) may hold a line feed,
//     a carriage return and a tab: the line feed breaks the line, the
//     carriage return is dropped and the tab prints as a space.
//   - Bidirectional formatting controls (U+202A to U+202E and U+2066 to
//     U+2069) are refused: they can reorder what a reader sees, for
//     example the digits of an IBAN.
//   - Every word must fit the cell it is printed in, measured with the
//     fonts' advances (layout.go).
//   - Lengths, line counts and pages are bounded by the Limits of the
//     template version.
//
// Refusing is chosen over stripping: an issued document's frozen
// payload must print exactly what it holds, and #70 runs CheckPrintable
// at issue time, so a refused character never reaches a frozen payload.

// Limits bound what one template version can print. They are part of
// the version's frozen contract: a payload issued under a version stays
// renderable forever, so a version's limits are never tightened. A
// tighter limit is a new template version.
type Limits struct {
	// Lines is the most item lines.
	Lines int
	// LineChars bounds a single line field (customer, number, reference).
	LineChars int
	// UnitChars bounds a line's unit, printed in a narrow column.
	UnitChars int
	// SellerChars bounds a seller field, printed in the footer.
	SellerChars int
	// FooterTextChars and FooterTextLines bound the footer text.
	FooterTextChars int
	FooterTextLines int
	// TextChars and TextLines bound a free text field: a description, the
	// tax note, the payment terms and the notes.
	TextChars int
	TextLines int
	// Pages bounds the page count. CheckPrintable computes the pages from
	// the measured rows before Typst runs, and Render counts them in the
	// PDF afterwards.
	Pages int
	// DataBytes bounds data.json.
	DataBytes int
}

// documentV1Limits are document-v1's limits. Every word must also fit
// its cell (layout.go). TestWorstCaseRender renders the largest
// payloads they allow and checks they stay below 5 MiB and Pages.
var documentV1Limits = Limits{
	Lines:           1000,
	LineChars:       200,
	UnitChars:       16,
	SellerChars:     80,
	FooterTextChars: 300,
	FooterTextLines: 4,
	TextChars:       1000,
	TextLines:       20,
	Pages:           MaxPages,
	DataBytes:       2 << 20,
}

// MaxPages is the most pages a document-v1 PDF has. The largest payloads
// the limits allow render to about 18 KB a page with typst 0.14.2
// (TestWorstCaseRender), so MaxPages pages stay far below the 10 MiB
// output limit.
const MaxPages = 200

// limitsByTemplate pins the limits per template version.
var limitsByTemplate = map[string]Limits{
	"document-v1": documentV1Limits,
}

// TemplateLimits returns the limits of a template version.
func TemplateLimits(version string) (Limits, bool) {
	l, ok := limitsByTemplate[version]

	return l, ok
}

// fontCoverage is every rune the bundled fonts can all show.
type fontCoverage struct {
	fonts []*sfnt.Font
}

var loadCoverage = sync.OnceValues(func() (*fontCoverage, error) {
	c := &fontCoverage{}
	for _, name := range []string{"fonts/Go-Regular.ttf", "fonts/Go-Bold.ttf"} {
		b, err := assets.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("pdf: read %s: %w", name, err)
		}
		f, err := sfnt.Parse(b)
		if err != nil {
			return nil, fmt.Errorf("pdf: parse %s: %w", name, err)
		}
		c.fonts = append(c.fonts, f)
	}

	return c, nil
})

// covers reports whether every bundled font has a glyph for r.
func (c *fontCoverage) covers(buf *sfnt.Buffer, r rune) bool {
	for _, f := range c.fonts {
		g, err := f.GlyphIndex(buf, r)
		if err != nil || g == 0 {
			return false
		}
	}

	return true
}

func isBidiControl(r rune) bool {
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}

// printer collects the problems of every payload string BuildInput
// prints, keyed by the payload's field path.
type printer struct {
	cov      *fontCoverage
	buf      sfnt.Buffer
	m        *metrics
	lim      Limits
	problems map[string]string
	// showVAT selects the description cell's width.
	showVAT bool
}

func newPrinter(lim Limits) (*printer, error) {
	cov, err := loadCoverage()
	if err != nil {
		return nil, err
	}
	m, err := loadMetrics()
	if err != nil {
		return nil, err
	}

	return &printer{cov: cov, m: m, lim: lim, problems: map[string]string{}}, nil
}

func (pr *printer) add(path, reason string) {
	if _, seen := pr.problems[path]; !seen {
		pr.problems[path] = reason
	}
}

// runeProblem names why r cannot be printed, or "" when it can.
func (pr *printer) runeProblem(r rune, multiline bool) string {
	switch {
	case multiline && (r == '\n' || r == '\r' || r == '\t'):
		return ""
	case isBidiControl(r):
		return fmt.Sprintf("bidirectional control character %U is not allowed", r)
	case unicode.IsControl(r):
		return fmt.Sprintf("control character %U is not allowed", r)
	case r == unicode.ReplacementChar:
		return "invalid UTF-8 or replacement character U+FFFD is not allowed"
	case !pr.cov.covers(&pr.buf, r):
		return fmt.Sprintf("character %q (%U) cannot be printed with the bundled font", r, r)
	default:
		return ""
	}
}

// check records the first rune of s that cannot be printed.
func (pr *printer) check(path, s string, multiline bool) {
	for _, r := range s {
		if reason := pr.runeProblem(r, multiline); reason != "" {
			pr.add(path, reason)

			return
		}
	}
}

func (pr *printer) limit(path, s string, maxChars int) {
	if utf8.RuneCountInString(s) > maxChars {
		pr.add(path, fmt.Sprintf("at most %d characters can be printed", maxChars))
	}
}

func (pr *printer) lineCount(path string, lines []string, maxLines int) {
	if len(lines) > maxLines {
		pr.add(path, fmt.Sprintf("at most %d lines can be printed", maxLines))
	}
}

// fit records the first word of s wider than one of the cells it is
// printed in.
func (pr *printer) fit(path, s string, cells ...cell) {
	for _, c := range cells {
		if w := pr.m.tooWide(s, c); w != "" {
			pr.add(path, tooWideReason(w, c))

			return
		}
	}
}

// fitWhole records a value that does not fit its cell on one line: the
// figures of the item table and the totals never wrap.
func (pr *printer) fitWhole(path, s string, c cell) string {
	if pr.m.widthMM(s, c) > c.widthMM {
		pr.add(path, fmt.Sprintf("value too wide for the cell: %q needs more than %.0f mm", shortWord(s), c.widthMM))
	}

	return s
}

// line checks a single line field printed in the given cells.
func (pr *printer) line(path, s string, cells ...cell) string {
	pr.check(path, s, false)
	pr.limit(path, s, pr.lim.LineChars)
	pr.fit(path, s, cells...)

	return s
}

// unit checks a line's unit.
func (pr *printer) unit(path, s string) string {
	pr.check(path, s, false)
	pr.limit(path, s, pr.lim.UnitChars)
	pr.fit(path, s, cellUnit)

	return s
}

// seller checks a seller field printed in the footer and the given
// cells.
func (pr *printer) seller(path, s string, cells ...cell) string {
	pr.check(path, s, false)
	pr.limit(path, s, pr.lim.SellerChars)
	pr.fit(path, s, append(cells, cellFooter)...)

	return s
}

// description checks a line's description.
func (pr *printer) description(path, s string) []string {
	lines := pr.freeText(path, s, pr.lim.TextChars, pr.lim.TextLines)
	pr.fit(path, strings.Join(lines, "\n"), cellDescription(pr.showVAT))

	return lines
}

// note checks the tax note, the payment terms or the notes.
func (pr *printer) note(path, s string) []string {
	lines := pr.freeText(path, s, pr.lim.TextChars, pr.lim.TextLines)
	pr.fit(path, strings.Join(lines, "\n"), cellFullText)

	return lines
}

// footerText checks the seller's footer text.
func (pr *printer) footerText(path, s string) []string {
	lines := pr.freeText(path, s, pr.lim.FooterTextChars, pr.lim.FooterTextLines)
	pr.fit(path, strings.Join(lines, "\n"), cellFooterText)

	return lines
}

func (pr *printer) freeText(path, s string, maxChars, maxLines int) []string {
	pr.check(path, s, true)
	pr.limit(path, s, maxChars)
	lines := splitLines(s)
	pr.lineCount(path, lines, maxLines)

	return lines
}

// country checks an ISO 3166-1 alpha-2 country code. Empty is allowed:
// nothing is printed then.
func (pr *printer) country(path, s string) string {
	if s == "" {
		return s
	}
	ok := len(s) == 2 && s[0] >= 'A' && s[0] <= 'Z' && s[1] >= 'A' && s[1] <= 'Z'
	if ok {
		r, err := language.ParseRegion(s)
		ok = err == nil && r.IsCountry() && r.String() == s
	}
	if !ok {
		pr.add(path, "must be an ISO 3166-1 alpha-2 country code")
	}

	return s
}

// composed fits a string as printed: several fields, or a label and a
// value, joined. Its parts were checked on their own already; together
// they can form a word none of them holds alone.
func (pr *printer) composed(path, s string, c cell) string {
	pr.fit(path, s, c)

	return s
}

// printed checks a string as the renderer transformed it, in its cell.
func (pr *printer) printed(path, s string, c cell) string {
	pr.check(path, s, false)
	pr.fit(path, s, c)

	return s
}

// ErrTemplateData is template data that holds a character the fonts
// cannot show although every payload field passed: a renderer bug, never
// a payload error.
var ErrTemplateData = errors.New("pdf: template data cannot be printed")

// checkData checks every string of the template data once more, so a
// transformation or a label the field checks miss can never reach
// Typst. It runs only after the field checks found nothing.
func (pr *printer) checkData(in Input) error {
	b, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("pdf: encode input: %w", err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return fmt.Errorf("pdf: decode input: %w", err)
	}
	if path, reason := pr.walkData("template", v); path != "" {
		return fmt.Errorf("%w: %s: %s", ErrTemplateData, path, reason)
	}

	return nil
}

// walkData returns the first string below v that cannot be printed.
func (pr *printer) walkData(path string, v any) (string, string) {
	switch t := v.(type) {
	case string:
		for _, r := range t {
			if reason := pr.runeProblem(r, true); reason != "" {
				return path, reason
			}
		}
	case []any:
		for i, e := range t {
			if p, reason := pr.walkData(fmt.Sprintf("%s[%d]", path, i), e); p != "" {
				return p, reason
			}
		}
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if p, reason := pr.walkData(path+"."+k, t[k]); p != "" {
				return p, reason
			}
		}
	}

	return "", ""
}
