package pdf

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
)

// The review's fuzz generator, kept with its structure and seed: it
// builds adversarial strings from blocks (runs of wide and narrow
// letters, compounds, numbers, e-mail addresses, URLs, punctuation next
// to spaces, quotes and brackets), puts them into random fields, and
// shrinks a refused payload towards the limits, so many payloads land
// just inside them.

var reviewCompounds = []string{
	"Donau", "dampf", "schiff", "fahrts", "gesellschaft", "kapitäns", "mützen", "Rechts", "anwalts",
	"Kraft", "fahrzeug", "haftpflicht", "versicherungs", "Grundstücks", "verkehrs", "genehmigungs",
	"zuständigkeiten", "übertragungs", "verordnung", "Software", "entwicklungs", "leistungen", "Wartungs",
	"pauschale", "Straßen", "bahn", "Rückerstattungs", "anspruch",
}

type reviewGen struct {
	rng       *rand.Rand
	multiline bool
}

func (g reviewGen) letters() string {
	var b strings.Builder
	for range 1 + g.rng.IntN(3) {
		b.WriteString(strings.Repeat(string("WMil"[g.rng.IntN(4)]), 1+g.rng.IntN(30)))
	}

	return b.String()
}

func (g reviewGen) compound() string {
	var b strings.Builder
	for utf8.RuneCountInString(b.String()) < 12+g.rng.IntN(29) {
		b.WriteString(reviewCompounds[g.rng.IntN(len(reviewCompounds))])
	}
	r := []rune(b.String())

	return string(r[:min(len(r), 40)])
}

func (g reviewGen) alnum(n int) string {
	const a = "abcdefghijklmnopqrstuvwxyzWM0123456789"
	var b strings.Builder
	for range n {
		b.WriteByte(a[g.rng.IntN(len(a))])
	}

	return b.String()
}

func (g reviewGen) email() string {
	n := 20 + g.rng.IntN(61)
	local := g.alnum(max(1, n/2-1))
	dom := g.alnum(max(1, n-len(local)-4))

	return local + "@" + dom + ".de"
}

func (g reviewGen) url() string {
	n := 20 + g.rng.IntN(61)
	s := "https://" + g.alnum(8) + ".example/"
	for len(s) < n {
		s += g.alnum(1+g.rng.IntN(10)) + []string{"/", "-", ".", "?q=", "_"}[g.rng.IntN(5)]
	}

	return s[:n]
}

// reviewSpecials: four kinds of space, a soft hyphen, two combining
// marks, and break relevant punctuation.
var reviewSpecials = []string{
	" ", string(noBreakSpace), string(rune(0x202F)), string(rune(0x2007)), string(rune(0x2009)),
	string(rune(0x00AD)), string(rune(0x0301)), string(rune(0x0308)),
	"-", "/", ".", ",", "@", "://",
}

func (g reviewGen) block() string {
	switch g.rng.IntN(12) {
	case 0, 1, 2:
		return g.letters()
	case 3:
		return strings.Repeat("0123456789", 3)[:1+g.rng.IntN(25)]
	case 4:
		return reviewSpecials[g.rng.IntN(len(reviewSpecials))]
	case 5:
		p := string(")]}!?,.:;"[g.rng.IntN(9)])
		if g.rng.IntN(2) == 0 {
			return " " + p
		}

		return p + " "
	case 6:
		return g.compound()
	case 7:
		return g.email()
	case 8:
		return g.url()
	case 9:
		n := 10 + g.rng.IntN(40)
		s := strings.Repeat("9876543210", 5)[:n]
		if g.rng.IntN(2) == 0 {
			s = groupDigits(s, []string{".", ",", " ", string(noBreakSpace)}[g.rng.IntN(4)])
		}

		return s
	case 10:
		if g.multiline {
			return []string{"\t", "\n", "\n\n", "\r\n"}[g.rng.IntN(4)]
		}
		if g.rng.IntN(8) == 0 {
			return "\t"
		}

		return " "
	default:
		return []string{"(", "[", "„", "»", "«", "“", "”"}[g.rng.IntN(7)]
	}
}

func (g reviewGen) text(maxRunes int) string {
	target := 1 + g.rng.IntN(maxRunes)
	var b strings.Builder
	for utf8.RuneCountInString(b.String()) < target {
		b.WriteString(g.block())
		b.WriteString([]string{"", " ", " ", "  "}[g.rng.IntN(4)])
	}
	r := []rune(b.String())
	if len(r) > target {
		r = r[:target]
	}

	return string(r)
}

type reviewField struct {
	path      string
	max       int
	multiline bool
	ptr       func(p *docpayload.FrozenPayloadV1) *string
}

