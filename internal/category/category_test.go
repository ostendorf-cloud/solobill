package category_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/category"
)

// keyPattern is the only shape a key may have: a line of the Anlage EÜR
// and an optional deductibility suffix. A form number or a Kennzahl
// never matches it.
var keyPattern = regexp.MustCompile(`^euer\.z[0-9]{1,3}(\.(d|nd))?$`)

// keysDir holds one table per issue that registers keys.
var keysDir = filepath.Join("..", "..", "docs", "concepts", "euer-keys")

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
		if c.Issue == 0 {
			t.Errorf("key %q has no owning issue", c.Key)
		}
		switch c.Kind {
		case category.KindIncome, category.KindExpense, category.KindAsset, category.KindDerived:
		default:
			t.Errorf("key %q has unknown kind %q", c.Key, c.Kind)
		}
	}
}

// docRow is one row of a docs/concepts/euer-keys/<issue>.md table.
type docRow struct {
	issue int
	kind  string
	zeile string
}

// readRows gathers the key rows of every per issue table.
func readRows(t *testing.T) map[string]docRow {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(keysDir, "*.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("find key tables in %s: %v (found %d)", keysDir, err, len(files))
	}

	rows := map[string]docRow{}
	for _, file := range files {
		issue, err := strconv.Atoi(strings.TrimSuffix(filepath.Base(file), ".md"))
		if err != nil {
			t.Fatalf("%s: the file name must be the owning issue number", file)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for line := range strings.Lines(string(data)) {
			if !strings.HasPrefix(line, "| `euer.") {
				continue
			}
			cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
			for i := range cells {
				cells[i] = strings.TrimSpace(cells[i])
			}
			if len(cells) < 3 {
				t.Errorf("%s: row %q has fewer than 3 cells", file, line)

				continue
			}
			key := strings.Trim(cells[0], "`")
			if _, dup := rows[key]; dup {
				t.Errorf("%s: key %q is documented twice", file, key)
			}
			rows[key] = docRow{issue: issue, kind: cells[1], zeile: cells[2]}
		}
	}

	return rows
}

// TestEveryKeyIsDocumentedByItsOwner checks code and tables in both
// directions: every key has a row in the table of the issue that
// registers it, with the same kind and Zeile, and every row is a key.
func TestEveryKeyIsDocumentedByItsOwner(t *testing.T) {
	t.Parallel()

	rows := readRows(t)

	for _, c := range category.All() {
		row, ok := rows[string(c.Key)]
		if !ok {
			t.Errorf("key %q has no row in %s/%d.md", c.Key, keysDir, c.Issue)

			continue
		}
		if row.issue != c.Issue {
			t.Errorf("key %q is registered by #%d but documented in %d.md", c.Key, c.Issue, row.issue)
		}
		if row.kind != string(c.Kind) {
			t.Errorf("row of %q names kind %q, the code says %q", c.Key, row.kind, c.Kind)
		}
		if want := strconv.Itoa(category.Zeile(c.Key)); row.zeile != want {
			t.Errorf("row of %q names Zeile %q, the key says %q", c.Key, row.zeile, want)
		}
	}

	for key := range rows {
		if _, ok := category.Lookup(category.Key(key)); !ok {
			t.Errorf("a key table has a row for %q, which is no registered key", key)
		}
	}
}

func TestAllIsInFormOrder(t *testing.T) {
	t.Parallel()

	all := category.All()
	for i := 1; i < len(all); i++ {
		if category.Zeile(all[i-1].Key) > category.Zeile(all[i].Key) {
			t.Errorf("%q comes before %q", all[i-1].Key, all[i].Key)
		}
	}
	idx := func(k category.Key) int {
		for i, c := range all {
			if c.Key == k {
				return i
			}
		}

		return -1
	}
	if idx(category.GiftsNonDeductible) > idx(category.GiftsDeductible) {
		t.Error("the non deductible key of a line must come first")
	}
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
		{"key of another issue file", category.HomeOfficeDeductible, category.KindExpense, true},
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
