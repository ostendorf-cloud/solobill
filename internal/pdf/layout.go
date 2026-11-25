package pdf

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Typst is the authority on the layout: the template marks every printed
// character and fails the compile with "overflow: <field path>" when one
// lies outside its cell (see templates/document-v1.typ). This file is
// the fast pre check that runs before Typst and names the field. It
// must be conservative: whatever it accepts, Typst must lay out without
// overflow. Every column has a fixed width (the description takes the
// rest), and every word is measured with the real advances of the
// bundled fonts. Measured against Typst, the model's widths match
// exactly: Typst applies no kerning with the Go fonts. widthMargin adds
// 3 percent anyway. The rows each description wraps to are counted the
// same way, so the page count is bounded before Typst runs.
//
// The constants below are part of document-v1, and document-v2 keeps
// them: its logo sits outside the flow. Both templates hold the same
// values; TestLayoutConstantsMatchTemplate compares them with the
// current one.

// widthMargin widens every measured word by 3 percent.
const widthMargin = 1.03

// ptPerMM converts millimetres to points.
const ptPerMM = 72 / 25.4

// face is one bundled font.
type face int

const (
	regular face = iota
	bold
)

// cell is a box text is set into: its usable width and its font.
type cell struct {
	widthMM float64
	sizePt  float64
	face    face
}

// document-v1 page geometry in millimetres.
const (
	pageHeightMM = 297.0
	textWidthMM  = 165.0
	topMarginMM  = 18.0
	// The bottom margin is the measured footer plus this.
	bottomExtraMM = 18.0
	// cellInsetMM is the item table's horizontal and vertical inset.
	cellInsetMM  = 1.5
	cellInsetYMM = 1.6
)

// Item table column widths of document-v1 in millimetres. The
// description takes what is left of textWidthMM.
const (
	colPositionMM  = 10.0
	colQuantityMM  = 16.5
	colUnitMM      = 17.5
	colUnitPriceMM = 25.0
	colVATMM       = 12.5
	colAmountMM    = 27.5
	// tableSizePt is the item table's font size.
	tableSizePt = 8.0
	// discountSizePt is the size of a line's discount note.
	discountSizePt = 8.0
)

// Other fixed widths of document-v1 in millimetres.
const (
	metaWidthMM       = 88.0
	metaLabelMM       = 32.0
	metaGutterMM      = 3.0
	addressGutterMM   = 8.0
	totalsWidthMM     = 100.0
	totalsValueMM     = 40.0
	totalsGutterMM    = 4.0
	footerGutterMM    = 6.0
	footerColumnCount = 2
	// leadingEm is the template's leading. A line's box spans the font's
	// ascender to its descender (1.156 em for the Go fonts), so a row is
	// 1.306 em; the estimate uses lineHeightFactor, more.
	leadingEm        = 0.15
	lineHeightFactor = 1.4
)

// descriptionWidthMM is the description column's width.
func descriptionWidthMM(showVAT bool) float64 {
	w := textWidthMM - colPositionMM - colQuantityMM - colUnitMM - colUnitPriceMM - colAmountMM
	if showVAT {
		w -= colVATMM
	}

	return w
}

// The cells of document-v1.
var (
	cellLetterhead       = cell{textWidthMM, 14, bold}
	cellSender           = cell{textWidthMM, 6.5, regular}
	cellAddress          = cell{textWidthMM - metaWidthMM - addressGutterMM, 10, regular}
	cellMetaValue        = cell{metaWidthMM - metaLabelMM - metaGutterMM, 9, regular}
	cellMetaLabel        = cell{metaLabelMM, 9, regular}
	cellFullText         = cell{textWidthMM, 9, regular}
	cellFooter           = cell{(textWidthMM - footerGutterMM*(footerColumnCount-1)) / footerColumnCount, 7, regular}
	cellFooterText       = cell{textWidthMM, 7, regular}
	cellTotalLabel       = cell{totalsWidthMM - totalsValueMM - totalsGutterMM, 9, regular}
	cellTotalValue       = cell{totalsValueMM, 9, regular}
	cellTotalStrong      = cell{totalsValueMM, 10, bold}
	cellTotalStrongLabel = cell{totalsWidthMM - totalsValueMM - totalsGutterMM, 10, bold}
)

// tableCell is an item table column at the table's font size, its inset
// already taken off.
func tableCell(widthMM float64, f face) cell {
	return cell{widthMM - 2*cellInsetMM, tableSizePt, f}
}

func cellDescription(showVAT bool) cell {
	return tableCell(descriptionWidthMM(showVAT), regular)
}

func cellDiscount(showVAT bool) cell {
	return cell{descriptionWidthMM(showVAT) - 2*cellInsetMM, discountSizePt, regular}
}

