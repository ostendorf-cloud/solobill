// Package numbering parses, validates and renders the display number
// templates of the seller profile (docs/concepts/solobill.md, "Number
// format"). Pure: no database, no clock, no I/O.
//
// Grammar:
//
//   - {year} is the four digit number year, {yy} its last two digits.
//   - {seq} is the counter value without padding, {seq:N} padded with
//     zeros to at least N digits, N from 1 to 9.
//   - {{ and }} are a literal { and }.
//   - Every other character is a literal from the allowed set: ASCII
//     letters, digits, "-", "_", "." and "/". Nothing else: no space, no
//     control character, no quote, no backslash.
//   - Exactly one sequence token and exactly one year token.
//   - The template starts with a literal prefix of at least one
//     character, so the prefixes of two document types can be kept apart.
//
// A rendered number is at most MaxRendered characters, so a correction
// suffix ("-1" to "-999") still fits into MaxDisplay.
package numbering

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// MaxDisplay is the longest display number solobill stores, corrections
// included.
const MaxDisplay = 40

// MaxRendered is the longest number a template may render, leaving room
// for a correction suffix of up to four characters.
const MaxRendered = MaxDisplay - 4

// worstSeq is the counter value a template is checked against at save:
// nine digits, far beyond any real yearly count.
const worstSeq = 999_999_999

// ErrTemplate wraps every template validation error.
var ErrTemplate = errors.New("numbering: invalid template")

type tokenKind int

const (
	literal tokenKind = iota
	year
	yearShort
	seq
)

type part struct {
	kind  tokenKind
	text  string
	width int
}

// Template is a parsed, valid template.
type Template struct {
	raw    string
	parts  []part
	prefix string
}

// Prefix is the literal text before the first token.
func (t Template) Prefix() string { return t.prefix }

// String is the template as written.
func (t Template) String() string { return t.raw }

// Parse validates raw and returns the parsed template. Every error wraps
// ErrTemplate and names the position.
func Parse(raw string) (Template, error) {
	t := Template{raw: raw}
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			t.parts = append(t.parts, part{kind: literal, text: lit.String()})
			lit.Reset()
		}
	}

	seqs, years := 0, 0
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c == '{' && i+1 < len(raw) && raw[i+1] == '{':
			lit.WriteByte('{')
			i++
		case c == '}' && i+1 < len(raw) && raw[i+1] == '}':
			lit.WriteByte('}')
			i++
		case c == '{':
			end := strings.IndexByte(raw[i:], '}')
			if end < 0 {
				return Template{}, fmt.Errorf("%w: unclosed { at %d", ErrTemplate, i)
			}
			flush()
			p, err := parseToken(raw[i+1 : i+end])
			if err != nil {
				return Template{}, fmt.Errorf("%w: %w at %d", ErrTemplate, err, i)
			}
			if p.kind == seq {
				seqs++
			} else {
				years++
			}
			t.parts = append(t.parts, p)
			i += end
		case c == '}':
			return Template{}, fmt.Errorf("%w: single } at %d, write }} for a literal brace", ErrTemplate, i)
		case allowed(c):
			lit.WriteByte(c)
		default:
			return Template{}, fmt.Errorf("%w: character %q at %d is not allowed", ErrTemplate, c, i)
		}
	}
	flush()

	if seqs != 1 {
		return Template{}, fmt.Errorf("%w: needs exactly one {seq} or {seq:N}, has %d", ErrTemplate, seqs)
	}
	if years != 1 {
		return Template{}, fmt.Errorf("%w: needs exactly one {year} or {yy}, has %d", ErrTemplate, years)
	}
	if len(t.parts) == 0 || t.parts[0].kind != literal {
		return Template{}, fmt.Errorf("%w: must start with a literal prefix", ErrTemplate)
	}
	t.prefix = t.parts[0].text

	worst, err := t.render(2026, worstSeq)
	if err != nil {
		return Template{}, err
	}
	if len(worst) > MaxRendered {
		return Template{}, fmt.Errorf("%w: renders up to %d characters, at most %d allowed", ErrTemplate, len(worst), MaxRendered)
	}

	return t, nil
}

