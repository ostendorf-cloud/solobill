package euer

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/ostendorf-cloud/solobill/internal/category"
)

// Status is how far a tax year's mapping was checked against the form.
type Status string

const (
	// StatusVerified is a mapping compared with the BMF form of its year.
	StatusVerified Status = "verified"
	// StatusUnverified is a mapping nobody compared with the form of its
	// year yet. The report view shows a banner for it.
	StatusUnverified Status = "unverified"
)

// MappingLine maps one line key to the Zeile and Kennzahl of one tax
// year's Anlage EÜR.
type MappingLine struct {
	Key   category.Key `json:"key"`
	Zeile int          `json:"zeile"`
	// Kennzahl is empty when it was not read. The report then shows no
	// Kennzahl rather than a guessed one.
	Kennzahl string `json:"kennzahl"`
	// Operator is the sign the form prints next to the Kennzahl: "-" for
	// a line the form subtracts (Zeile 72), "+" for one it adds back
	// (Zeile 73), empty for a plain line. The amount itself stays
	// positive, as it goes into the field.
	Operator string `json:"operator,omitempty"`
	// Verified is false for a line whose Zeile or Kennzahl was not
	// checked against the form, also inside a verified year.
	Verified bool   `json:"verified"`
	Note     string `json:"note,omitempty"`
}

// Mapping is the line mapping of one tax year. It is data, embedded from
// mapping/<year>.json, never code (docs/concepts/solobill.md, "EÜR
// mapping as data per tax year").
type Mapping struct {
	TaxYear int           `json:"tax_year"`
	Status  Status        `json:"status"`
	Source  string        `json:"source"`
	Lines   []MappingLine `json:"lines"`
}

// Line returns the mapping of key and whether the year maps it.
func (m Mapping) Line(key category.Key) (MappingLine, bool) {
	for _, l := range m.Lines {
		if l.Key == key {
			return l, true
		}
	}

	return MappingLine{}, false
}

//go:embed mapping/*.json
var mappingFiles embed.FS

// mappings is every embedded year, parsed and checked once.
var mappings = mustLoadMappings()

func mustLoadMappings() map[int]Mapping {
	out, err := loadMappings()
	if err != nil {
		panic(err)
	}

	return out
}

func loadMappings() (map[int]Mapping, error) {
	entries, err := mappingFiles.ReadDir("mapping")
	if err != nil {
		return nil, fmt.Errorf("euer: read the mappings: %w", err)
	}
	out := map[int]Mapping{}
	for _, e := range entries {
		data, err := mappingFiles.ReadFile(path.Join("mapping", e.Name()))
		if err != nil {
			return nil, fmt.Errorf("euer: read %s: %w", e.Name(), err)
		}
		m, err := parseMapping(data)
		if err != nil {
			return nil, fmt.Errorf("euer: %s: %w", e.Name(), err)
		}
		if want := strings.TrimSuffix(e.Name(), ".json"); strconv.Itoa(m.TaxYear) != want {
			return nil, fmt.Errorf("euer: %s holds tax year %d", e.Name(), m.TaxYear)
		}
		out[m.TaxYear] = m
	}

	return out, nil
}

// parseMapping decodes one year and checks it: a known status, every key
// registered in internal/category, no key twice, a positive Zeile, a
// Kennzahl of digits or empty, and an operator of "+", "-" or none. An empty Kennzahl or an unverified year
// needs the line to be unverified too.
func parseMapping(data []byte) (Mapping, error) {
	var m Mapping
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Mapping{}, fmt.Errorf("decode: %w", err)
	}
	if m.Status != StatusVerified && m.Status != StatusUnverified {
		return Mapping{}, fmt.Errorf("unknown status %q", m.Status)
	}
	seen := map[category.Key]bool{}
	for _, l := range m.Lines {
		if _, ok := category.Lookup(l.Key); !ok {
			return Mapping{}, fmt.Errorf("key %q is not registered", l.Key)
		}
		if seen[l.Key] {
			return Mapping{}, fmt.Errorf("key %q appears twice", l.Key)
		}
		seen[l.Key] = true
		if l.Zeile <= 0 {
			return Mapping{}, fmt.Errorf("key %q has no Zeile", l.Key)
		}
		if strings.Trim(l.Kennzahl, "0123456789") != "" {
			return Mapping{}, fmt.Errorf("key %q has Kennzahl %q, not digits", l.Key, l.Kennzahl)
		}
		if l.Operator != "" && l.Operator != "-" && l.Operator != "+" {
			return Mapping{}, fmt.Errorf("key %q has operator %q, not + or -", l.Key, l.Operator)
		}
		if l.Verified && (l.Kennzahl == "" || m.Status != StatusVerified) {
			return Mapping{}, fmt.Errorf("key %q is marked verified without a Kennzahl or in an unverified year", l.Key)
		}
	}

	return m, nil
}

// MappingFor returns the mapping of a tax year and whether one exists.
func MappingFor(year int) (Mapping, bool) {
	m, ok := mappings[year]
	if !ok {
		return Mapping{}, false
	}
	m.Lines = append([]MappingLine(nil), m.Lines...)

	return m, true
}

// MappingYears is every tax year with a mapping, ascending.
func MappingYears() []int {
	years := make([]int, 0, len(mappings))
	for y := range mappings {
		years = append(years, y)
	}
	sort.Ints(years)

	return years
}
