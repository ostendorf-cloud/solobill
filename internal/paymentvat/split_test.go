package paymentvat_test

import (
	"errors"
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
func chainOf(t *testing.T, corrections ...string) (docchain.Chain, []docchain.Member) {
	t.Helper()

	members := []docchain.Member{{ID: uuid.New(), Seq: 0, IssuedOn: day("2026-11-01")}}
	for i, issued := range corrections {
		members = append(members, docchain.Member{ID: uuid.New(), Seq: i + 1, IssuedOn: day(issued)})
	}
	chain, err := docchain.New(members)
	if err != nil {
		t.Fatal(err)
	}

	return chain, members
}

// payments builds payments of root, created one second apart in the
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
		CreatedAt:   time.Date(2026, 12, 1, 0, 0, len(l.list), 0, time.UTC),
	}
	l.list = append(l.list, p)

	return p
}

func (l *paymentList) reverse(receivedOn string, of storage.Payment) {
	p := l.add(receivedOn, -of.AmountCents)
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

func mustSplit(t *testing.T, chain docchain.Chain, figures map[uuid.UUID]paymentvat.Figures, list []storage.Payment) []paymentvat.Share {
	t.Helper()

	shares, err := paymentvat.Split(chain, figures, list)
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

	chain, m := chainOf(t)
	l := paymentList{root: m[0].ID}
	l.add("2026-11-02", 10000)
	l.add("2026-11-03", 3750)
	shares := mustSplit(t, chain, map[uuid.UUID]paymentvat.Figures{m[0].ID: {GrossCents: 13750}}, l.list)
	if got := vatOf(shares); !equal(got, []int64{0, 0}) {
		t.Fatalf("VAT shares = %v, want none", got)
	}
}

// TestCumulativeRoundingLosesNoCent: three partial payments of an invoice
// of 1.19 EUR gross and 0.19 EUR VAT. Rounding each share alone gives
// 6 + 6 + 6 = 18 cents. The cumulative rule hands out exactly 19.
func TestCumulativeRoundingLosesNoCent(t *testing.T) {
	t.Parallel()

	chain, m := chainOf(t)
	figures := map[uuid.UUID]paymentvat.Figures{m[0].ID: {GrossCents: 119, VATCents: 19}}
	l := paymentList{root: m[0].ID}
	l.add("2026-11-02", 40)
	l.add("2026-11-03", 40)
	l.add("2026-11-04", 39)

	got := vatOf(mustSplit(t, chain, figures, l.list))
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
	if got := sum(vatOf(mustSplit(t, chain, figures, l.list))); got != 19 {
		t.Fatalf("119 one cent payments carry %d cents VAT, want 19", got)
	}
}

// TestAnySplitOfTheTotalSumsToTheVATTotal splits a standard invoice into
// random partial payments many times: the shares always sum to exactly
// the VAT total, and every share stays within one cent of its exact value
// relative to the running sum.
func TestAnySplitOfTheTotalSumsToTheVATTotal(t *testing.T) {
	t.Parallel()

	chain, m := chainOf(t)
	rng := rand.New(rand.NewPCG(71, 5))
	for _, f := range []paymentvat.Figures{
		{GrossCents: 11900, VATCents: 1900},
		{GrossCents: 23757, VATCents: 3793},
		{GrossCents: 10700, VATCents: 700},
		{GrossCents: 1, VATCents: 0},
		{GrossCents: 99999, VATCents: 15966},
	} {
		figures := map[uuid.UUID]paymentvat.Figures{m[0].ID: f}
		for range 200 {
			l := paymentList{root: m[0].ID}
			left := f.GrossCents
			for left > 0 {
				amount := 1 + rng.Int64N(left)
				l.add("2026-11-0"+string(rune('2'+rng.IntN(7))), amount)
				left -= amount
			}
			if got := sum(vatOf(mustSplit(t, chain, figures, l.list))); got != f.VATCents {
				t.Fatalf("figures %+v, %d payments: VAT shares sum to %d, want %d", f, len(l.list), got, f.VATCents)
			}
		}
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

	before, m0 := chainOf(t)
	l := paymentList{root: m0[0].ID}
	l.add("2026-11-05", 5950)
	first := mustSplit(t, before, map[uuid.UUID]paymentvat.Figures{m0[0].ID: original}, l.list)

	// The same chain after the correction: same original id.
	members := []docchain.Member{m0[0], {ID: uuid.New(), Seq: 1, IssuedOn: day("2026-11-10")}}
	after, err := docchain.New(members)
	if err != nil {
		t.Fatal(err)
	}
	figures := map[uuid.UUID]paymentvat.Figures{members[0].ID: original, members[1].ID: corrected}
	l.add("2026-11-10", 4750)
	shares := mustSplit(t, after, figures, l.list)

	if shares[0] != first[0] {
		t.Fatalf("the correction changed the earlier share: %+v, was %+v", shares[0], first[0])
	}
	if shares[0].VATCents != 950 || shares[0].RatioDocumentID != members[0].ID {
		t.Fatalf("first share = %+v, want 950 cents by the original", shares[0])
	}
	// Exact: 950 + 4750 * 700 / 10700 = 950 + 310.75 = 1260.75, rounded
	// 1261, so the second share is 311.
	if shares[1].VATCents != 311 || shares[1].RatioDocumentID != members[1].ID {
		t.Fatalf("second share = %+v, want 311 cents by the correction", shares[1])
	}
}

// TestALaterPaymentNeverChangesAnEarlierShare: every prefix of a list of
// payments splits to the same shares the full list starts with.
func TestALaterPaymentNeverChangesAnEarlierShare(t *testing.T) {
	t.Parallel()

	chain, m := chainOf(t, "2026-11-15")
	figures := map[uuid.UUID]paymentvat.Figures{
		m[0].ID: {GrossCents: 11900, VATCents: 1900},
		m[1].ID: {GrossCents: 10700, VATCents: 700},
	}
	l := paymentList{root: m[0].ID}
	l.add("2026-11-02", 3333)
	l.add("2026-11-03", 3333)
	l.add("2026-11-20", 3334)
	l.add("2026-11-21", -1200)
	l.add("2026-11-22", 77)

	full := vatOf(mustSplit(t, chain, figures, l.list))
	for n := 1; n < len(l.list); n++ {
		if got := vatOf(mustSplit(t, chain, figures, l.list[:n])); !equal(got, full[:n]) {
			t.Fatalf("first %d payments split to %v, the full list starts with %v", n, got, full[:n])
		}
	}
}

func TestAReversalCancelsItsPaymentToTheCent(t *testing.T) {
	t.Parallel()

	chain, m := chainOf(t)
	figures := map[uuid.UUID]paymentvat.Figures{m[0].ID: {GrossCents: 119, VATCents: 19}}
	l := paymentList{root: m[0].ID}
	l.add("2026-11-02", 40)
	wrong := l.add("2026-11-03", 40)
	l.reverse("2026-11-04", wrong)
	l.add("2026-11-05", 79)

	shares := mustSplit(t, chain, figures, l.list)
	got := vatOf(shares)
	if got[2] != -got[1] {
		t.Fatalf("reversal share %d, reversed share %d, want them to cancel", got[2], got[1])
	}
	if shares[2].RatioDocumentID != shares[1].RatioDocumentID {
		t.Fatalf("the reversal uses document %s, the reversed payment %s", shares[2].RatioDocumentID, shares[1].RatioDocumentID)
	}
	// Paid in full: 40 + 79 = 119.
	if sum(got) != 19 {
		t.Fatalf("VAT shares %v sum to %d, want 19", got, sum(got))
	}
}

// TestARefundAfterACorrection: 119.00 EUR paid in full, then a correction
// to 107.00 EUR at 7 %, then a refund of 12.00 EUR. The refund carries
// the ratio of the correction, the effective document on its day.
func TestARefundAfterACorrection(t *testing.T) {
	t.Parallel()

	chain, m := chainOf(t, "2026-11-10")
	figures := map[uuid.UUID]paymentvat.Figures{
		m[0].ID: {GrossCents: 11900, VATCents: 1900},
		m[1].ID: {GrossCents: 10700, VATCents: 700},
	}
	l := paymentList{root: m[0].ID}
	l.add("2026-11-02", 11900)
	l.add("2026-11-12", -1200)

	shares := mustSplit(t, chain, figures, l.list)
	// Exact: 1900 - 1200 * 700 / 10700 = 1900 - 78.505 = 1821.495, rounded
	// 1821, so the refund's share is -79.
	if shares[0].VATCents != 1900 || shares[1].VATCents != -79 || shares[1].RatioDocumentID != m[1].ID {
		t.Fatalf("shares = %+v", shares)
	}
}

func TestInputOrderDoesNotMatter(t *testing.T) {
	t.Parallel()

	chain, m := chainOf(t)
	figures := map[uuid.UUID]paymentvat.Figures{m[0].ID: {GrossCents: 119, VATCents: 19}}
	l := paymentList{root: m[0].ID}
	a := l.add("2026-11-02", 40)
	b := l.add("2026-11-02", 40)
	c := l.add("2026-11-01", 39)

	shuffled := []storage.Payment{b, a, c}
	got := mustSplit(t, chain, figures, shuffled)
	if got[0].PaymentID != c.ID || got[1].PaymentID != a.ID || got[2].PaymentID != b.ID {
		t.Fatalf("order = %v, %v, %v, want received_on, then creation", got[0].PaymentID, got[1].PaymentID, got[2].PaymentID)
	}
}

func TestSplitRefusals(t *testing.T) {
	t.Parallel()

	chain, m := chainOf(t, "2026-11-10")
	figures := map[uuid.UUID]paymentvat.Figures{m[0].ID: {GrossCents: 119, VATCents: 19}}

	other := paymentList{root: uuid.New()}
	other.add("2026-11-02", 40)
	if _, err := paymentvat.Split(chain, figures, other.list); err == nil {
		t.Error("a payment of another chain was split")
	}

	l := paymentList{root: m[0].ID}
	l.reverse("2026-11-03", storage.Payment{ID: uuid.New(), AmountCents: 40})
	if _, err := paymentvat.Split(chain, figures, l.list); !errors.Is(err, paymentvat.ErrUnknownReversal) {
		t.Errorf("unknown reversal error = %v, want ErrUnknownReversal", err)
	}

	// The correction's figures are missing.
	l = paymentList{root: m[0].ID}
	l.add("2026-11-12", 40)
	if _, err := paymentvat.Split(chain, figures, l.list); err == nil {
		t.Error("a payment was split without the figures of its effective document")
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