var (
	cellPosition  = tableCell(colPositionMM, regular)
	cellQuantity  = tableCell(colQuantityMM, regular)
	cellUnit      = tableCell(colUnitMM, regular)
	cellUnitPrice = tableCell(colUnitPriceMM, regular)
	cellVAT       = tableCell(colVATMM, regular)
	cellAmount    = tableCell(colAmountMM, regular)
)

// tableHeaderCells are the header cells in column order, bold.
func tableHeaderCells(showVAT bool) []cell {
	cs := []cell{
		tableCell(colPositionMM, bold),
		tableCell(descriptionWidthMM(showVAT), bold),
		tableCell(colQuantityMM, bold),
		tableCell(colUnitMM, bold),
		tableCell(colUnitPriceMM, bold),
	}
	if showVAT {
		cs = append(cs, tableCell(colVATMM, bold))
	}

	return append(cs, tableCell(colAmountMM, bold))
}

// metrics measures text with the bundled fonts' advances.
type metrics struct {
	fonts [2]*sfnt.Font
	mu    sync.Mutex
	cache map[face]map[rune]float64
	buf   sfnt.Buffer
}

var loadMetrics = sync.OnceValues(func() (*metrics, error) {
	cov, err := loadCoverage()
	if err != nil {
		return nil, err
	}

	return &metrics{
		fonts: [2]*sfnt.Font{cov.fonts[0], cov.fonts[1]},
		cache: map[face]map[rune]float64{regular: {}, bold: {}},
	}, nil
})

// em is r's advance in em. A rune without a glyph measures as a wide
// one; the printability check refuses it anyway.
func (m *metrics) em(f face, r rune) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w, ok := m.cache[f][r]; ok {
		return w
	}
	fnt := m.fonts[f]
	upem := fnt.UnitsPerEm()
	w := 1.0
	if g, err := fnt.GlyphIndex(&m.buf, r); err == nil && g != 0 {
		if adv, err := fnt.GlyphAdvance(&m.buf, g, fixed.I(int(upem)), font.HintingNone); err == nil {
			w = float64(adv) / 64 / float64(upem)
		}
	}
	m.cache[f][r] = w

	return w
}

// widthMM is the width of s set in c, with widthMargin.
func (m *metrics) widthMM(s string, c cell) float64 {
	var em float64
	for _, r := range s {
		em += m.em(c.face, r)
	}

	return em * c.sizePt / ptPerMM * widthMargin
}

// splitWords splits a line conservatively: at a run of ordinary spaces
// only when a letter or a digit follows it, and not when opening or
// quotation punctuation precedes it. Unicode line breaking (UAX #14)
// allows a break after spaces before a letter or digit, except after
// opening punctuation (LB14: classes Ps and the opening Pi or Pf, and
// "¡", "¿", "⸘", which are opening there although their category is
// Po) and around quotation marks (LB15a, LB19). Everything else stays
// in one word: punctuation before a space (", ", ": ", ") "), no-break
// spaces, hyphens, slashes, "@" and "://". Typst breaks at least
// wherever this splits, so a word that fits here fits in Typst.
// Typst decides the rest, and refuses with a field path when it does
// not fit.
func splitWords(line string) []string {
	var out []string
	start := 0
	for i := 0; i < len(line); {
		if line[i] != ' ' {
			_, n := utf8.DecodeRuneInString(line[i:])
			i += n

			continue
		}
		j := i
		for j < len(line) && line[j] == ' ' {
			j++
		}
		before, _ := utf8.DecodeLastRuneInString(line[:i])
		after, _ := utf8.DecodeRuneInString(line[j:])
		if i > start && j < len(line) && !isOpening(before) && isLetterOrDigit(after) {
			out = append(out, line[start:i])
			start = j
		}
		i = j
	}

	return append(out, line[start:])
}

// isOpening reports opening and quotation punctuation, after which a
// space is no break opportunity.
func isOpening(r rune) bool {
	return r == '¡' || r == '¿' || r == '⸘' || unicode.In(r, unicode.Ps, unicode.Pi, unicode.Pf, unicode.Quotation_Mark)
}

