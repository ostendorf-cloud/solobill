package pdf

import (
	"fmt"
	"strings"
	"testing"
	"unicode"

	"golang.org/x/image/font/sfnt"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
)

// maxScanned is the last code point the table comparison looks at. Every
// plane is scanned, so a font that gains a glyph outside the BMP shows up.
const maxScanned = unicode.MaxRune

// scanned reports whether the comparison covers r. Control characters,
// bidirectional controls and U+FFFD are refused for their own reasons before
// the glyph table is consulted, so the fonts' coverage does not decide them.
func scanned(r rune) bool {
	switch {
	case r < 0x20, unicode.IsControl(r), isBidiControl(r), r == unicode.ReplacementChar:
		return false
	case r >= 0xD800 && r <= 0xDFFF:
		return false
	default:
		return true
	}
}

// coveredRanges returns the runes both bundled fonts have a glyph for, as
// the inclusive ranges of docpayload's printableRanges literal.
func coveredRanges(t *testing.T) [][2]rune {
	t.Helper()

	cov, err := loadCoverage()
	if err != nil {
		t.Fatalf("load the bundled fonts: %v", err)
	}

	var (
		buf    sfnt.Buffer
		ranges [][2]rune
		open   bool
	)
	for r := rune(0x20); r <= maxScanned; r++ {
		if !scanned(r) || !cov.covers(&buf, r) {
			open = false

			continue
		}
		if open && ranges[len(ranges)-1][1] == r-1 {
			ranges[len(ranges)-1][1] = r
		} else {
			ranges = append(ranges, [2]rune{r, r})
		}
		open = true
	}

	return ranges
}

// literal formats ranges as the source of docpayload's printableRanges, so
// a failing run prints the table to paste after the font or the table
// changed on purpose.
func literal(ranges [][2]rune) string {
	var b strings.Builder
	for _, rg := range ranges {
		fmt.Fprintf(&b, "\t{0x%04X, 0x%04X},\n", rg[0], rg[1])
	}

	return b.String()
}

// TestPrintableTableMatchesFonts proves docpayload's glyph table equals the
// coverage of the two embedded fonts. The table is rebuilt from the fonts
// with the same rule as before: a glyph index other than 0 in both fonts.
// To regenerate the table, copy the literal that a failing run logs into
// docpayload.printableRanges.
//
// Both directions are errors:
//   - A rune the table accepts and a font lacks passes issuing and then
//     fails the PDF/A-3b render, and a frozen payload can never be fixed.
//   - A rune the fonts cover and the table lacks is refused although it
//     could be printed. Widening the accepted set is a decision, so the
//     test fails until the table is updated on purpose.
func TestPrintableTableMatchesFonts(t *testing.T) {
	t.Parallel()

	cov, err := loadCoverage()
	if err != nil {
		t.Fatalf("load the bundled fonts: %v", err)
	}

	var (
		buf         sfnt.Buffer
		unprintable []rune
		refused     []rune
	)
	for r := rune(0x20); r <= maxScanned; r++ {
		if !scanned(r) {
			continue
		}
		accepted := docpayload.RuneProblem(r, false) == ""
		printable := cov.covers(&buf, r)
		switch {
		case accepted && !printable:
			unprintable = append(unprintable, r)
		case !accepted && printable:
			refused = append(refused, r)
		}
	}

	if len(unprintable) > 0 {
		t.Errorf("the table accepts %d runes a bundled font has no glyph for, first %s",
			len(unprintable), formatRunes(unprintable))
	}
	if len(refused) > 0 {
		t.Errorf("the table refuses %d runes both bundled fonts can print, first %s",
			len(refused), formatRunes(refused))
	}
	if t.Failed() {
		t.Logf("table rebuilt from the fonts:\n%s", literal(coveredRanges(t)))
	}
}

// TestReplacementCharacterStaysRefused pins U+FFFD out of the table although
// the fonts have a glyph for it: it is what invalid UTF-8 decodes to, so
// accepting it would let broken input through issuing.
func TestReplacementCharacterStaysRefused(t *testing.T) {
	t.Parallel()

	for _, multiline := range []bool{false, true} {
		if docpayload.RuneProblem(unicode.ReplacementChar, multiline) == "" {
			t.Errorf("U+FFFD is accepted with multiline %t", multiline)
		}
	}
}

// formatRunes lists up to five runes as code points.
func formatRunes(rs []rune) string {
	const most = 5
	parts := make([]string, 0, most)
	for _, r := range rs {
		if len(parts) == most {
			break
		}
		parts = append(parts, fmt.Sprintf("%U", r))
	}

	return strings.Join(parts, ", ")
}
