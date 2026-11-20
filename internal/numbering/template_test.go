package numbering_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/numbering"
)

func mustParse(t *testing.T, raw string) numbering.Template {
	t.Helper()

	tpl, err := numbering.Parse(raw)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", raw, err)
	}

	return tpl
}

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
		{"_{year}{seq:9}", 2026, 1, "_2026000000001"},
	}

	for _, tt := range tests {
		t.Run(tt.template, func(t *testing.T) {
			t.Parallel()

			got, err := mustParse(t, tt.template).Render(tt.year, tt.n)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Render() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderRefuses(t *testing.T) {
	t.Parallel()

	tpl := mustParse(t, "RE-{year}{seq}")
	for name, args := range map[string]struct {
		year int
		n    int64
	}{
		"three digit year": {999, 1},
		"five digit year":  {10000, 1},
		"zero counter":     {2026, 0},
		"negative counter": {2026, -1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := tpl.Render(args.year, args.n); err == nil {
				t.Errorf("Render(%d, %d) error = nil", args.year, args.n)
			}
		})
	}

	// A prefix of 23 characters plus "2026" and nine digits is exactly 36;
	// a ten digit counter renders 37 and is refused at render time.
	edge := mustParse(t, strings.Repeat("A", 23)+"{year}{seq}")
	if got, err := edge.Render(2026, 999_999_999); err != nil || len(got) != numbering.MaxRendered {
		t.Errorf("Render(nine digits) = %q, %v, want %d characters", got, err, numbering.MaxRendered)
	}
	if _, err := edge.Render(2026, 1_000_000_000); err == nil {
		t.Error("Render(ten digits) beyond MaxRendered error = nil")
	}
}

func TestParseLengthBoundary(t *testing.T) {
	t.Parallel()

	if _, err := numbering.Parse(strings.Repeat("A", 23) + "{year}{seq}"); err != nil {
		t.Errorf("Parse(renders 36) error = %v, want nil", err)
	}
	if _, err := numbering.Parse(strings.Repeat("A", 24) + "{year}{seq}"); !errors.Is(err, numbering.ErrTemplate) {
		t.Errorf("Parse(renders 37) error = %v, want ErrTemplate", err)
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
		"width leading zero":    "RE-{year}{seq:04}",
		"width with sign":       "RE-{year}{seq:+4}",
		"width negative":        "RE-{year}{seq:-4}",
		"width empty":           "RE-{year}{seq:}",
		"width letter":          "RE-{year}{seq:a}",
		"no prefix":             "{year}-{seq}",
		"starts with dot":       ".RE{year}{seq}",
		"starts with hyphen":    "-RE{year}{seq}",
		"starts with slash":     "/RE{year}{seq}",
		"double dot":            "RE..{year}{seq}",
		"double slash":          "RE//{year}{seq}",
		"space":                 "RE {year}{seq}",
		"quote":                 `RE"{year}{seq}`,
		"backslash":             `RE\{year}{seq}`,
		"control character":     "RE\t{year}{seq}",
		"non ASCII":             "RÉ-{year}{seq}",
		"unclosed brace":        "RE-{year{seq}",
		"single closing brace":  "RE}-{year}{seq}",
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

	defaults := map[string]numbering.Template{}
	for name, raw := range numbering.Defaults {
		defaults[name] = mustParse(t, raw)
	}
	if err := numbering.ValidateSet(defaults); err != nil {
		t.Errorf("ValidateSet(defaults) error = %v", err)
	}

	for name, set := range map[string]map[string]numbering.Template{
		"identical":         {"invoice": mustParse(t, "RE-{year}{seq:4}"), "offer": mustParse(t, "RE-{year}{seq:4}")},
		"invoice in offer":  {"invoice": mustParse(t, "R{year}{seq}"), "offer": mustParse(t, "RE-{year}{seq}")},
		"offer in invoice":  {"invoice": mustParse(t, "RE-{year}{seq}"), "offer": mustParse(t, "R{year}{seq}")},
		"same short prefix": {"invoice": mustParse(t, "R{year}{seq}"), "offer": mustParse(t, "R{seq}{year}")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := numbering.ValidateSet(set); !errors.Is(err, numbering.ErrTemplate) {
				t.Errorf("ValidateSet() error = %v, want ErrTemplate", err)
			}
		})
	}
}

// TestValidateChange: after the first issued document only the prefix
// may change, to one that does not overlap the old prefix.
func TestValidateChange(t *testing.T) {
	t.Parallel()

	old := mustParse(t, "RE-{year}{seq}")
	tests := []struct {
		next string
		ok   bool
	}{
		{"RE-{year}{seq}", true},
		{"RG-{year}{seq}", true},
		{"RE-{seq}{year}", false},      // can render "RE-20261" from both
		{"RE-{year}{seq:4}", false},    // padding is not the prefix
		{"RE-{year}-{seq}", false},     // a literal after the prefix
		{"RE-1{year}{seq}", false},     // old prefix is a prefix of the new one
		{"R{year}{seq}", false},        // new prefix is a prefix of the old one
		{"INV-{year}{seq}", true},      // a fully different prefix
		{"INV-{yy}{seq}", false},       // year token changed
		{"RE-{year}{{x}}{seq}", false}, // literal brace added
	}

	for _, tt := range tests {
		t.Run(tt.next, func(t *testing.T) {
			t.Parallel()

			err := numbering.ValidateChange(old, mustParse(t, tt.next))
			if tt.ok && err != nil {
				t.Errorf("ValidateChange() error = %v, want nil", err)
			}
			if !tt.ok && !errors.Is(err, numbering.ErrTemplate) {
				t.Errorf("ValidateChange() error = %v, want ErrTemplate", err)
			}
		})
	}
}

func TestValidNumber(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"RE-2024-017", "R1234", "GS_2024/3", strings.Repeat("A", numbering.MaxRendered)} {
		if err := numbering.ValidNumber(ok); err != nil {
			t.Errorf("ValidNumber(%q) error = %v", ok, err)
		}
	}

	for name, bad := range map[string]string{
		"empty":            "",
		"too long":         strings.Repeat("A", numbering.MaxRendered+1),
		"space and quotes": `RE 2024 "17"`,
		"backslash":        `RE\17`,
		"semicolon":        "RE;17",
		"newline":          "RE\n17",
		"non ASCII":        "RÉ17",
		"leading dot":      ".RE17",
		"leading hyphen":   "-RE17",
		"leading slash":    "/RE17",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := numbering.ValidNumber(bad); !errors.Is(err, numbering.ErrNumber) {
				t.Errorf("ValidNumber(%q) error = %v, want ErrNumber", bad, err)
			}
		})
	}
}