func reviewFields() []reviewField {
	f := func(path string, n int, ml bool, ptr func(p *docpayload.FrozenPayloadV1) *string) reviewField {
		return reviewField{path, n, ml, ptr}
	}

	return []reviewField{
		f("lines[0].description", 1000, true, func(p *docpayload.FrozenPayloadV1) *string { return &p.Lines[0].Description }),
		f("lines[0].unit", 16, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Lines[0].Unit }),
		f("tax_note", 1000, true, func(p *docpayload.FrozenPayloadV1) *string { return &p.TaxNote }),
		f("payment_terms", 1000, true, func(p *docpayload.FrozenPayloadV1) *string { return &p.PaymentTerms }),
		f("notes", 1000, true, func(p *docpayload.FrozenPayloadV1) *string { return &p.Notes }),
		f("seller.footer_text", 300, true, func(p *docpayload.FrozenPayloadV1) *string { return &p.Seller.FooterText }),
		f("seller.legal_name", 80, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Seller.LegalName }),
		f("seller.address.street", 80, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Seller.Address.Street }),
		f("seller.address.line2", 80, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Seller.Address.Line2 }),
		f("seller.address.city", 80, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Seller.Address.City }),
		f("seller.address.postal_code", 80, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Seller.Address.PostalCode }),
		f("seller.email", 80, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Seller.Email }),
		f("seller.website", 80, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Seller.Website }),
		f("seller.phone", 80, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Seller.Phone }),
		f("seller.iban", 80, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Seller.IBAN }),
		f("seller.bic", 80, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Seller.BIC }),
		f("seller.bank_name", 80, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Seller.BankName }),
		f("seller.tax_number", 80, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Seller.TaxNumber }),
		f("seller.vat_id", 80, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Seller.VATID }),
		f("customer.name", 200, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Customer.Name }),
		f("customer.number", 200, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Customer.Number }),
		f("customer.vat_id", 200, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Customer.VATID }),
		f("customer.address.street", 200, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Customer.Address.Street }),
		f("customer.address.line2", 200, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Customer.Address.Line2 }),
		f("customer.address.postal_code", 200, false, func(p *docpayload.FrozenPayloadV1) *string {
			return &p.Customer.Address.PostalCode
		}),
		f("customer.address.city", 200, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Customer.Address.City }),
		f("number", 200, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.Number }),
		f("customer_reference", 200, false, func(p *docpayload.FrozenPayloadV1) *string { return &p.CustomerReference }),
		f("corrects.number", 200, false, func(p *docpayload.FrozenPayloadV1) *string {
			if p.Corrects == nil {
				p.Corrects = &docpayload.DocumentReference{Number: "RE-1", IssueDate: "2026-01-01"}
			}

			return &p.Corrects.Number
		}),
	}
}

// reviewShrink removes one rune from the middle of the widest word of s.
func reviewShrink(t *testing.T, s string) string {
	t.Helper()
	m, err := loadMetrics()
	if err != nil {
		t.Fatal(err)
	}
	best, bestW, off, pos := "", -1.0, 0, 0
	for _, line := range strings.Split(s, "\n") {
		lpos := 0
		for _, w := range splitWords(line) {
			i := strings.Index(line[lpos:], w) + lpos
			if ww := m.widthMM(w, cell{1, 10, bold}); ww > bestW {
				best, bestW, off = w, ww, pos+i
			}
			lpos = i + len(w)
		}
		pos += len(line) + 1
	}
	r := []rune(best)
	if len(r) == 0 {
		return s
	}
	k := len(r) / 2

	return s[:off] + string(r[:k]) + string(r[k+1:]) + s[off+len(best):]
}

var reviewRune = regexp.MustCompile(`U\+([0-9A-F]{4,6})`)

