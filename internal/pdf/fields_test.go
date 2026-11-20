package pdf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
)

// stringField is one string field of FrozenPayloadV1 by its JSON path.
type stringField struct {
	path string
	set  func(*docpayload.FrozenPayloadV1, string)
}

func jsonName(f reflect.StructField) string {
	n, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	if n == "" {
		return f.Name
	}

	return n
}

// stringFields walks every string field below t, the first element of a
// slice and the target of a pointer, which it allocates when nil.
func stringFields(t reflect.Type, prefix string, get func(*docpayload.FrozenPayloadV1) reflect.Value, out *[]stringField) {
	switch t.Kind() {
	case reflect.String:
		*out = append(*out, stringField{prefix, func(p *docpayload.FrozenPayloadV1, s string) { get(p).SetString(s) }})
	case reflect.Struct:
		for i := range t.NumField() {
			f := t.Field(i)
			sub := jsonName(f)
			if prefix != "" {
				sub = prefix + "." + sub
			}
			stringFields(f.Type, sub, func(p *docpayload.FrozenPayloadV1) reflect.Value { return get(p).Field(i) }, out)
		}
	case reflect.Pointer:
		et := t.Elem()
		stringFields(et, prefix, func(p *docpayload.FrozenPayloadV1) reflect.Value {
			v := get(p)
			if v.IsNil() {
				v.Set(reflect.New(et))
			}

			return v.Elem()
		}, out)
	case reflect.Slice:
		stringFields(t.Elem(), prefix+"[0]", func(p *docpayload.FrozenPayloadV1) reflect.Value { return get(p).Index(0) }, out)
	default:
	}
}

func allStringFields() []stringField {
	var fields []stringField
	stringFields(reflect.TypeFor[docpayload.FrozenPayloadV1](), "",
		func(p *docpayload.FrozenPayloadV1) reflect.Value { return reflect.ValueOf(p).Elem() }, &fields)

	return fields
}

// fieldsPayload is a correction with every optional printed field set.
func fieldsPayload(t *testing.T) docpayload.FrozenPayloadV1 {
	t.Helper()
	p := loadFixture(t, "correction-de")
	p.CustomerReference = "PO 1"
	p.Notes = "n"
	p.TaxNote = "tn"
	p.Customer.VATID = "ATU1"
	p.Seller.KleinunternehmerID = "KU1"
	p.Seller.Address.Line2 = "l2"

	return p
}

func clonePayload(p docpayload.FrozenPayloadV1) docpayload.FrozenPayloadV1 {
	q := p
	q.Lines = append([]docpayload.Line(nil), p.Lines...)
	q.Totals.VAT = append([]docpayload.VATBucket(nil), p.Totals.VAT...)
	if p.ServicePeriod != nil {
		sp := *p.ServicePeriod
		q.ServicePeriod = &sp
	}
	if p.Corrects != nil {
		c := *p.Corrects
		q.Corrects = &c
	}

	return q
}

// validMarkers are fields whose marker must be a valid value; the second
// string is what the template prints for it.
var validMarkers = map[string][2]string{
	"customer.address.country_code": {"AT", "ÖSTERREICH"},
}

// fieldMarker is the value set into a field and the text printed for it.
func fieldMarker(path string, i int) (string, string) {
	if m, ok := validMarkers[path]; ok {
		return m[0], m[1]
	}
	m := fmt.Sprintf("QZX%dXZQ", i)

	return m, m
}

func squeeze(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == zeroWidthSpace {
			return -1
		}

		return r
	}, s)
}

