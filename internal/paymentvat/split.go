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
// rounded cumulatively, in the order of received_on, then creation, then
// id: the share of payment i is the running exact sum of all shares up to
// and including i, rounded half away from zero, minus the shares already
// handed out. So
//   - no cent is lost or invented: the shares always sum to the rounded
//     exact total, and a chain paid in full at one ratio sums to exactly
//     the document's VAT total, however it was split;
//   - a share depends only on the payments before it, so a later payment
//     never changes an earlier share.
//
// A reversal carries exactly the negated share of the payment it reverses,
// so a mistake and its reversal cancel to the cent. Its exact share is
// removed from the running sum as well. The two rounded sums can then
// differ by at most one cent until the next payment that is not a
// reversal, whose share absorbs the difference.
//
// Pure: no database, no clock, no I/O. Issue #80 (the EÜR report) calls
// Split with the payments of each chain it reports.
package paymentvat

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

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

// Split returns one Share per payment, in the order of received_on, then
// CreatedAt, then id. figures must hold every member of chain a payment's
// received_on can resolve to. Every payment must belong to the chain
// (DocumentID is the chain's original).
func Split(chain docchain.Chain, figures map[uuid.UUID]Figures, payments []storage.Payment) ([]Share, error) {
	// EffectiveOn of the zero time is the original: no correction was
	// issued before it.
	rootID := chain.EffectiveOn(time.Time{}).ID

	sorted := append([]storage.Payment(nil), payments...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if !a.ReceivedOn.Equal(b.ReceivedOn) {
			return a.ReceivedOn.Before(b.ReceivedOn)
		}
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
			exactSum.Sub(exactSum, original.exact)
			share := Share{
				PaymentID:       p.ID,
				RatioDocumentID: original.share.RatioDocumentID,
				GrossCents:      p.AmountCents,
				VATCents:        -original.share.VATCents,
			}
			share.NetCents = share.GrossCents - share.VATCents
			handedOut += share.VATCents
			byID[p.ID] = done{share: share, exact: new(big.Rat).Neg(original.exact)}
			out = append(out, share)

			continue
		}

		member := chain.EffectiveOn(p.ReceivedOn)
		f, ok := figures[member.ID]
		if !ok {
			return nil, fmt.Errorf("paymentvat: no figures for document %s", member.ID)
		}
		exact := exactShare(p.AmountCents, f)
		exactSum.Add(exactSum, exact)
		vat := roundHalfAwayFromZero(exactSum) - handedOut
		handedOut += vat

		share := Share{
			PaymentID:       p.ID,
			RatioDocumentID: member.ID,
			GrossCents:      p.AmountCents,
			VATCents:        vat,
			NetCents:        p.AmountCents - vat,
		}
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