func parseToken(name string) (part, error) {
	switch {
	case name == "year":
		return part{kind: year}, nil
	case name == "yy":
		return part{kind: yearShort}, nil
	case name == "seq":
		return part{kind: seq}, nil
	case strings.HasPrefix(name, "seq:"):
		n, err := strconv.Atoi(strings.TrimPrefix(name, "seq:"))
		if err != nil || n < 1 || n > 9 {
			return part{}, fmt.Errorf("{%s}: the width must be 1 to 9", name)
		}

		return part{kind: seq, width: n}, nil
	default:
		return part{}, fmt.Errorf("unknown token {%s}", name)
	}
}

func allowed(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '-', c == '_', c == '.', c == '/':
		return true
	default:
		return false
	}
}

// Render renders the number of counter value n in numberYear.
func (t Template) Render(numberYear int, n int64) (string, error) {
	s, err := t.render(numberYear, n)
	if err != nil {
		return "", err
	}
	if len(s) > MaxRendered {
		return "", fmt.Errorf("numbering: %q is longer than %d characters", s, MaxRendered)
	}

	return s, nil
}

func (t Template) render(numberYear int, n int64) (string, error) {
	if numberYear < 1000 || numberYear > 9999 {
		return "", fmt.Errorf("numbering: year %d is not four digits", numberYear)
	}
	if n < 1 {
		return "", fmt.Errorf("numbering: counter value %d must be positive", n)
	}

	var b strings.Builder
	for _, p := range t.parts {
		switch p.kind {
		case literal:
			b.WriteString(p.text)
		case year:
			b.WriteString(strconv.Itoa(numberYear))
		case yearShort:
			fmt.Fprintf(&b, "%02d", numberYear%100)
		case seq:
			fmt.Fprintf(&b, "%0*d", p.width, n)
		}
	}

	return b.String(), nil
}

// Correction renders the number of correction seq of the document whose
// display number is original: the original plus "-" and seq.
func Correction(original string, seq int) (string, error) {
	if seq < 1 || seq > 999 {
		return "", fmt.Errorf("numbering: correction sequence %d must be 1 to 999", seq)
	}
	s := original + "-" + strconv.Itoa(seq)
	if len(s) > MaxDisplay {
		return "", fmt.Errorf("numbering: %q is longer than %d characters", s, MaxDisplay)
	}

	return s, nil
}

// ValidateSet checks the templates of all document types together: no
// two may ever render the same string. It requires that no type's
// literal prefix is a prefix of another type's, which makes the rendered
// numbers differ at the latest at the end of the shorter prefix. The
// map keys are document type names, used in the error.
func ValidateSet(templates map[string]Template) error {
	names := make([]string, 0, len(templates))
	for name := range templates {
		names = append(names, name)
	}
	for i, a := range names {
		for _, b := range names[i+1:] {
			pa, pb := templates[a].Prefix(), templates[b].Prefix()
			if strings.HasPrefix(pa, pb) || strings.HasPrefix(pb, pa) {
				return fmt.Errorf("%w: the prefixes of %s (%q) and %s (%q) overlap, so their numbers could collide",
					ErrTemplate, a, pa, b, pb)
			}
		}
	}

	return nil
}

// Defaults are the templates of a profile that saved none. They differ
// in their prefix, so an invoice and an offer never share a number.
var Defaults = map[string]string{
	"invoice": "RE-{year}{seq:4}",
	"offer":   "AN-{year}{seq:4}",
}

// FileName is the display number as a file name: "/" becomes "_" and
// ".pdf" is appended. The allowed character set leaves nothing else to
// escape; the caller still quotes it per RFC 6266 in a header.
func FileName(display string) string {
	return strings.ReplaceAll(display, "/", "_") + ".pdf"
}
