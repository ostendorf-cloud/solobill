package euer

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
)

// CSVHeader is the header row of docs/api.md, "GET
// /api/v1/reports/euer/{year}/csv".
var CSVHeader = []string{"key", "zeile", "kennzahl", "operator", "label", "amount", "source_kind", "source_id", "source_amount"}

// WriteCSV writes the report as semicolon separated rows with a decimal
// comma: one row per source, so every figure traces to its rows. The
// line's amount repeats on each of its rows.
//
// CSV injection: a spreadsheet runs a cell that starts with "=", "+",
// "-", "@", a tab or a carriage return as a formula. Every text cell goes
// through NeutraliseCell, which puts a "'" in front of such a cell. The
// amount cells are the exception on purpose: FormatAmount builds them
// from an integer and they always match -?[0-9]+,[0-9]{2}, which a
// spreadsheet reads as a number, never as a formula. Neutralising them
// would turn every negative figure, a refund for example, into text.
func WriteCSV(w io.Writer, r Report) error {
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	if err := cw.Write(CSVHeader); err != nil {
		return fmt.Errorf("euer: write the csv header: %w", err)
	}
	for _, l := range r.Lines {
		for _, s := range l.Sources {
			row := []string{
				NeutraliseCell(string(l.Key)),
				NeutraliseCell(strconv.Itoa(l.Zeile)),
				NeutraliseCell(l.Kennzahl),
				NeutraliseCell(operatorWord(l.Operator)),
				NeutraliseCell(l.LabelDE),
				FormatAmount(l.AmountCents),
				NeutraliseCell(string(s.Kind)),
				NeutraliseCell(s.ID.String()),
				FormatAmount(s.AmountCents),
			}
			if err := cw.Write(row); err != nil {
				return fmt.Errorf("euer: write a csv row: %w", err)
			}
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("euer: write the csv: %w", err)
	}

	return nil
}

// operatorWord spells a line's operator as "minus" or "plus": a bare "-"
// or "+" cell would start like a formula.
func operatorWord(op string) string {
	switch op {
	case "-":
		return "minus"
	case "+":
		return "plus"
	default:
		return op
	}
}

// NeutraliseCell prefixes a cell that a spreadsheet would read as a
// formula with "'".
func NeutraliseCell(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}

	return s
}

// FormatAmount formats cents with a decimal comma and no thousands
// separator: -1250 is "-12,50".
func FormatAmount(cents int64) string {
	sign := ""
	// Negate in uint64, so the lowest int64 cannot overflow.
	abs := uint64(cents)
	if cents < 0 {
		sign = "-"
		abs = -abs
	}

	return fmt.Sprintf("%s%d,%02d", sign, abs/100, abs%100)
}
