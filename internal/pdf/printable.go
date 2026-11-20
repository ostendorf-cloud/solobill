package pdf

import (
	"fmt"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/font/sfnt"
)

// The bundled fonts are the only fonts Typst may use, and PDF/A-3b
// refuses a character none of them can show. A payload string is
// therefore checked rune by rune before it reaches Typst, so a payload
// is refused with the field that holds the character instead of failing
// the compile.
//
// The rules:
//   - Every rune needs a glyph in every bundled font (regular and bold).
//   - Control characters are refused. A free text field (description,
//     notes, payment terms, tax note, footer text) may hold a line feed,
//     a carriage return and a tab: the line feed breaks the line, the
//     carriage return is dropped and the tab prints as white space.
//   - Bidirectional formatting controls (U+202A to U+202E and U+2066 to
//     U+2069) are refused: they can reorder what a reader sees, for
//     example the digits of an IBAN.
//
// Refusing is chosen over stripping: an issued document's frozen
// payload must print exactly what it holds, and #70 runs CheckPrintable
// at issue time, so a refused character never reaches a frozen payload.

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
	problems map[string]string
}

func newPrinter() (*printer, error) {
	cov, err := loadCoverage()
	if err != nil {
		return nil, err
	}

	return &printer{cov: cov, problems: map[string]string{}}, nil
}

// check records the first rune of s that cannot be printed.
func (pr *printer) check(path, s string, multiline bool) {
	for _, r := range s {
		var reason string
		switch {
		case multiline && (r == '\n' || r == '\r' || r == '\t'):
			continue
		case isBidiControl(r):
			reason = fmt.Sprintf("bidirectional control character %U is not allowed", r)
		case unicode.IsControl(r):
			reason = fmt.Sprintf("control character %U is not allowed", r)
		case r == unicode.ReplacementChar:
			reason = "invalid UTF-8 or replacement character U+FFFD is not allowed"
		case !pr.cov.covers(&pr.buf, r):
			reason = fmt.Sprintf("character %q (%U) cannot be printed with the bundled font", r, r)
		default:
			continue
		}
		if _, seen := pr.problems[path]; !seen {
			pr.problems[path] = reason
		}

		return
	}
}

// Length limits in characters. Long text wraps (the template breaks a
// run of 25 characters without a space), but the footer has a fixed
// height on every page: the seller's fields and footer text are kept
// short enough that real data fits, and the footer clips what does not.
const (
	// MaxLineChars bounds a single line field.
	MaxLineChars = 200
	// MaxSellerChars bounds a seller field, printed in the footer.
	MaxSellerChars = 80
	// MaxFooterTextChars bounds the seller's footer text.
	MaxFooterTextChars = 300
	// MaxUnitChars bounds a line's unit, printed in a narrow column.
	MaxUnitChars = 16
)

// unit checks a line's unit.
func (pr *printer) unit(path, s string) string {
	pr.check(path, s, false)
	pr.limit(path, s, MaxUnitChars)

	return s
}

// limit records a field longer than maxChars characters.
func (pr *printer) limit(path, s string, maxChars int) {
	if utf8.RuneCountInString(s) > maxChars {
		if _, seen := pr.problems[path]; !seen {
			pr.problems[path] = fmt.Sprintf("at most %d characters can be printed", maxChars)
		}
	}
}

// line checks a single line field and returns it unchanged.
func (pr *printer) line(path, s string) string {
	pr.check(path, s, false)
	pr.limit(path, s, MaxLineChars)

	return s
}

// seller checks a seller field printed in the footer.
func (pr *printer) seller(path, s string) string {
	pr.check(path, s, false)
	pr.limit(path, s, MaxSellerChars)

	return s
}

// text checks a free text field and returns its printed lines.
func (pr *printer) text(path, s string) []string {
	pr.check(path, s, true)

	return splitLines(s)
}
