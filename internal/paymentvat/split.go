// Package paymentvat splits the gross payments of one invoice chain into
// their net and VAT shares (docs/concepts/solobill.md, "Payments").
//
// Under the standard regime the VAT share of a partial payment is pro
// rata: the payment's amount times the VAT total over the grand total of
// the chain's effective document on the payment's received_on. That
// document comes from docchain.Chain.EffectiveOn, never from a second
// implementation here. A later correction therefore never changes the
// share of a payment received before it. Under the Kleinunternehmer
// regime the VAT total is 0, so every share is 0.
//
// NOT VERIFIED: the legal side of the pro rata split and of the "ratio of
// the payment's own day" rule (sections 13 and 17 UStG on partial
// payments and corrections) is an assumption the owner confirms with a
// Steuerberater (issue #71). The code is not tax advice.
//
// Rounding. Each payment's exact share is a fraction. The shares are
// rounded cumulatively, in the order the payments were recorded
// (CreatedAt, then id): the share of payment i is the running exact sum of
// all shares up to and including i, rounded half away from zero, minus the
// shares already handed out. So
//   - no cent is lost or invented: the shares always sum to the rounded
//     exact total, and a chain paid in full at one ratio sums to exactly
//     the document's VAT total, however it was split;
//   - a share depends only on the payments recorded before it. The table
//     is append-only, so a share never changes once its payment is
//     recorded, also when a reversal or a late payment is recorded later
//     with an earlier received_on. Ordering by received_on would let such
//     a row move the rounding of every share after its date.
//
// A reversal follows the same rule: its exact share is the negated exact
// share of the payment it reverses, and its rounded share is the rounded
// running sum minus what was handed out. So a chain paid in full sums to
// exactly the document's VAT total in every order, also when a reversal
// comes last. The price: a reversal's share may differ by one cent from
// the negated share of the payment it reverses. Example: gross 11900, VAT
// 1900, payments 101, 3 (a mistake), 11799, then the reversal of the 3:
// shares 16, 1, 1883, 0, sum 1900. Negating the 1 would give 1899.
//
// Pure: no database, no clock, no I/O. Issue #80 (the EÜR report) calls
// Split once per chain with the chain's whole payment history, never with
// a ListReceivedBetween slice of one year, and reports the shares whose
// received_on falls into the year.
package paymentvat

import (
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/docchain"
	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// Figures is what the split needs of one issued document of the chain:
// its grand total and its VAT total, both in cents, from the frozen
// payload.
type Figures struct {
	GrossCents int64
	VATCents   int64
}

// FiguresOf reads the figures of an issued document from its frozen
// payload.
func FiguresOf(d storage.Document) (Figures, error) {
	if d.State != storage.DocumentStateIssued || len(d.FrozenPayload) == 0 {
		return Figures{}, fmt.Errorf("paymentvat: document %s is not issued", d.ID)
	}
	p, err := docpayload.Unmarshal(d.FrozenPayload)
	if err != nil {
		return Figures{}, fmt.Errorf("paymentvat: document %s: %w", d.ID, err)
	}

	return Figures{GrossCents: p.Totals.GrandTotalCents, VATCents: p.Totals.VATTotalCents}, nil
}

// Share is the split of one payment.
type Share struct {
	PaymentID uuid.UUID
	// RatioDocumentID is the chain member whose ratio the share uses: the
	// effective document on the payment's received_on, or for a reversal
	// the one of the reversed payment.
	RatioDocumentID uuid.UUID
	GrossCents      int64
	VATCents        int64
	NetCents        int64
}

// ErrUnknownReversal is returned when a reversal names a payment that is
// not among the earlier payments of the chain.
var ErrUnknownReversal = errors.New("paymentvat: a reversal names no earlier payment of the chain")

// Split returns one Share per payment, in the order the payments were
// recorded: CreatedAt, then id.
//
// members is the whole chain: the original (Seq 0) and every issued
// correction, with IssuedOn set. figures must hold every member a
// payment's received_on can resolve to. payments must be every payment of
// the chain, from the first one on, never a slice of one year: a share
// depends on all payments before it. Every payment must belong to the
// chain (DocumentID is the original's id).
func Split(members []docchain.Member, figures map[uuid.UUID]Figures, payments []storage.Payment) ([]Share, error) {
	chain, err := docchain.New(members)
	if err != nil {
		return nil, fmt.Errorf("paymentvat: %w", err)
	}
	var rootID uuid.UUID
	for _, m := range members {
		if m.Seq == 0 {
			rootID = m.ID
		}
	}

	sorted := append([]storage.Payment(nil), payments...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}

		return a.ID.String() < b.ID.String()
	})

	type done struct {
		share Share
		exact *big.Rat
	}
	byID := make(map[uuid.UUID]done, len(sorted))

	exactSum := new(big.Rat)
	var handedOut int64
	// next adds exact to the running sum and hands out the rounded running
	// sum minus what was handed out before.
	next := func(p storage.Payment, ratioID uuid.UUID, exact *big.Rat) Share {
		exactSum.Add(exactSum, exact)
		vat := roundHalfAwayFromZero(exactSum) - handedOut
		handedOut += vat

		return Share{
			PaymentID:       p.ID,
			RatioDocumentID: ratioID,
			GrossCents:      p.AmountCents,
			VATCents:        vat,
			NetCents:        p.AmountCents - vat,
		}
	}
	out := make([]Share, 0, len(sorted))
	for _, p := range sorted {
		if p.DocumentID != rootID {
			return nil, fmt.Errorf("paymentvat: payment %s belongs to chain %s, not %s", p.ID, p.DocumentID, rootID)
		}

		if p.ReversesPaymentID != nil {
			original, ok := byID[*p.ReversesPaymentID]
			if !ok {
				return nil, fmt.Errorf("%w: payment %s", ErrUnknownReversal, p.ID)
			}
			// The reversal removes exactly the reversed exact share and is
			// rounded cumulatively like every other row.
			exact := new(big.Rat).Neg(original.exact)
			share := next(p, original.share.RatioDocumentID, exact)
			byID[p.ID] = done{share: share, exact: exact}
			out = append(out, share)

			continue
		}

		member := chain.EffectiveOn(p.ReceivedOn)
		f, ok := figures[member.ID]
		if !ok {
			return nil, fmt.Errorf("paymentvat: no figures for document %s", member.ID)
		}
		exact := exactShare(p.AmountCents, f)
		share := next(p, member.ID, exact)
		byID[p.ID] = done{share: share, exact: exact}
		out = append(out, share)
	}

	return out, nil
}

// exactShare is amount * VAT / gross as an exact fraction. A document
// without a grand total has no VAT to share.
func exactShare(amount int64, f Figures) *big.Rat {
	if f.GrossCents == 0 || f.VATCents == 0 {
		return new(big.Rat)
	}
	r := new(big.Rat).SetFrac(big.NewInt(f.VATCents), big.NewInt(f.GrossCents))

	return r.Mul(r, new(big.Rat).SetInt64(amount))
}

// roundHalfAwayFromZero rounds r to whole cents, a half away from zero,
// the rule docs/concepts/solobill.md's Rounding section uses for VAT.
func roundHalfAwayFromZero(r *big.Rat) int64 {
	num := new(big.Int).Abs(r.Num())
	den := r.Denom()
	q, rem := new(big.Int).QuoRem(num, den, new(big.Int))
	if rem.Lsh(rem, 1).Cmp(den) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if r.Sign() < 0 {
		q.Neg(q)
	}

	return q.Int64()
}
