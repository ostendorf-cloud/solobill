package paymentvat_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/docchain"
	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/paymentvat"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

func day(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}

	return d
}

// chainOf is an original issued on 2026-11-01 plus one correction per
// issue date given.
func chainOf(corrections ...string) []docchain.Member {
	members := []docchain.Member{{ID: uuid.New(), Seq: 0, IssuedOn: day("2026-11-01")}}
	for i, issued := range corrections {
		members = append(members, docchain.Member{ID: uuid.New(), Seq: i + 1, IssuedOn: day(issued)})
	}

	return members
}

// paymentList builds payments of root, created one second apart in the
// order given.
type paymentList struct {
	root uuid.UUID
	list []storage.Payment
}

func (l *paymentList) add(receivedOn string, amount int64) storage.Payment {
	p := storage.Payment{
		ID:          uuid.New(),
		DocumentID:  l.root,
		ReceivedOn:  day(receivedOn),
		AmountCents: amount,
		CreatedAt:   time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(len(l.list)) * time.Second),
	}
	l.list = append(l.list, p)

	return p
}

// reverse adds the reversal of of: the negated amount on of's own day.
func (l *paymentList) reverse(of storage.Payment) {
	p := l.add(of.ReceivedOn.Format(time.DateOnly), -of.AmountCents)
	p.ReversesPaymentID = &of.ID
	l.list[len(l.list)-1] = p
}

func vatOf(shares []paymentvat.Share) []int64 {
	out := make([]int64, len(shares))
	for i, s := range shares {
		out[i] = s.VATCents
	}

	return out
}

func sum(xs []int64) int64 {
	var s int64
	for _, x := range xs {
		s += x
	}

	return s
}

func mustSplit(t *testing.T, members []docchain.Member, figures map[uuid.UUID]paymentvat.Figures, list []storage.Payment) []paymentvat.Share {
	t.Helper()

	shares, err := paymentvat.Split(members, figures, list)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}
	for _, s := range shares {
		if s.NetCents+s.VATCents != s.GrossCents {
			t.Fatalf("share %+v does not add up", s)
		}
	}

	return shares
}

