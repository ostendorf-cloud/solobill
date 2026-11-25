package euer

import (
	"slices"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/category"
)

func TestEveryKeyIsMappedInEveryYear(t *testing.T) {
	t.Parallel()

	years := MappingYears()
	if !slices.Equal(years, []int{2025, 2026}) {
		t.Fatalf("mapping years = %v, want 2025 and 2026", years)
	}
	for _, y := range years {
		m, _ := MappingFor(y)
		for _, c := range category.All() {
			l, ok := m.Line(c.Key)
			if !ok {
				t.Errorf("%d: key %q has no mapping row", y, c.Key)

				continue
			}
			// The keys name the 2025 lines; the copy keeps them.
			if l.Zeile != category.Zeile(c.Key) {
				t.Errorf("%d: %q maps to Zeile %d, the key names %d", y, c.Key, l.Zeile, category.Zeile(c.Key))
			}
		}
		if len(m.Lines) != len(category.All()) {
			t.Errorf("%d: %d mapping rows for %d keys", y, len(m.Lines), len(category.All()))
		}
	}
}

func TestMapping2025IsVerified(t *testing.T) {
	t.Parallel()

	m, ok := MappingFor(2025)
	if !ok || m.Status != StatusVerified || !strings.Contains(m.Source, "29 August 2025") {
		t.Fatalf("2025 = %v %q %q, want verified against the letter of 29 August 2025", ok, m.Status, m.Source)
	}
	for _, l := range m.Lines {
		if !l.Verified || l.Kennzahl == "" {
			t.Errorf("2025 %q verified = %v, Kennzahl %q", l.Key, l.Verified, l.Kennzahl)
		}
	}

	// The lines the contract left open, as the form prints them
	// (docs/concepts/euer-keys/80.md): Kz 162 in the column "nicht
	// abziehbar" and 172 in "abziehbar", like 164 and 174 on Zeile 62;
	// Kz 111 on the amount field of Zeile 12. And the two car lines the
	// form prints with an operator.
	want := map[category.Key]struct{ kz, op string }{
		category.HomeOfficeNonDeductible: {"162", ""},
		category.HomeOfficeDeductible:    {"172", ""},
		category.IncomeKleinunternehmer:  {"111", ""},
		category.CarCommuteCosts:         {"142", "-"},
		category.CarCommuteAllowance:     {"176", "+"},
		category.CarLeasing:              {"144", ""},
	}
	for k, w := range want {
		if l, _ := m.Line(k); l.Kennzahl != w.kz || l.Operator != w.op {
			t.Errorf("%q = Kz %q operator %q, want Kz %q operator %q", k, l.Kennzahl, l.Operator, w.kz, w.op)
		}
	}
}

func TestMapping2026IsAnUnverifiedCopyOf2025(t *testing.T) {
	t.Parallel()

	m25, _ := MappingFor(2025)
	m26, ok := MappingFor(2026)
	if !ok || m26.Status != StatusUnverified || m26.Source != "copy of 2025" {
		t.Fatalf("2026 = %v %q %q, want an unverified copy of 2025", ok, m26.Status, m26.Source)
	}
	if len(m25.Lines) != len(m26.Lines) {
		t.Fatalf("2026 has %d lines, 2025 has %d", len(m26.Lines), len(m25.Lines))
	}
	for i, l := range m26.Lines {
		want := m25.Lines[i]
		if l.Key != want.Key || l.Zeile != want.Zeile || l.Kennzahl != want.Kennzahl || l.Operator != want.Operator || l.Note != want.Note {
			t.Errorf("2026 line %d = %+v, 2025 has %+v", i, l, want)
		}
		if l.Verified {
			t.Errorf("2026 %q is verified", l.Key)
		}
	}
}

func TestMappingForReturnsACopy(t *testing.T) {
	t.Parallel()

	m, _ := MappingFor(2026)
	m.Lines[0].Kennzahl = "999"
	if again, _ := MappingFor(2026); again.Lines[0].Kennzahl == "999" {
		t.Error("MappingFor exposes the package's own lines")
	}
}

func TestParseMappingRefuses(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"unknown status":                      `{"tax_year": 2025, "status": "maybe", "lines": []}`,
		"unknown field":                       `{"tax_year": 2025, "status": "verified", "lines": [], "extra": 1}`,
		"unknown key":                         `{"tax_year": 2025, "status": "verified", "lines": [{"key": "euer.z999", "zeile": 999, "kennzahl": "1"}]}`,
		"key twice":                           `{"tax_year": 2025, "status": "verified", "lines": [{"key": "euer.z27", "zeile": 27, "kennzahl": "100"}, {"key": "euer.z27", "zeile": 27, "kennzahl": "100"}]}`,
		"no zeile":                            `{"tax_year": 2025, "status": "verified", "lines": [{"key": "euer.z27", "zeile": 0, "kennzahl": "100"}]}`,
		"letters":                             `{"tax_year": 2025, "status": "verified", "lines": [{"key": "euer.z27", "zeile": 27, "kennzahl": "1a"}]}`,
		"verified, no kz":                     `{"tax_year": 2025, "status": "verified", "lines": [{"key": "euer.z27", "zeile": 27, "kennzahl": "", "verified": true}]}`,
		"verified line in an unverified year": `{"tax_year": 2026, "status": "unverified", "lines": [{"key": "euer.z27", "zeile": 27, "kennzahl": "100", "verified": true}]}`,
		"unknown operator":                    `{"tax_year": 2025, "status": "verified", "lines": [{"key": "euer.z72", "zeile": 72, "kennzahl": "142", "operator": "minus"}]}`,
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := parseMapping([]byte(data)); err == nil {
				t.Error("accepted")
			}
		})
	}
}
