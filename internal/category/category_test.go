package category_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/category"
)

// keyPattern is the only shape a key may have: a line of the Anlage EÜR
// and an optional deductibility suffix. A form number or a Kennzahl
// never matches it.
var keyPattern = regexp.MustCompile(`^euer\.z[0-9]{1,3}(\.(d|nd))?$`)

func TestKeysAreUniqueAndWellFormed(t *testing.T) {
	t.Parallel()

	seen := map[category.Key]bool{}
	for _, c := range category.All() {
		if seen[c.Key] {
			t.Errorf("key %q appears twice", c.Key)
		}
		seen[c.Key] = true

		if !keyPattern.MatchString(string(c.Key)) {
			t.Errorf("key %q does not match %s", c.Key, keyPattern)
		}
		if c.LabelDE == "" || c.LabelEN == "" {
			t.Errorf("key %q lacks a German or an English label", c.Key)
		}
		switch c.Kind {
		case category.KindIncome, category.KindExpense, category.KindAsset, category.KindDerived:
		default:
			t.Errorf("key %q has unknown kind %q", c.Key, c.Kind)
		}
	}
}

// TestEveryKeyIsDocumented keeps the concept's key table and this
// package in step: a key nobody documented is a key nobody can map to a
// form line.
func TestEveryKeyIsDocumented(t *testing.T) {
	t.Parallel()

	concept, err := os.ReadFile("../../docs/concepts/solobill.md")
	if err != nil {
		t.Fatalf("read concept: %v", err)
	}

	rows := map[string][]string{}
	for line := range strings.Lines(string(concept)) {
		if !strings.HasPrefix(line, "| `euer.") {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		rows[strings.Trim(cells[0], "`")] = cells
	}

	for _, c := range category.All() {
		cells, ok := rows[string(c.Key)]
		if !ok {
			t.Errorf("key %q has no row in docs/concepts/solobill.md", c.Key)

			continue
		}
		if len(cells) < 3 {
			t.Errorf("row of %q has %d cells, want key, kind and Zeile at least", c.Key, len(cells))

			continue
		}
		if cells[1] != string(c.Kind) {
			t.Errorf("row of %q names kind %q, the code says %q", c.Key, cells[1], c.Kind)
		}
		if want := zeile(c.Key); cells[2] != want {
			t.Errorf("row of %q names Zeile %q, the key says %q", c.Key, cells[2], want)
		}
	}

	for key := range rows {
		if _, ok := category.Lookup(category.Key(key)); !ok {
			t.Errorf("docs/concepts/solobill.md has a row for %q, which is no key", key)
		}
	}
}

// zeile extracts the line number from a key: "euer.z62.nd" is "62".
func zeile(key category.Key) string {
	rest := strings.TrimPrefix(string(key), "euer.z")
	number, _, _ := strings.Cut(rest, ".")

	return number
}

func TestValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  category.Key
		kind category.Kind
		want bool
	}{
		{"expense key as expense", category.Goods, category.KindExpense, true},
		{"expense key as income", category.Goods, category.KindIncome, false},
		{"income key as income", category.VATRefunded, category.KindIncome, true},
		{"derived key is never an expense", category.InputVAT, category.KindExpense, false},
		{"asset key is never an expense", category.LowValueAssets, category.KindExpense, false},
		{"unknown key", category.Key("euer.z999"), category.KindExpense, false},
		{"form number is no key", category.Key("2025AnlEÜR801"), category.KindExpense, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := category.Valid(tt.key, tt.kind); got != tt.want {
				t.Errorf("Valid(%q, %q) = %v, want %v", tt.key, tt.kind, got, tt.want)
			}
		})
	}
}

func TestAllReturnsACopy(t *testing.T) {
	t.Parallel()

	first := category.All()
	first[0].Key = "changed"

	if category.All()[0].Key == "changed" {
		t.Error("All() exposes the package's own slice")
	}
}
