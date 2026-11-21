package pdf

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
)

// The functions below format a payload value for print. They never
// compute a figure: every amount is printed as the payload holds it.

// numberStyle is the digit grouping and the decimal mark of a language.
type numberStyle struct {
	group   string
	decimal string
}

func styleFor(lang string) numberStyle {
	if lang == docpayload.LanguageGerman {
		return numberStyle{group: ".", decimal: ","}
	}

	return numberStyle{group: ",", decimal: "."}
}

// groupDigits inserts the group separator every three digits from the
// right of a string of decimal digits.
func groupDigits(digits, sep string) string {
	if len(digits) <= 3 {
		return digits
	}
	var b strings.Builder
	head := len(digits) % 3
	if head > 0 {
		b.WriteString(digits[:head])
	}
	for i := head; i < len(digits); i += 3 {
		if b.Len() > 0 {
			b.WriteString(sep)
		}
		b.WriteString(digits[i : i+3])
	}

	return b.String()
}

// formatCents prints an amount in cents with the currency code after it:
// "1.234,56 EUR" in German, "1,234.56 EUR" in English.
func formatCents(cents int64, currency, lang string) string {
	s := styleFor(lang)
	sign := ""
	// The magnitude is taken as uint64, so the most negative int64 does
	// not overflow.
	abs := uint64(cents)
	if cents < 0 {
		sign = "-"
		abs = uint64(-(cents + 1)) + 1
	}
	whole := strconv.FormatUint(abs/100, 10)
	frac := fmt.Sprintf("%02d", abs%100)

	return sign + groupDigits(whole, s.group) + s.decimal + frac + " " + currency
}

// formatFixed prints value/10^places with trailing fractional zeros
// removed: formatFixed(1500, 3) is "1,5" in German.
func formatFixed(value int64, places int, lang string) string {
	s := styleFor(lang)
	sign := ""
	abs := uint64(value)
	if value < 0 {
		sign = "-"
		abs = uint64(-(value + 1)) + 1
	}
	div := uint64(1)
	for range places {
		div *= 10
	}
	whole := groupDigits(strconv.FormatUint(abs/div, 10), s.group)
	frac := strings.TrimRight(fmt.Sprintf("%0*d", places, abs%div), "0")
	if frac == "" {
		return sign + whole
	}

	return sign + whole + s.decimal + frac
}

// formatQuantity prints a quantity in thousandths.
func formatQuantity(thousandths int64, lang string) string {
	return formatFixed(thousandths, 3, lang)
}

// noBreakSpace (U+00A0) keeps a number and its percent sign on one line.
// Written as a rune value so the source holds no invisible character.
const noBreakSpace rune = 0xA0

// formatPercent prints a rate in permyriad: 1900 is "19 %" in German
// (DIN 5008 sets a no-break space) and "19%" in English.
func formatPercent(permyriad int64, lang string) string {
	n := formatFixed(permyriad, 2, lang)
	if lang == docpayload.LanguageGerman {
		return n + string(noBreakSpace) + "%"
	}

	return n + "%"
}

var monthsEnglish = [...]string{
	"January", "February", "March", "April", "May", "June",
	"July", "August", "September", "October", "November", "December",
}

// formatDate prints a docpayload.DateLayout date: "05.12.2026" in German,
// "5 December 2026" in English, with no-break spaces so a date never
// wraps inside. An unparsable date is printed unchanged, so a renderer
// never hides what the payload holds.
func formatDate(s, lang string) string {
	t, err := time.Parse(docpayload.DateLayout, s)
	if err != nil {
		return s
	}
	if lang == docpayload.LanguageGerman {
		return t.Format("02.01.2006")
	}

	return fmt.Sprintf("%d%c%s%c%d", t.Day(), noBreakSpace, monthsEnglish[t.Month()-1], noBreakSpace, t.Year())
}

// groupIBAN prints an IBAN in groups of four characters (DIN 5008), so
// it never forms a token long enough to get a break opportunity.
func groupIBAN(s string) string {
	compact := strings.Join(strings.Fields(s), "")
	var b strings.Builder
	for i, r := range []rune(compact) {
		if i > 0 && i%4 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}

	return b.String()
}

// splitLines splits free text into printed lines. Carriage returns are
// dropped, a tab prints as a space (so the layout check measures exactly
// what is printed) and trailing blank lines are removed.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.TrimRight(s, "\n ")
	if s == "" {
		return []string{}
	}

	return strings.Split(s, "\n")
}
