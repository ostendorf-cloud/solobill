package docissue_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/docissue"
	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

func TestDraftPayload(t *testing.T) {
	t.Parallel()

	t.Run("an invoice draft prints as it is now, without a number", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, storage.VATRegimeKleinunternehmer)
		logoID := uuid.New()
		f.issuer.SellerProfileLogos = logos{id: &logoID}
		f.issuer.Files = files{file: storage.File{ID: logoID, SHA256: strings.Repeat("cd", 32)}}
		draft := f.draft(t, storage.Document{})

		p, err := f.issuer.DraftPayload(f.ctx, draft, f.profile, now)
		if err != nil {
			t.Fatalf("DraftPayload() error = %v", err)
		}
		if p.Number != "" || p.NumberValue != 0 || p.Corrects != nil {
			t.Errorf("number %q %d corrects %+v, want none", p.Number, p.NumberValue, p.Corrects)
		}
		if p.IssueDate != "2026-11-20" || p.TemplateVersion != docpayload.TemplateCurrent {
			t.Errorf("issue date %q template %q, want today and the current template", p.IssueDate, p.TemplateVersion)
		}
		if p.Customer.Name != f.customer.Name || p.Seller.LegalName != f.profile.LegalName {
			t.Errorf("customer %q seller %q, want the current ones", p.Customer.Name, p.Seller.LegalName)
		}
		if p.Seller.LogoSHA256 != strings.Repeat("cd", 32) {
			t.Errorf("logo_sha256 = %q, want the current logo's hash", p.Seller.LogoSHA256)
		}
		if got, _ := f.store.Get(draft.ID); got.State != storage.DocumentStateDraft || f.counterOf(t) != 0 {
			t.Error("building a draft payload changed the draft or the counter")
		}
	})

	t.Run("a correction draft names its original", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, storage.VATRegimeKleinunternehmer)
		original, err := f.issuer.Issue(f.ctx, f.draft(t, storage.Document{}).ID, now)
		if err != nil {
			t.Fatalf("Issue() error = %v", err)
		}
		draft := f.correctionDraft(t, original.ID, original, 8000)

		p, err := f.issuer.DraftPayload(f.ctx, draft, f.profile, now)
		if err != nil {
			t.Fatalf("DraftPayload() error = %v", err)
		}
		if p.Corrects == nil || p.Corrects.Number != "RE-20260001" || p.Corrects.IssueDate != "2026-11-20" {
			t.Errorf("corrects = %+v, want RE-20260001 of 2026-11-20", p.Corrects)
		}
	})

	t.Run("an issued document is no draft", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, storage.VATRegimeKleinunternehmer)
		issued, err := f.issuer.Issue(f.ctx, f.draft(t, storage.Document{}).ID, now)
		if err != nil {
			t.Fatalf("Issue() error = %v", err)
		}
		if _, err := f.issuer.DraftPayload(f.ctx, issued, f.profile, now); !errors.Is(err, docissue.ErrNotDraft) {
			t.Errorf("DraftPayload(issued) error = %v, want ErrNotDraft", err)
		}
	})
}

func (f *fixture) counterOf(t *testing.T) int64 {
	t.Helper()

	return f.store.Counter(f.tenant, storage.DocumentTypeInvoice, 2026)
}