func equal(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func TestKleinunternehmerHasNoVATShare(t *testing.T) {
	t.Parallel()

	m := chainOf()
	l := paymentList{root: m[0].ID}
	l.add("2026-11-02", 10000)
	l.add("2026-11-03", 3750)
	shares := mustSplit(t, m, map[uuid.UUID]paymentvat.Figures{m[0].ID: {GrossCents: 13750}}, l.list)
	if got := vatOf(shares); !equal(got, []int64{0, 0}) {
		t.Fatalf("VAT shares = %v, want none", got)
	}
}

// TestCumulativeRoundingLosesNoCent: three partial payments of an invoice
// of 1.19 EUR gross and 0.19 EUR VAT. Rounding each share alone gives
// 6 + 6 + 6 = 18 cents. The cumulative rule hands out exactly 19.
func TestCumulativeRoundingLosesNoCent(t *testing.T) {
	t.Parallel()

	m := chainOf()
	figures := map[uuid.UUID]paymentvat.Figures{m[0].ID: {GrossCents: 119, VATCents: 19}}
	l := paymentList{root: m[0].ID}
	l.add("2026-11-02", 40)
	l.add("2026-11-03", 40)
	l.add("2026-11-04", 39)

	got := vatOf(mustSplit(t, m, figures, l.list))
	// Exact: 6.387, 12.773 and 19.000 in total.
	if want := []int64{6, 7, 6}; !equal(got, want) {
		t.Fatalf("VAT shares = %v, want %v", got, want)
	}

	// 119 payments of one cent: every exact share is 0.16, rounding each
	// alone would hand out nothing at all.
	l = paymentList{root: m[0].ID}
	for range 119 {
		l.add("2026-11-02", 1)
	}
	if got := sum(vatOf(mustSplit(t, m, figures, l.list))); got != 19 {
		t.Fatalf("119 one cent payments carry %d cents VAT, want 19", got)
	}
}

var propertyFigures = []paymentvat.Figures{
	{GrossCents: 11900, VATCents: 1900},
	{GrossCents: 23757, VATCents: 3793},
	{GrossCents: 10700, VATCents: 700},
	{GrossCents: 1, VATCents: 0},
	{GrossCents: 99999, VATCents: 15966},
}

func randomDay(rng *rand.Rand) string {
	return fmt.Sprintf("2026-11-%02d", 2+rng.IntN(7))
}

// TestAnySplitOfTheTotalSumsToTheVATTotal splits a standard invoice into
// random partial payments many times: the shares always sum to exactly
// the VAT total.
func TestAnySplitOfTheTotalSumsToTheVATTotal(t *testing.T) {
	t.Parallel()

	m := chainOf()
	rng := rand.New(rand.NewPCG(71, 5))
	for _, f := range propertyFigures {
		figures := map[uuid.UUID]paymentvat.Figures{m[0].ID: f}
		for range 200 {
			l := paymentList{root: m[0].ID}
			left := f.GrossCents
			for left > 0 {
				amount := 1 + rng.Int64N(left)
				l.add(randomDay(rng), amount)
				left -= amount
			}
			if got := sum(vatOf(mustSplit(t, m, figures, l.list))); got != f.VATCents {
				t.Fatalf("figures %+v, %d payments: VAT shares sum to %d, want %d", f, len(l.list), got, f.VATCents)
			}
		}
	}
}

// TestFullyPaidChainsWithTrailingReversalsSumToTheVATTotal: random chains
// paid in full, plus mistaken payments and refunds that are reversed at
// the end. Every one sums to exactly the VAT total. Rounding a reversal as
// the negated share of its payment fails this test already on its first
// chain (checked by mutation).
func TestFullyPaidChainsWithTrailingReversalsSumToTheVATTotal(t *testing.T) {
	t.Parallel()

	m := chainOf()
	rng := rand.New(rand.NewPCG(71, 6))
	for i := range 5000 {
		f := propertyFigures[i%len(propertyFigures)]
		figures := map[uuid.UUID]paymentvat.Figures{m[0].ID: f}
		l := paymentList{root: m[0].ID}
		var mistakes []storage.Payment
		left := f.GrossCents
		for left > 0 {
			amount := 1 + rng.Int64N(left)
			l.add(randomDay(rng), amount)
			left -= amount
			if rng.IntN(2) == 0 {
				mistake := 1 + rng.Int64N(f.GrossCents+50)
				if rng.IntN(3) == 0 {
					mistake = -mistake
				}
				mistakes = append(mistakes, l.add(randomDay(rng), mistake))
			}
		}
		// The last day, so the reversals of these come last in every order.
		for range 1 + rng.IntN(2) {
			mistakes = append(mistakes, l.add("2026-11-30", 1+rng.Int64N(500)))
		}
		for _, mistake := range mistakes {
			l.reverse(mistake)
		}

		if got := sum(vatOf(mustSplit(t, m, figures, l.list))); got != f.VATCents {
			t.Fatalf("chain %d, figures %+v, %d rows: VAT shares sum to %d, want %d", i, f, len(l.list), got, f.VATCents)
		}
	}
}

// TestATrailingReversalKeepsTheTotal pins the review case: gross 11900,
// VAT 1900, payments 101, 3 (a mistake), 11799, then the reversal of the
// 3, all on one day. Negating the share of the 3 would give 16, 1, 1883,
// -1 and a sum of 1899.
func TestATrailingReversalKeepsTheTotal(t *testing.T) {
	t.Parallel()

	m := chainOf()
	figures := map[uuid.UUID]paymentvat.Figures{m[0].ID: {GrossCents: 11900, VATCents: 1900}}
	l := paymentList{root: m[0].ID}
	l.add("2026-11-02", 101)
	mistake := l.add("2026-11-02", 3)
	l.add("2026-11-02", 11799)
	l.reverse(mistake)

	// Exact running sums: 16.126, 16.605, 1900.479, 1900.
	if got := vatOf(mustSplit(t, m, figures, l.list)); !equal(got, []int64{16, 1, 1883, 0}) {
		t.Fatalf("VAT shares = %v, want [16 1 1883 0]", got)
	}
}

// TestTheRatioComesFromTheDayOfThePayment: the original is 119.00 EUR at
// 19 %, a correction issued on 2026-11-10 is 107.00 EUR at 7 %. A
// payment before the correction keeps the original's ratio, also once the
// correction exists. A payment on the correction's issue day uses the
// correction.
func TestTheRatioComesFromTheDayOfThePayment(t *testing.T) {
	t.Parallel()

	original := paymentvat.Figures{GrossCents: 11900, VATCents: 1900}
	corrected := paymentvat.Figures{GrossCents: 10700, VATCents: 700}

	before := chainOf()
	l := paymentList{root: before[0].ID}
	l.add("2026-11-05", 5950)
	first := mustSplit(t, before, map[uuid.UUID]paymentvat.Figures{before[0].ID: original}, l.list)

	// The same chain after the correction: same original id.
	after := []docchain.Member{before[0], {ID: uuid.New(), Seq: 1, IssuedOn: day("2026-11-10")}}
	figures := map[uuid.UUID]paymentvat.Figures{after[0].ID: original, after[1].ID: corrected}
	l.add("2026-11-10", 4750)
	shares := mustSplit(t, after, figures, l.list)

	if shares[0] != first[0] {
		t.Fatalf("the correction changed the earlier share: %+v, was %+v", shares[0], first[0])
	}
	if shares[0].VATCents != 950 || shares[0].RatioDocumentID != after[0].ID {
		t.Fatalf("first share = %+v, want 950 cents by the original", shares[0])
	}
	// Exact: 950 + 4750 * 700 / 10700 = 950 + 310.75 = 1260.75, rounded
	// 1261, so the second share is 311. The chain then carries 1261 cents
	// VAT, neither the original's 1900 nor the correction's 700: the day
	// ratio rule, NOT VERIFIED.
	if shares[1].VATCents != 311 || shares[1].RatioDocumentID != after[1].ID {
		t.Fatalf("second share = %+v, want 311 cents by the correction", shares[1])
	}
}

// TestALaterPaymentNeverChangesAnEarlierShare: every prefix of a list of
// payments, in recording order, splits to the same shares the full list
// starts with. The reversal is recorded after the refund but dated before
// it, on the day of the payment it reverses: the refund's share stays.
func TestALaterPaymentNeverChangesAnEarlierShare(t *testing.T) {
	t.Parallel()

	m := chainOf("2026-11-15")
	figures := map[uuid.UUID]paymentvat.Figures{
		m[0].ID: {GrossCents: 11900, VATCents: 1900},
		m[1].ID: {GrossCents: 10700, VATCents: 700},
	}
	l := paymentList{root: m[0].ID}
	l.add("2026-11-02", 3333)
	l.add("2026-11-03", 3333)
	wrong := l.add("2026-11-20", 3334)
	l.add("2026-11-21", -1200)
	l.reverse(wrong)
	l.add("2026-11-22", 77)

	full := mustSplit(t, m, figures, l.list)
	for n := 1; n < len(l.list); n++ {
		prefix := mustSplit(t, m, figures, l.list[:n])
		for i, s := range prefix {
			if s != full[i] {
				t.Fatalf("first %d payments: share %d = %+v, the full list has %+v", n, i, s, full[i])
			}
		}
	}
}

func TestAReversalUsesTheRatioOfItsPayment(t *testing.T) {
	t.Parallel()

	m := chainOf("2026-11-10")
	figures := map[uuid.UUID]paymentvat.Figures{
		m[0].ID: {GrossCents: 119, VATCents: 19},
		m[1].ID: {GrossCents: 107, VATCents: 7},
	}
	l := paymentList{root: m[0].ID}
	l.add("2026-11-02", 40)
	wrong := l.add("2026-11-03", 40)
	l.reverse(wrong)
	l.add("2026-11-05", 79)

	shares := mustSplit(t, m, figures, l.list)
	got := vatOf(shares)
	// Exact running sums: 6.387, 12.773, 6.387, 19.
	if !equal(got, []int64{6, 7, -7, 13}) {
		t.Fatalf("VAT shares = %v, want [6 7 -7 13]", got)
	}
	if shares[2].RatioDocumentID != shares[1].RatioDocumentID {
		t.Fatalf("the reversal uses document %s, the reversed payment %s", shares[2].RatioDocumentID, shares[1].RatioDocumentID)
	}
}

// TestARefundAfterACorrection: 119.00 EUR paid in full, then a correction
// to 107.00 EUR at 7 %, then a refund of 12.00 EUR. The refund carries
// the ratio of the correction, the effective document on its day. A
// mistaken refund is undone by a positive reversal.
func TestARefundAfterACorrection(t *testing.T) {
	t.Parallel()

	m := chainOf("2026-11-10")
	figures := map[uuid.UUID]paymentvat.Figures{
		m[0].ID: {GrossCents: 11900, VATCents: 1900},
		m[1].ID: {GrossCents: 10700, VATCents: 700},
	}
	l := paymentList{root: m[0].ID}
	l.add("2026-11-02", 11900)
	refund := l.add("2026-11-12", -1200)

	shares := mustSplit(t, m, figures, l.list)
	// Exact: 1900 - 1200 * 700 / 10700 = 1900 - 78.505 = 1821.495, rounded
	// 1821, so the refund's share is -79. The chain then carries 1821 cents
	// VAT while the effective document shows 700: the day ratio rule with
	// a rate change, NOT VERIFIED.
	if shares[0].VATCents != 1900 || shares[1].VATCents != -79 || shares[1].RatioDocumentID != m[1].ID {
		t.Fatalf("shares = %+v", shares)
	}

	l.reverse(refund)
	shares = mustSplit(t, m, figures, l.list)
	if shares[2].GrossCents != 1200 || shares[2].VATCents != 79 || sum(vatOf(shares)) != 1900 {
		t.Fatalf("shares after the reversal of the refund = %+v", shares)
	}
}

func TestTheRoundingOrderIsTheRecordingOrder(t *testing.T) {
	t.Parallel()

	m := chainOf()
	figures := map[uuid.UUID]paymentvat.Figures{m[0].ID: {GrossCents: 119, VATCents: 19}}
	l := paymentList{root: m[0].ID}
	a := l.add("2026-11-02", 40)
	b := l.add("2026-11-02", 40)
	c := l.add("2026-11-01", 39)

	// c is dated first but recorded last: the rounding order is the
	// recording order.
	got := mustSplit(t, m, figures, []storage.Payment{b, c, a})
	if got[0].PaymentID != a.ID || got[1].PaymentID != b.ID || got[2].PaymentID != c.ID {
		t.Fatalf("order = %v, %v, %v, want the recording order", got[0].PaymentID, got[1].PaymentID, got[2].PaymentID)
	}
}

func TestSplitRefusals(t *testing.T) {
	t.Parallel()

	m := chainOf("2026-11-10")
	figures := map[uuid.UUID]paymentvat.Figures{m[0].ID: {GrossCents: 119, VATCents: 19}}

	other := paymentList{root: uuid.New()}
	other.add("2026-11-02", 40)
	if _, err := paymentvat.Split(m, figures, other.list); err == nil {
		t.Error("a payment of another chain was split")
	}

	l := paymentList{root: m[0].ID}
	l.reverse(storage.Payment{ID: uuid.New(), AmountCents: 40, ReceivedOn: day("2026-11-03")})
	if _, err := paymentvat.Split(m, figures, l.list); !errors.Is(err, paymentvat.ErrUnknownReversal) {
		t.Errorf("unknown reversal error = %v, want ErrUnknownReversal", err)
	}

	// The correction's figures are missing.
	l = paymentList{root: m[0].ID}
	l.add("2026-11-12", 40)
	if _, err := paymentvat.Split(m, figures, l.list); err == nil {
		t.Error("a payment was split without the figures of its effective document")
	}

	// A chain without its original.
	l = paymentList{root: m[1].ID}
	l.add("2026-11-12", 40)
	if _, err := paymentvat.Split(m[1:], figures, l.list); err == nil {
		t.Error("a chain without its original was split")
	}
}

// TestTheRootIsTheOriginal: a correction issued with a zero date (a
// broken row) must not be taken for the original.
func TestTheRootIsTheOriginal(t *testing.T) {
	t.Parallel()

	m := chainOf()
	m = append(m, docchain.Member{ID: uuid.New(), Seq: 1})
	figures := map[uuid.UUID]paymentvat.Figures{m[0].ID: {GrossCents: 119, VATCents: 19}, m[1].ID: {GrossCents: 119, VATCents: 19}}
	l := paymentList{root: m[0].ID}
	l.add("2026-11-02", 119)
	if got := vatOf(mustSplit(t, m, figures, l.list)); !equal(got, []int64{19}) {
		t.Fatalf("VAT shares = %v, want [19]", got)
	}
}

func TestFiguresOf(t *testing.T) {
	t.Parallel()

	payload, err := docpayload.Marshal(docpayload.FrozenPayloadV1{
		Version: docpayload.Version1,
		Totals:  docpayload.Totals{GrandTotalCents: 11900, VATTotalCents: 1900},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := paymentvat.FiguresOf(storage.Document{State: storage.DocumentStateIssued, FrozenPayload: payload})
	if err != nil || got != (paymentvat.Figures{GrossCents: 11900, VATCents: 1900}) {
		t.Fatalf("FiguresOf() = %+v, %v", got, err)
	}
	if _, err := paymentvat.FiguresOf(storage.Document{State: storage.DocumentStateDraft}); err == nil {
		t.Error("FiguresOf() of a draft succeeded")
	}
}