func isLetterOrDigit(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// tooWide returns the first word of s wider than c, or "".
func (m *metrics) tooWide(s string, c cell) string {
	for _, line := range strings.Split(s, "\n") {
		for _, w := range splitWords(line) {
			if m.widthMM(w, c) > c.widthMM {
				return w
			}
		}
	}

	return ""
}

// rows is the number of rows s wraps to in c: greedy, as Typst breaks
// a line at the last space that fits. An empty line is one row. Every
// word must fit (tooWide), otherwise it counts as one row on its own.
func (m *metrics) rows(s string, c cell) int {
	space := m.widthMM(" ", c)
	n := 0
	for _, line := range strings.Split(s, "\n") {
		n++
		used := 0.0
		for i, w := range splitWords(line) {
			ww := m.widthMM(w, c)
			switch {
			case i == 0:
				used = ww
			case used+space+ww <= c.widthMM:
				used += space + ww
			default:
				n++
				used = ww
			}
		}
	}

	return n
}

// heightMM is the height of rows lines of text at size, generously:
// lineHeightFactor times the font size per row.
func heightMM(rows int, sizePt float64) float64 {
	return float64(rows) * sizePt * lineHeightFactor / ptPerMM
}

// estimatePages lays out in on paper, generously, and returns an upper
// bound of its pages and the paths of the notes taller than one page.
// Every height is overestimated: lineHeightFactor per row, a slack per
// block, an item that does not fit the rest of a page starts a new one.
func (m *metrics) estimatePages(in Input) (int, []string) {
	join := func(lines []string) string { return strings.Join(lines, "\n") }
	rowsOf := func(lines []string, c cell) int {
		if len(lines) == 0 {
			return 0
		}

		return m.rows(join(lines), c)
	}
	const slackMM = 3.0

	// The footer, measured like the template measures it.
	footerRows := 0
	for i := 0; i < len(in.Footer); i += footerColumnCount {
		r := 0
		for j := i; j < i+footerColumnCount && j < len(in.Footer); j++ {
			r = max(r, rowsOf(in.Footer[j], cellFooter))
		}
		footerRows += r
	}
	footerRows += rowsOf(in.FooterText, cellFooterText)
	footerMM := heightMM(footerRows, 7) + 1.5*float64(len(in.Footer)/footerColumnCount) + 2*slackMM
	bodyMM := pageHeightMM - topMarginMM - bottomExtraMM - footerMM
	if bodyMM <= 0 {
		return math.MaxInt32, nil
	}

	// The head of the first page.
	metaMM := 0.0
	for _, p := range in.Meta {
		metaMM += heightMM(max(m.rows(p.Label, cellMetaLabel), m.rows(p.Value, cellMetaValue)), 9) + 1.6
	}
	headMM := heightMM(m.rows(in.SellerName, cellLetterhead), 14) + 18 +
		heightMM(m.rows(in.SenderLine, cellSender), 6.5) + 1 +
		max(heightMM(rowsOf(in.RecipientLines, cellAddress), 10), metaMM) + 14 +
		heightMM(1, 16) + heightMM(m.rows(in.CorrectsLine, cellFullText), 9) + 3 + 3*slackMM

	headers := []string{
		in.Columns.Position, in.Columns.Description, in.Columns.Quantity, in.Columns.Unit, in.Columns.UnitPrice,
	}
	if in.ShowVAT {
		headers = append(headers, in.Columns.VATRate)
	}
	headers = append(headers, in.Columns.Amount)
	headerRows := 1
	for i, c := range tableHeaderCells(in.ShowVAT) {
		headerRows = max(headerRows, m.rows(headers[i], c))
	}
	tableHeaderMM := heightMM(headerRows, tableSizePt) + 2*cellInsetYMM

	pages := 1
	used := headMM
	place := func(h float64, repeatHeader bool) {
		if used+h <= bodyMM {
			used += h

			return
		}
		// A block taller than a page continues over several pages.
		pages++
		used = 0
		if repeatHeader {
			used = tableHeaderMM
		}
		for used+h > bodyMM {
			h -= bodyMM - used
			pages++
			used = 0
			if repeatHeader {
				used = tableHeaderMM
			}
		}
		used += h
	}

	place(tableHeaderMM, false)
	for _, l := range in.Lines {
		h := heightMM(rowsOf(l.Description, cellDescription(in.ShowVAT)), tableSizePt)
		if l.Discount != "" {
			h += heightMM(m.rows(l.Discount, cellDiscount(in.ShowVAT)), discountSizePt)
		}
		h = max(h, heightMM(m.rows(l.Unit, cellUnit), tableSizePt), heightMM(1, tableSizePt))
		place(h+2*cellInsetYMM, true)
	}

	totalsMM := 2.0
	for _, t := range in.Totals {
		size, label := 9.0, cellTotalLabel
		if t.Strong {
			size, label = 10, cellTotalStrongLabel
		}
		totalsMM += heightMM(m.rows(t.Label, label), size) + 3.6
	}
	place(totalsMM, false)

	var tooTall []string
	for _, s := range in.Sections {
		h := heightMM(rowsOf(append([]string{s.Title}, s.Lines...), cellFullText), 9) + slackMM
		if h > bodyMM {
			tooTall = append(tooTall, s.Path)
		}
		place(h, false)
	}

	return pages, tooTall
}

// shortWord abbreviates a refused word for an error message.
func shortWord(w string) string {
	if utf8.RuneCountInString(w) <= 24 {
		return w
	}
	r := []rune(w)

	return string(r[:21]) + "..."
}

// tooWideReason is the reason for a word wider than its cell.
func tooWideReason(w string, c cell) string {
	return fmt.Sprintf("word too wide for the cell: %q needs more than %.0f mm", shortWord(w), math.Floor(c.widthMM))
}