// shrinkToAccept edits the fields a refusal names until CheckPrintable
// accepts p or nothing changes.
func shrinkToAccept(t *testing.T, p *docpayload.FrozenPayloadV1, fields []reviewField) error {
	t.Helper()
	err := CheckPrintable(*p)
	for tries := 0; err != nil && tries < 400; tries++ {
		var invalid *InvalidPayloadError
		if !errors.As(err, &invalid) {
			break
		}
		changed := false
		for path, reason := range invalid.Details {
			if strings.HasPrefix(path, "lines[") && strings.Contains(reason, "value too wide") {
				k, _ := strconv.Atoi(path[len("lines["):strings.Index(path, "]")])
				l := &p.Lines[k]
				switch {
				case strings.HasSuffix(path, "quantity_thousandths"):
					l.QuantityThousandths /= 10
				case strings.HasSuffix(path, "unit_price_cents"):
					l.UnitPriceCents /= 10
				case strings.HasSuffix(path, "net_after_line_discount_cents"):
					l.NetAfterLineDiscountCents /= 10
				}
				changed = true

				continue
			}
			if path == "lines" {
				p.Lines = p.Lines[:len(p.Lines)*3/4]
				changed = true

				continue
			}
			var sp *string
			for _, fl := range fields {
				if fl.path == path {
					sp = fl.ptr(p)
				}
			}
			if strings.HasPrefix(path, "lines[") && strings.HasSuffix(path, ".description") {
				k, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(path, "lines["), "].description"))
				sp = &p.Lines[k].Description
			}
			if sp == nil {
				continue
			}
			if m := reviewRune.FindStringSubmatch(reason); m != nil {
				v, _ := strconv.ParseUint(m[1], 16, 32)
				*sp = strings.ReplaceAll(*sp, string(rune(v)), "")
			} else if strings.Contains(reason, "at most") {
				rs := []rune(*sp)
				*sp = string(rs[:len(rs)*3/4])
				if strings.Contains(reason, "lines") {
					*sp = strings.Join(strings.SplitN(*sp, "\n", 10)[:min(9, strings.Count(*sp, "\n"))], "\n")
				}
			} else {
				*sp = reviewShrink(t, *sp)
			}
			changed = true
		}
		if !changed {
			break
		}
		err = CheckPrintable(*p)
	}

	return err
}

// TestFuzzReviewSeed runs the review's generator with its seed over 400
// payloads. Every accepted payload must render: no overflow, no two
// words overlapping, no clipped word, at most MaxPages pages. A refused
// one must be refused before Typst runs. Skipped with -short.
func TestFuzzReviewSeed(t *testing.T) {
	if testing.Short() {
		t.Skip("renders a hundred documents")
	}
	bin := typstOrSkip(t)
	pdftotext := toolOrSkip(t, "pdftotext")
	rng := rand.New(rand.NewPCG(20261120, 74))
	r := New(Options{Binary: bin, TempDir: t.TempDir()})
	noTypst := New(Options{Binary: "/nonexistent/typst", TempDir: t.TempDir()})
	fields := reviewFields()
	bases := []string{
		"invoice-standard-de", "invoice-exempt-de", "invoice-line-discount-en", "invoice-kleinunternehmer-de",
		"invoice-kleinunternehmer-en", "correction-de", "offer-de", "offer-en",
	}
	accepted, refused := 0, 0
	// The payloads are drawn one after another from the seeded generator,
	// so they are the same on every run; only the rendering runs in
	// parallel.
	var cases []namedPayload
	for i := range 400 {
		p := loadFixture(t, bases[rng.IntN(len(bases))])
		nl := 1 + rng.IntN(12)
		if rng.IntN(10) == 0 {
			nl = 50 + rng.IntN(400)
		}
		base := p.Lines[0]
		p.Lines = p.Lines[:0]
		for k := range nl {
			l := base
			l.Position = int32(k + 1)
			if rng.IntN(4) == 0 {
				l.QuantityThousandths = rng.Int64N(1e10) - 5e8
				l.UnitPriceCents = rng.Int64N(1e10)
				l.NetAfterLineDiscountCents = rng.Int64N(1e12) - 1e10
			}
			p.Lines = append(p.Lines, l)
		}
		f := fields[i%len(fields)]
		shrink := rng.IntN(10) < 7
		set := func(fl reviewField) {
			*fl.ptr(&p) = reviewGen{rng: rng, multiline: fl.multiline}.text(fl.max)
		}
		set(f)
		for range 2 {
			set(fields[rng.IntN(len(fields))])
		}
		if nl > 1 && rng.IntN(3) == 0 {
			for k := range p.Lines {
				p.Lines[k].Description = reviewGen{rng: rng, multiline: true}.text(300)
			}
		}
		err := CheckPrintable(p)
		if shrink {
			err = shrinkToAccept(t, &p, fields)
		}
		name := fmt.Sprintf("review-%03d", i)
		if err != nil {
			refused++
			if _, rerr := noTypst.RenderPayload(context.Background(), p, false); errors.Is(rerr, ErrUnavailable) {
				t.Errorf("%s: refused by CheckPrintable (%v), but Render reached typst", name, err)
			}

			continue
		}
		accepted++
		cases = append(cases, namedPayload{name, p})
	}
	renderAllChecked(t, r, pdftotext, cases)
	t.Logf("400 payloads: %d accepted and rendered, %d refused before Typst", accepted, refused)
	if accepted < 50 {
		t.Errorf("only %d payloads accepted", accepted)
	}
}
