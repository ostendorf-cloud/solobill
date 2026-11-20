// Package docchain holds the rules of an invoice chain under the replace
// model (docs/concepts/solobill.md, "Corrections"): which document is
// effective, which are superseded, the next correction sequence, and
// whether a correction draft is still based on the effective document.
//
// Pure: no database, no clock, no I/O. Issue #70 loads a chain inside the
// issuing transaction, under a lock on the root row, and asks this
// package.
package docchain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrCorrectionBaseChanged is returned by CheckBase when the chain's
// effective document is no longer the one the draft was created from.
// The API answers it as 409 correction_base_changed.
var ErrCorrectionBaseChanged = errors.New("docchain: the chain's effective document changed since the draft was created")

// Member is one issued document of a chain: the original invoice
// (Seq 0) or one of its corrections (Seq 1 or higher). Drafts are not
// members.
type Member struct {
	ID  uuid.UUID
	Seq int
	// IssuedOn is the document's issue date, used by EffectiveOn. Only its
	// calendar date counts.
	IssuedOn time.Time
}

// Status is the derived state of a member. It is never stored.
type Status string

const (
	// StatusEffective is the newest issued document of the chain.
	StatusEffective Status = "effective"
	// StatusSuperseded is every older issued document of the chain.
	StatusSuperseded Status = "superseded"
)

// Chain is the issued documents of one original invoice.
type Chain struct {
	members []Member
}

// New builds a chain. It needs exactly one original (Seq 0), distinct
// sequences and distinct ids.
func New(members []Member) (Chain, error) {
	seen := map[int]bool{}
	ids := map[uuid.UUID]bool{}
	for _, m := range members {
		if m.Seq < 0 {
			return Chain{}, fmt.Errorf("docchain: negative sequence %d", m.Seq)
		}
		if seen[m.Seq] {
			return Chain{}, fmt.Errorf("docchain: sequence %d appears twice", m.Seq)
		}
		if ids[m.ID] {
			return Chain{}, fmt.Errorf("docchain: document %s appears twice", m.ID)
		}
		seen[m.Seq] = true
		ids[m.ID] = true
	}
	if !seen[0] {
		return Chain{}, errors.New("docchain: the chain has no original invoice")
	}

	return Chain{members: append([]Member(nil), members...)}, nil
}

// Effective is the member with the highest sequence.
func (c Chain) Effective() Member {
	best := c.members[0]
	for _, m := range c.members[1:] {
		if m.Seq > best.Seq {
			best = m
		}
	}

	return best
}

// EffectiveOn is the member that was effective on day: the one with the
// highest sequence issued on or before that calendar day. Before the
// original's own issue date it is the original. This is the document
// whose VAT ratio a payment received on day uses (docs/concepts/
// solobill.md, "Payments"), for issue #71 and the EÜR report (#80).
func (c Chain) EffectiveOn(day time.Time) Member {
	cut := dateOnly(day)
	var best Member
	for _, m := range c.members {
		if m.Seq == 0 {
			if best.Seq == 0 {
				best = m
			}

			continue
		}
		if !dateOnly(m.IssuedOn).After(cut) && m.Seq > best.Seq {
			best = m
		}
	}

	return best
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()

	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// StatusOf is the derived status of the member with id, and whether id
// is a member at all.
func (c Chain) StatusOf(id uuid.UUID) (Status, bool) {
	for _, m := range c.members {
		if m.ID == id {
			if m == c.Effective() {
				return StatusEffective, true
			}

			return StatusSuperseded, true
		}
	}

	return "", false
}

// NextSeq is the sequence the next issued correction gets.
func (c Chain) NextSeq() int {
	return c.Effective().Seq + 1
}

// Base is what a correction draft records when it is created: the
// effective document it copied its lines from.
type Base struct {
	DocumentID uuid.UUID
	Seq        int
}

// BaseOf is the Base a new correction draft records now.
func (c Chain) BaseOf() Base {
	e := c.Effective()

	return Base{DocumentID: e.ID, Seq: e.Seq}
}

// CheckBase refuses to issue a correction draft whose base is no longer
// the effective document, for example because another correction was
// issued in the meantime. The draft would otherwise replace lines it
// never saw.
func (c Chain) CheckBase(base Base) error {
	if e := c.Effective(); e.ID != base.DocumentID || e.Seq != base.Seq {
		return fmt.Errorf("%w: draft based on sequence %d, effective is sequence %d", ErrCorrectionBaseChanged, base.Seq, e.Seq)
	}

	return nil
}
