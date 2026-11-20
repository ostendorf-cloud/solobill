package numbering_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/numbering"
)

func TestRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		template string
		year     int
		n        int64
		want     string
	}{
		{"RE-{year}{seq:4}", 2026, 9, "RE-20260009"},
		{"RE-{year}{seq:4}", 2026, 12345, "RE-202612345"},
		{"AN-{yy}/{seq}", 2026, 7, "AN-26/7"},
		{"R{{X}}-{year}-{seq:3}", 2026, 5, "R{X}-2026-005"},
		{"INV.{seq:2}_{year}", 2027, 3, "INV.03_2027"},
	}

	for _, tt := range tests {
		t.Run(tt.template, func(t *testing.T) {
			t.Parallel()

			tpl, err := numbering.Parse(tt.template)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			got, err := tpl.Render(tt.year, tt.n)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Render() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseRefuses(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"no sequence":           "RE-{year}",
		"two sequences":         "RE-{year}{seq}{seq:2}",
		"no year":               "RE-{seq:4}",
		"two years":             "RE-{year}{yy}{seq}",
		"unknown token":         "RE-{month}{year}{seq}",
		"width zero":            "RE-{year}{seq:0}",
		"width ten":             "RE-{year}{seq:10}",
		"no prefix":             "{year}-{seq}",
		"space":                 "RE {year}{seq}",
		"quote":                 `RE"{year}{seq}`,
		"backslash":             `RE\{year}{seq}`,
		"control character":     "RE\t{year}{seq}",
		"non ASCII":             "RÉ-{year}{seq}",
		"unclosed brace":        "RE-{year{seq}",
		"single closing brace":  "RE}-{year}{seq}",
		"longer than allowed":   strings.Repeat("A", 30) + "{year}{seq}",
		"empty":                 "",
		"literal brace no year": "RE{{year}}{seq}",
	}

	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := numbering.Parse(raw); !errors.Is(err, numbering.ErrTemplate) {
				t.Errorf("Parse(%q) error = %v, want ErrTemplate", raw, err)
			}
		})
	}
}

func TestValidateSet(t *testing.T) {
	t.Parallel()

	parse := func(raw string) numbering.Template {
		t.Helper()
		tpl, err := numbering.Parse(raw)
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", raw, err)
		}

		return tpl
	}

	defaults := map[string]numbering.Template{}
	for name, raw := range numbering.Defaults {
		defaults[name] = parse(raw)
	}
	if err := numbering.ValidateSet(defaults); err != nil {
		t.Errorf("ValidateSet(defaults) error = %v", err)
	}

	same := map[string]numbering.Template{"invoice": parse("RE-{year}{seq:4}"), "offer": parse("RE-{year}{seq:4}")}
	if err := numbering.ValidateSet(same); !errors.Is(err, numbering.ErrTemplate) {
		t.Errorf("ValidateSet(identical) error = %v, want ErrTemplate", err)
	}

	nested := map[string]numbering.Template{"invoice": parse("R{year}{seq}"), "offer": parse("RE-{year}{seq}")}
	if err := numbering.ValidateSet(nested); !errors.Is(err, numbering.ErrTemplate) {
		t.Errorf("ValidateSet(prefix of the other) error = %v, want ErrTemplate", err)
	}
}

func TestCorrectionAndFileName(t *testing.T) {
	t.Parallel()

	got, err := numbering.Correction("RE-20260009", 2)
	if err != nil || got != "RE-20260009-2" {
		t.Errorf("Correction() = %q, %v, want RE-20260009-2", got, err)
	}
	if _, err := numbering.Correction("RE-20260009", 0); err == nil {
		t.Error("Correction(seq 0) error = nil")
	}
	if _, err := numbering.Correction(strings.Repeat("A", 39), 1); err == nil {
		t.Error("Correction() longer than MaxDisplay error = nil")
	}

	if got := numbering.FileName("AN-26/7"); got != "AN-26_7.pdf" {
		t.Errorf("FileName() = %q, want AN-26_7.pdf", got)
	}
}
