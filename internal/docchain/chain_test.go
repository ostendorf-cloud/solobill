package docchain_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/docchain"
)

var (
	original = uuid.MustParse("6f1c2a4e-0000-4000-8000-000000000010")
	first    = uuid.MustParse("6f1c2a4e-0000-4000-8000-000000000011")
	second   = uuid.MustParse("6f1c2a4e-0000-4000-8000-000000000012")
)

func mustChain(t *testing.T, members ...docchain.Member) docchain.Chain {
	t.Helper()

	c, err := docchain.New(members)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return c
}

// TestStaleCorrectionDraftIsRefused is the example of the contract:
// draft A is created while correction 1 is effective, correction 2 is
// issued meanwhile, and A must not be issued any more.
func TestStaleCorrectionDraftIsRefused(t *testing.T) {
	t.Parallel()

	before := mustChain(t, docchain.Member{ID: original, Seq: 0}, docchain.Member{ID: first, Seq: 1})
	draftA := before.BaseOf()
	if draftA != (docchain.Base{DocumentID: first, Seq: 1}) {
		t.Fatalf("BaseOf() = %+v, want correction 1", draftA)
	}
	if err := before.CheckBase(draftA); err != nil {
		t.Fatalf("CheckBase() before the second correction = %v, want nil", err)
	}

	after := mustChain(t,
		docchain.Member{ID: original, Seq: 0},
		docchain.Member{ID: first, Seq: 1},
		docchain.Member{ID: second, Seq: 2},
	)
	if err := after.CheckBase(draftA); !errors.Is(err, docchain.ErrCorrectionBaseChanged) {
		t.Errorf("CheckBase() after the second correction = %v, want ErrCorrectionBaseChanged", err)
	}
}

func TestEffectiveStatusAndNextSeq(t *testing.T) {
	t.Parallel()

	only := mustChain(t, docchain.Member{ID: original, Seq: 0})
	if only.NextSeq() != 1 {
		t.Errorf("NextSeq() of a lone invoice = %d, want 1", only.NextSeq())
	}
	if s, _ := only.StatusOf(original); s != docchain.StatusEffective {
		t.Errorf("StatusOf(original) = %q, want effective", s)
	}

	c := mustChain(t,
		docchain.Member{ID: second, Seq: 2},
		docchain.Member{ID: original, Seq: 0},
		docchain.Member{ID: first, Seq: 1},
	)
	if c.Effective().ID != second || c.NextSeq() != 3 {
		t.Errorf("Effective() = %+v, NextSeq() = %d, want correction 2 and 3", c.Effective(), c.NextSeq())
	}
	for id, want := range map[uuid.UUID]docchain.Status{
		original: docchain.StatusSuperseded,
		first:    docchain.StatusSuperseded,
		second:   docchain.StatusEffective,
	} {
		if got, ok := c.StatusOf(id); !ok || got != want {
			t.Errorf("StatusOf(%s) = %q, %v, want %q", id, got, ok, want)
		}
	}
	if _, ok := c.StatusOf(uuid.New()); ok {
		t.Error("StatusOf(stranger) reported a member")
	}
}

func TestNewRefusesBrokenChains(t *testing.T) {
	t.Parallel()

	for name, members := range map[string][]docchain.Member{
		"no original":       {{ID: first, Seq: 1}},
		"duplicate seq":     {{ID: original, Seq: 0}, {ID: first, Seq: 1}, {ID: second, Seq: 1}},
		"negative sequence": {{ID: original, Seq: 0}, {ID: first, Seq: -1}},
		"empty":             nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := docchain.New(members); err == nil {
				t.Error("New() error = nil")
			}
		})
	}
}
