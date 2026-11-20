package pdf

import (
	"encoding/json"
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
//     carriage return is dropped and the tab prints as white space.
//   - Bidirectional formatting controls (U+202A to U+202E and U+2066 to
//     U+2069) are refused: they can reorder what a reader sees, for
//     example the digits of an IBAN.
//   - Lengths, line counts and the longest run without a space are
//     bounded by the Limits of the template version.
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
	// FooterTokenChars bounds the longest run without a space in a seller
	// field and the footer text: the footer never breaks a word, and a
	// run this long fits a footer column at 7 pt in the widest glyphs.
	FooterTokenChars int
	// FooterTextChars and FooterTextLines bound the footer text.
	FooterTextChars int
	FooterTextLines int
	// TextChars and TextLines bound a free text field: a description, the
	// tax note, the payment terms and the notes.
	TextChars int
	TextLines int
	// NoteTokenChars bounds the longest run without a space in the tax
	// note, the payment terms and the notes: those are never broken
	// inside a word, and a run this long fits the text width.
	NoteTokenChars int
	// RowChars and Rows bound the descriptions: every description line
	// counts one row per RowChars characters, at least one, and the sum
	// over all item lines is at most Rows. Together with the other
	// limits this bounds the page count.
	RowChars int
	Rows     int
	// DataBytes bounds data.json.
	DataBytes int
}

// documentV1Limits are document-v1's limits. TestWorstCaseRender
// renders the largest payload they allow and checks it stays below
// 5 MiB and MaxPages pages.
var documentV1Limits = Limits{
	Lines:            1000,
	LineChars:        200,
	UnitChars:        16,
	SellerChars:      80,
	FooterTokenChars: 32,
	FooterTextChars:  300,
	FooterTextLines:  4,
	TextChars:        1000,
	TextLines:        20,
	NoteTokenChars:   48,
	// The description cell breaks a run of more than 32 characters every
	// 16 and holds at least 16 characters a row otherwise, so 16 per row
	// is an upper bound of the rows a description takes.
	RowChars:  16,
	Rows:      4000,
	DataBytes: 2 << 20,
}

// MaxPages is the page count the largest document-v1 payload stays
// below, measured by TestWorstCaseRender with typst 0.14.2: 147 pages
// and 1.9 MB for 1000 lines of four one character rows each, with every
// seller field and note at its limit.
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
	lim      Limits
	problems map[string]string
	rows     int
}

func newPrinter(lim Limits) (*printer, error) {
	cov, err := loadCoverage()
	if err != nil {
		return nil, err
	}

	return &printer{cov: cov, lim: lim, problems: map[string]string{}}, nil
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

// tokens records a run longer than maxChars characters that Typst cannot
// break: Typst breaks a line at a space and after "-" or "/".
func (pr *printer) tokens(path, s string, maxChars int) {
	breaks := func(r rune) bool { return unicode.IsSpace(r) || r == '-' || r == '/' }
	for _, tok := range strings.FieldsFunc(s, breaks) {
		if utf8.RuneCountInString(tok) > maxChars {
			pr.add(path, fmt.Sprintf("a word of at most %d characters without a space can be printed", maxChars))

			return
		}
	}
}

func (pr *printer) lineCount(path string, lines []string, maxLines int) {
	if len(lines) > maxLines {
		pr.add(path, fmt.Sprintf("at most %d lines can be printed", maxLines))
	}
}

// line checks a single line field and returns it unchanged.
func (pr *printer) line(path, s string) string {
	pr.check(path, s, false)
	pr.limit(path, s, pr.lim.LineChars)

	return s
}

// unit checks a line's unit.
func (pr *printer) unit(path, s string) string {
	pr.check(path, s, false)
	pr.limit(path, s, pr.lim.UnitChars)

	return s
}

// seller checks a seller field printed in the header or the footer.
func (pr *printer) seller(path, s string) string {
	pr.check(path, s, false)
	pr.limit(path, s, pr.lim.SellerChars)
	pr.tokens(path, s, pr.lim.FooterTokenChars)

	return s
}

// description checks a line's description and counts its rows.
func (pr *printer) description(path, s string) []string {
	lines := pr.freeText(path, s, pr.lim.TextChars, pr.lim.TextLines)
	for _, l := range lines {
		pr.rows += max(1, (utf8.RuneCountInString(l)+pr.lim.RowChars-1)/pr.lim.RowChars)
	}

	return lines
}

// note checks the tax note, the payment terms or the notes.
func (pr *printer) note(path, s string) []string {
	pr.tokens(path, s, pr.lim.NoteTokenChars)

	return pr.freeText(path, s, pr.lim.TextChars, pr.lim.TextLines)
}

// footerText checks the seller's footer text.
func (pr *printer) footerText(path, s string) []string {
	pr.tokens(path, s, pr.lim.FooterTokenChars)

	return pr.freeText(path, s, pr.lim.FooterTextChars, pr.lim.FooterTextLines)
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

// printed checks a string as the renderer transformed it.
func (pr *printer) printed(path, s string) string {
	pr.check(path, s, false)

	return s
}

// checkData checks every string of the template data once more, so a
// transformation or a label the field checks above miss can never reach
// Typst. A problem here is a renderer bug, reported under "template.".
func (pr *printer) checkData(in Input) error {
	b, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("pdf: encode input: %w", err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return fmt.Errorf("pdf: decode input: %w", err)
	}
	pr.walkData("template", v)

	return nil
}

func (pr *printer) walkData(path string, v any) {
	switch t := v.(type) {
	case string:
		for _, r := range t {
			if reason := pr.runeProblem(r, true); reason != "" {
				pr.add(path, reason)

				return
			}
		}
	case []any:
		for i, e := range t {
			pr.walkData(fmt.Sprintf("%s[%d]", path, i), e)
		}
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			pr.walkData(path+"."+k, t[k])
		}
	}
}