// TestEveryPrintedFieldIsChecked walks every string field of
// FrozenPayloadV1. A field whose marker reaches the template data must
// be refused by CheckPrintable and BuildInput, at its own path, when it
// holds a character the fonts cannot show; a field that is not printed
// must not be refused. So what is printed and what is checked cannot
// drift apart.
func TestEveryPrintedFieldIsChecked(t *testing.T) {
	base := fieldsPayload(t)
	printedCount := 0
	for i, f := range allStringFields() {
		value, shown := fieldMarker(f.path, i)
		p := clonePayload(base)
		f.set(&p, value)
		printed := false
		if _, data, err := buildData(p, false); err == nil {
			printed = strings.Contains(squeeze(string(data)), squeeze(shown))
		}

		p = clonePayload(base)
		f.set(&p, "ok"+string(rune(0x4E2D)))
		var invalid *InvalidPayloadError
		checkErr := CheckPrintable(p)
		refused := errors.As(checkErr, &invalid) && invalid.Details[f.path] != ""
		_, buildErr := BuildInput(p, false)
		buildRefused := errors.As(buildErr, &invalid) && invalid.Details[f.path] != ""

		switch {
		case printed && !refused:
			t.Errorf("%s is printed, but CheckPrintable does not refuse it at its path: %v", f.path, checkErr)
		case printed && !buildRefused:
			t.Errorf("%s is printed, but BuildInput does not refuse it at its path: %v", f.path, buildErr)
		case !printed && refused:
			t.Errorf("%s is refused at its path, but never printed", f.path)
		}
		if printed {
			printedCount++
		}
	}
	if printedCount < 30 {
		t.Errorf("only %d fields are printed: the walk or the payload is wrong", printedCount)
	}
}

// TestEveryPrintedFieldReachesThePDF renders one payload with a marker
// in every field the template data holds and finds each marker in the
// PDF's text.
func TestEveryPrintedFieldReachesThePDF(t *testing.T) {
	bin := typstOrSkip(t)
	pdftotext := toolOrSkip(t, "pdftotext")
	base := fieldsPayload(t)
	p := clonePayload(base)
	var want []string
	for i, f := range allStringFields() {
		value, shown := fieldMarker(f.path, i)
		q := clonePayload(base)
		f.set(&q, value)
		if _, data, err := buildData(q, false); err != nil || !strings.Contains(squeeze(string(data)), squeeze(shown)) {
			continue
		}
		f.set(&p, value)
		want = append(want, shown)
	}
	pdf, err := New(Options{Binary: bin, TempDir: t.TempDir()}).RenderPayload(context.Background(), p, false)
	if err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(t.TempDir(), "fields.pdf")
	if err := os.WriteFile(in, pdf, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(context.Background(), pdftotext, "-raw", in, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	text := squeeze(string(out))
	for _, m := range want {
		if !strings.Contains(text, squeeze(m)) {
			t.Errorf("marker %q is in the template data but not in the PDF text", m)
		}
	}
}

func TestCountryCode(t *testing.T) {
	base := loadFixture(t, "offer-de")
	for code, ok := range map[string]bool{
		"AT": true, "CH": true, "US": true, "": true,
		"at": false, "XX": false, "EU": false, "ZZ": false, "DEU": false, "A" + string(rune(0x0192)): false,
	} {
		p := base
		p.Customer.Address.CountryCode = code
		err := CheckPrintable(p)
		var invalid *InvalidPayloadError
		refused := errors.As(err, &invalid) && invalid.Details["customer.address.country_code"] != ""
		if refused == ok {
			t.Errorf("country code %q: CheckPrintable = %v", code, err)
		}
	}
}

// TestBidiReason pins the reason for a bidirectional control: the fonts
// lack these runes too, so only a direct check shows which rule decides.
func TestBidiReason(t *testing.T) {
	pr, err := newPrinter(documentV1Limits)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []rune{0x202A, 0x202E, 0x2066, 0x2069} {
		if !isBidiControl(r) {
			t.Errorf("%U is not a bidi control", r)
		}
		if got := pr.runeProblem(r, true); !strings.HasPrefix(got, "bidirectional control character") {
			t.Errorf("%U: reason %q", r, got)
		}
	}
	for _, r := range []rune{0x2029, 0x202F, 0x2065, 0x206A, 'A'} {
		if isBidiControl(r) {
			t.Errorf("%U is no bidi control", r)
		}
	}
}
