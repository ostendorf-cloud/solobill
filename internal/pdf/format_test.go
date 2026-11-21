package pdf

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func indentJSON(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		t.Fatal(err)
	}
	buf.WriteByte('\n')

	return buf.Bytes()
}

func TestFormatCents(t *testing.T) {
	cases := []struct {
		cents int64
		lang  string
		want  string
	}{
		{0, "de", "0,00 EUR"},
		{5, "de", "0,05 EUR"},
		{123456, "de", "1.234,56 EUR"},
		{123456, "en", "1,234.56 EUR"},
		{100000000, "de", "1.000.000,00 EUR"},
		{-2500, "de", "-25,00 EUR"},
		{-123456, "en", "-1,234.56 EUR"},
		{99999, "de", "999,99 EUR"},
		{math.MinInt64, "en", "-92,233,720,368,547,758.08 EUR"},
	}
	for _, c := range cases {
		if got := formatCents(c.cents, "EUR", c.lang); got != c.want {
			t.Errorf("formatCents(%d, %s) = %q, want %q", c.cents, c.lang, got, c.want)
		}
	}
}

func TestFormatQuantityAndPercent(t *testing.T) {
	nbsp := string(noBreakSpace)
	cases := []struct {
		got, want string
	}{
		{formatQuantity(1000, "de"), "1"},
		{formatQuantity(1500, "de"), "1,5"},
		{formatQuantity(12250, "en"), "12.25"},
		{formatQuantity(1, "de"), "0,001"},
		{formatQuantity(1234000, "de"), "1.234"},
		{formatPercent(1900, "de"), "19" + nbsp + "%"},
		{formatPercent(700, "en"), "7%"},
		{formatPercent(1050, "de"), "10,5" + nbsp + "%"},
		{formatPercent(0, "de"), "0" + nbsp + "%"},
		{formatPercent(25, "en"), "0.25%"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestFormatDate(t *testing.T) {
	cases := []struct {
		in, lang, want string
	}{
		{"2026-12-05", "de", "05.12.2026"},
		{"2026-12-05", "en", strings.ReplaceAll("5 December 2026", " ", string(noBreakSpace))},
		{"2026-01-31", "en", strings.ReplaceAll("31 January 2026", " ", string(noBreakSpace))},
		{"not a date", "de", "not a date"},
	}
	for _, c := range cases {
		if got := formatDate(c.in, c.lang); got != c.want {
			t.Errorf("formatDate(%q, %s) = %q, want %q", c.in, c.lang, got, c.want)
		}
	}
}

func TestSplitLines(t *testing.T) {
	got := splitLines("a\r\nb\n\n")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("splitLines = %q", got)
	}
	if got := splitLines(""); len(got) != 0 {
		t.Errorf("splitLines(\"\") = %q", got)
	}
}
