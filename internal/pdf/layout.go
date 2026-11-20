package pdf

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// The layout of document-v1 is exact by construction: every column has
// a fixed width (the description takes the rest), and every word is
// measured with the real advances of the bundled fonts before Typst
// runs. A word wider than its cell is refused, so no text can run over
// a neighbour cell, and the rows each description wraps to are counted
// the same way, so the page count is known before Typst runs. Kerning
// is ignored; widthMargin covers it. Every cell in the template also
// clips, as a last resort.
//
// The constants below are part of document-v1. templates/document-v1.typ
// holds the same values; TestLayoutConstantsMatchTemplate compares them.

// widthMargin widens every measured word by 3 percent for kerning and
// rounding.
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
	colQuantityMM  = 17.0
	colUnitMM      = 18.0
	colUnitPriceMM = 25.0
	colVATMM       = 13.0
	colAmountMM    = 30.0
	// tableSizePt is the item table's font size.
	tableSizePt = 8.0
	// discountSizePt is the size of a line's discount note.
	discountSizePt = 7.0
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

// tableCell is an item table column, its inset already taken off.
func tableCell(widthMM, sizePt float64, f face) cell {
	return cell{widthMM - 2*cellInsetMM, sizePt, f}
}

func cellDescription(showVAT bool) cell {
	return tableCell(descriptionWidthMM(showVAT), tableSizePt, regular)
}

func cellDiscount(showVAT bool) cell {
	return tableCell(descriptionWidthMM(showVAT), discountSizePt, regular)
}

var (
	cellPosition  = tableCell(colPositionMM, tableSizePt, regular)
	cellQuantity  = tableCell(colQuantityMM, tableSizePt, regular)
	cellUnit      = tableCell(colUnitMM, tableSizePt, regular)
	cellUnitPrice = tableCell(colUnitPriceMM, tableSizePt, regular)
	cellVAT       = tableCell(colVATMM, tableSizePt, regular)
	cellAmount    = tableCell(colAmountMM, tableSizePt, regular)
)

// tableHeaderCells are the header cells in column order, bold.
func tableHeaderCells(showVAT bool) []cell {
	cs := []cell{
		tableCell(colPositionMM, tableSizePt, bold),
		tableCell(descriptionWidthMM(showVAT), tableSizePt, bold),
		tableCell(colQuantityMM, tableSizePt, bold),
		tableCell(colUnitMM, tableSizePt, bold),
		tableCell(colUnitPriceMM, tableSizePt, bold),
	}
	if showVAT {
		cs = append(cs, tableCell(colVATMM, tableSizePt, bold))
	}

	return append(cs, tableCell(colAmountMM, tableSizePt, bold))
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

// words splits a line at the only characters Typst is sure to break at
// here: an ordinary space. A no-break space, a hyphen, a slash, "@" and
// "://" stay inside the word; Typst may break there or not, so the word
// is measured whole, which is the conservative choice.
//
// A space is no break opportunity either where Unicode line breaking
// (UAX #14) forbids one even after spaces: before closing punctuation,
// "!", "?", ",", ".", ":", ";" and "/" (LB13), after opening
// punctuation (LB14), and between two em dashes (LB17). Such a space
// joins its neighbours into one word. Two spaces in a row join as well.
func splitWords(line string) []string {
	var out []string
	cur := ""
	started := false
	for _, tok := range strings.Split(line, " ") {
		switch {
		case !started:
			cur, started = tok, true
		case tok == "" || cur == "" || noBreakBefore(tok) || noBreakAfter(cur) ||
			(strings.HasSuffix(cur, "—") && strings.HasPrefix(tok, "—")):
			cur += " " + tok
		default:
			out = append(out, cur)
			cur = tok
		}
	}

	return append(out, cur)
}

// noBreakBefore reports whether tok starts with a character UAX #14 never
// breaks before, even after spaces (LB13: CL, CP, EX, IS, SY).
func noBreakBefore(tok string) bool {
	r, _ := utf8.DecodeRuneInString(tok)

	return strings.ContainsRune(")]}!?,.:;/、。），．：；»›", r)
}

// noBreakAfter reports whether w ends with opening punctuation, after
// which UAX #14 never breaks, even across spaces (LB14: OP).
func noBreakAfter(w string) bool {
	r, _ := utf8.DecodeLastRuneInString(w)

	return strings.ContainsRune("([{„‚¡¿«‹", r)
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
			tooTall = append(tooTall, s.path)
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
