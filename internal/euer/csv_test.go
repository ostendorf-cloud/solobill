package euer_test

import (
	"encoding/csv"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ostendorf-cloud/solobill/internal/category"
	"github.com/ostendorf-cloud/solobill/internal/euer"
)

func TestNeutraliseCell(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"=1+1":              "'=1+1",
		"+49 30":            "'+49 30",
		"-2+3":              "'-2+3",
		"@SUM(A1)":          "'@SUM(A1)",
		"\tcmd":             "'\tcmd",
		"\rcmd":             "'\rcmd",
		"=HYPERLINK(\"x\")": "'=HYPERLINK(\"x\")",
		"Arbeitsmittel":     "Arbeitsmittel",
		"a=b":               "a=b",
		"":                  "",
		"'already":          "'already",
		"euer.z12":          "euer.z12",
		" =leading space":   " =leading space",
	}
	for in, want := range tests {
		if got := euer.NeutraliseCell(in); got != want {
			t.Errorf("NeutraliseCell(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatAmount(t *testing.T) {
	t.Parallel()

	tests := map[int64]string{
		0:             "0,00",
		5:             "0,05",
		-5:            "-0,05",
		1250:          "12,50",
		-1250:         "-12,50",
		2375000:       "23750,00",
		math.MinInt64: "-92233720368547758,08",
		math.MaxInt64: "92233720368547758,07",
	}
	for in, want := range tests {
		if got := euer.FormatAmount(in); got != want {
			t.Errorf("FormatAmount(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteCSV(t *testing.T) {
	t.Parallel()

	r := euer.Report{Lines: []euer.Line{
		{
			Key: category.IncomeKleinunternehmer, Zeile: 12, Kennzahl: "111", LabelDE: "Betriebseinnahmen als Kleinunternehmer",
			AmountCents: 22500,
			Sources: []euer.Source{
				{Kind: euer.SourcePayment, ID: id(1), Date: day("2026-03-01"), AmountCents: 23750},
				{Kind: euer.SourcePayment, ID: id(2), Date: day("2026-04-01"), AmountCents: -1250},
			},
		},
		{
			// A label no mapping ships, to prove every text cell passes
			// the neutralisation.
			Key: category.HomeOfficeDeductible, Zeile: 65, Kennzahl: "", LabelDE: "=cmd|' /C calc'!A0",
			AmountCents: -7,
			Sources:     []euer.Source{{Kind: euer.SourceExpense, ID: id(3), Date: time.Time{}, AmountCents: -7}},
		},
		{
			Key: category.CarCommuteCosts, Zeile: 72, Kennzahl: "142", Operator: "-", LabelDE: "Fahrtkosten",
			AmountCents: 120000,
			Sources:     []euer.Source{{Kind: euer.SourceExpense, ID: id(4), AmountCents: 120000}},
		},
		{
			Key: category.CarCommuteAllowance, Zeile: 73, Kennzahl: "176", Operator: "+", LabelDE: "Entfernungspauschale",
			AmountCents: 90000,
			Sources:     []euer.Source{{Kind: euer.SourceExpense, ID: id(5), AmountCents: 90000}},
		},
	}}

	var buf strings.Builder
	if err := euer.WriteCSV(&buf, r); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}

	// The bytes EF BB BF first, so Excel reads the umlauts as UTF-8.
	body, ok := strings.CutPrefix(buf.String(), "\xEF\xBB\xBF")
	if !ok {
		t.Fatalf("the csv starts with %q, want the UTF-8 byte order mark", buf.String()[:min(3, buf.Len())])
	}
	rd := csv.NewReader(strings.NewReader(body))
	rd.Comma = ';'
	rows, err := rd.ReadAll()
	if err != nil {
		t.Fatalf("read back: %v\n%s", err, buf.String())
	}
	want := [][]string{
		euer.CSVHeader,
		{"euer.z12", "12", "111", "", "Betriebseinnahmen als Kleinunternehmer", "225,00", "payment", id(1).String(), "237,50"},
		{"euer.z12", "12", "111", "", "Betriebseinnahmen als Kleinunternehmer", "225,00", "payment", id(2).String(), "-12,50"},
		{"euer.z65.d", "65", "", "", "'=cmd|' /C calc'!A0", "-0,07", "expense", id(3).String(), "-0,07"},
		{"euer.z72", "72", "142", "minus", "Fahrtkosten", "1200,00", "expense", id(4).String(), "1200,00"},
		{"euer.z73", "73", "176", "plus", "Entfernungspauschale", "900,00", "expense", id(5).String(), "900,00"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows =\n%q\nwant\n%q", rows, want)
	}
	for i := range want {
		if strings.Join(rows[i], "\x00") != strings.Join(want[i], "\x00") {
			t.Errorf("row %d = %q, want %q", i, rows[i], want[i])
		}
	}

	// Every cell that starts like a formula is neutralised, except the
	// amounts, which are plain numbers with a decimal comma.
	number := regexp.MustCompile(`^-?[0-9]+,[0-9]{2}$`)
	for i, row := range rows[1:] {
		for j, cell := range row {
			if j == 5 || j == 8 {
				if !number.MatchString(cell) {
					t.Errorf("row %d amount cell %q is no number", i+1, cell)
				}

				continue
			}
			if cell != "" && strings.ContainsRune("=+-@\t\r", rune(cell[0])) {
				t.Errorf("row %d cell %d = %q starts like a formula", i+1, j, cell)
			}
		}
	}
}