func TestCorrection(t *testing.T) {
	t.Parallel()

	got, err := numbering.Correction("RE-20260009", 2)
	if err != nil || got != "RE-20260009-2" {
		t.Errorf("Correction() = %q, %v, want RE-20260009-2", got, err)
	}

	longest := strings.Repeat("A", numbering.MaxRendered)
	got, err = numbering.Correction(longest, numbering.MaxCorrectionSeq)
	if err != nil || len(got) != numbering.MaxDisplay {
		t.Errorf("Correction(longest, max) = %q, %v, want %d characters", got, err, numbering.MaxDisplay)
	}

	for name, args := range map[string]struct {
		original string
		seq      int
	}{
		"sequence zero":      {"RE-20260009", 0},
		"sequence above max": {"RE-20260009", numbering.MaxCorrectionSeq + 1},
		"sequence 10000":     {"RE-20260009", 10000},
		"hostile original":   {`RE 2024 "17"`, 1},
		"header injection":   {"RE17\r\nX-Evil: 1", 1},
		"original too long":  {strings.Repeat("A", numbering.MaxRendered+1), 1},
		"empty original":     {"", 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := numbering.Correction(args.original, args.seq); err == nil {
				t.Errorf("Correction(%q, %d) error = nil", args.original, args.seq)
			}
		})
	}
}

func TestFileName(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"AN-26/7":            "AN-26_7.pdf",
		"RE-20260009-1":      "RE-20260009-1.pdf",
		`RE 2024 "17"`:       "RE_2024__17_.pdf",
		"RE17\r\nX-Evil: 1":  "RE17__X-Evil__1.pdf",
		`a\b;c'd`:            "a_b_c_d.pdf",
		"":                   "document.pdf",
		"R{X}-2026-005":      "R_X_-2026-005.pdf",
		"../../etc/passwd":   ".._.._etc_passwd.pdf",
		"RÉ":                 "R__.pdf",
		"<script>x</script>": "_script_x__script_.pdf",
	}

	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			t.Parallel()

			got := numbering.FileName(in)
			if got != want {
				t.Errorf("FileName(%q) = %q, want %q", in, got, want)
			}
			for _, bad := range []string{`"`, `\`, "\r", "\n", ";", " ", "/"} {
				if strings.Contains(got, bad) {
					t.Errorf("FileName(%q) = %q still contains %q", in, got, bad)
				}
			}
		})
	}
}
