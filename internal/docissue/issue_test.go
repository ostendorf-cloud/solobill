package docissue_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/docissue"
	"github.com/ostendorf-cloud/solobill/internal/docissue/docissuetest"
	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

type tenantKey struct{}

func tenantOf(ctx context.Context) (uuid.UUID, bool) {
	t, ok := ctx.Value(tenantKey{}).(uuid.UUID)

	return t, ok
}

// periods is a VATRegimePeriodRepository over a fixed list.
type periods struct{ list []storage.VATRegimePeriod }

func (p *periods) List(context.Context) ([]storage.VATRegimePeriod, error) { return p.list, nil }

func (p *periods) Create(context.Context, storage.VATRegimePeriod) (storage.VATRegimePeriod, error) {
	return storage.VATRegimePeriod{}, errors.New("not supported")
}

func (p *periods) RegimeOn(_ context.Context, date time.Time) (storage.VATRegimePeriod, error) {
	var best *storage.VATRegimePeriod
	for i, period := range p.list {
		if period.ValidFrom.After(date) {
			continue
		}
		if best == nil || !period.ValidFrom.Before(best.ValidFrom) {
			best = &p.list[i]
		}
	}
	if best == nil {
		return storage.VATRegimePeriod{}, storage.ErrNotFound
	}

	return *best, nil
}

// customers is a CustomerRepository whose FindByID answers a map.
type customers struct {
	rows map[uuid.UUID]storage.Customer
}

func (c *customers) Create(context.Context, storage.Customer) (storage.Customer, error) {
	return storage.Customer{}, errors.New("not supported")
}

func (c *customers) FindByID(ctx context.Context, id uuid.UUID) (storage.Customer, error) {
	tenant, _ := tenantOf(ctx)
	row, ok := c.rows[id]
	if !ok || row.TenantID != tenant {
		return storage.Customer{}, storage.ErrNotFound
	}

	return row, nil
}

func (c *customers) List(context.Context) ([]storage.Customer, error) { return nil, nil }

func (c *customers) Update(context.Context, storage.Customer) (storage.Customer, error) {
	return storage.Customer{}, errors.New("not supported")
}

func (c *customers) Delete(context.Context, uuid.UUID) error { return errors.New("not supported") }

func day(s string) time.Time {
	t, err := time.Parse(docpayload.DateLayout, s)
	if err != nil {
		panic(err)
	}

	return t
}

func ptr[T any](v T) *T { return &v }

// fixture is one tenant with a profile, a customer, the periods and the
// store.
type fixture struct {
	ctx      context.Context
	tenant   uuid.UUID
	store    *docissuetest.Store
	periods  *periods
	customer storage.Customer
	profile  storage.SellerProfile
	issuer   docissue.Issuer
}

func newFixture(t *testing.T, regime storage.VATRegime) *fixture {
	t.Helper()

	f := &fixture{tenant: uuid.New()}
	f.ctx = context.WithValue(context.Background(), tenantKey{}, f.tenant)
	f.profile = storage.SellerProfile{
		TenantID:         f.tenant,
		LegalName:        "Example Trading",
		Street:           "Main Street 1",
		PostalCode:       "12345",
		City:             "Example City",
		CountryCode:      "DE",
		TaxNumber:        ptr("12/345/67890"),
		DefaultVATRegime: regime,
		DefaultLanguage:  storage.DocumentLanguageGerman,
	}
	f.store = docissuetest.New(tenantOf, func(context.Context) (storage.SellerProfile, error) {
		return f.profile, nil
	})
	f.periods = &periods{list: []storage.VATRegimePeriod{{ID: uuid.New(), TenantID: f.tenant, ValidFrom: day("2026-01-01"), Regime: regime}}}
	f.customer = storage.Customer{
		ID:               uuid.New(),
		TenantID:         f.tenant,
		Name:             "Customer Ltd",
		Street:           ptr("Side Street 2"),
		PostalCode:       ptr("54321"),
		City:             ptr("Other City"),
		CountryCode:      ptr("DE"),
		DocumentLanguage: storage.DocumentLanguageGerman,
	}
	f.issuer = docissue.Issuer{
		Documents:        f.store,
		DocumentNumbers:  f.store,
		VATRegimePeriods: f.periods,
		Customers:        &customers{rows: map[uuid.UUID]storage.Customer{f.customer.ID: f.customer}},
		Checker:          passChecker{},
	}

	return f
}

// now is a plain November day in Germany.
var now = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

func (f *fixture) draft(t *testing.T, d storage.Document, lines ...storage.DocumentLine) storage.Document {
	t.Helper()

	d.TenantID = f.tenant
	d.CustomerID = f.customer.ID
	if d.Type == "" {
		d.Type = storage.DocumentTypeInvoice
	}
	if d.VATRegime == "" {
		d.VATRegime = f.periods.list[0].Regime
	}
	if d.ServiceDate == nil && d.Type != storage.DocumentTypeOffer {
		d.ServiceDate = ptr(day("2026-11-01"))
		d.ServiceDateTo = ptr(day("2026-11-30"))
	}
	if len(lines) == 0 {
		rate := int32(0)
		if d.VATRegime == storage.VATRegimeStandard {
			rate = 1900
		}
		lines = []storage.DocumentLine{{Description: "Consulting", QuantityThousandths: 2500, Unit: "hour", UnitPriceCents: 9500, VATRatePermyriad: rate}}
	}
	created, err := f.store.Create(f.ctx, d, lines)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	return created
}

func payloadOf(t *testing.T, d storage.Document) docpayload.FrozenPayloadV1 {
	t.Helper()

	p, err := docpayload.Unmarshal(d.FrozenPayload)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	sum := sha256.Sum256(d.FrozenPayload)
	if d.FrozenPayloadSHA256 == nil || *d.FrozenPayloadSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("frozen_payload_sha256 = %v, want the hash of the stored bytes", d.FrozenPayloadSHA256)
	}
	if problems := docpayload.Validate(p); len(problems) > 0 {
		t.Fatalf("the frozen payload does not validate: %v", problems)
	}

	return p
}

func TestIssueKleinunternehmerFreezesTheNoteFromTheRegime(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		lang storage.DocumentLanguage
		want string
	}{
		{storage.DocumentLanguageGerman, docissue.TaxNoteKleinunternehmerDE},
		{storage.DocumentLanguageEnglish, docissue.TaxNoteKleinunternehmerEN},
	} {
		t.Run(string(tt.lang), func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, storage.VATRegimeKleinunternehmer)
			draft := f.draft(t, storage.Document{Language: tt.lang})

			issued, err := f.issuer.Issue(f.ctx, draft.ID, now)
			if err != nil {
				t.Fatalf("Issue() error = %v", err)
			}
			if issued.State != storage.DocumentStateIssued || issued.DisplayNumber == nil || *issued.DisplayNumber != "RE-20260001" {
				t.Fatalf("issued = %+v, want state issued and number RE-20260001", issued)
			}
			p := payloadOf(t, issued)
			if p.TaxNote != tt.want {
				t.Errorf("tax_note = %q, want %q", p.TaxNote, tt.want)
			}
			if p.VATRegime != docpayload.RegimeKleinunternehmer || p.Totals.VATTotalCents != 0 || p.Totals.GrandTotalCents != 23750 {
				t.Errorf("payload regime %q totals %+v, want kleinunternehmer, no VAT, 23750", p.VATRegime, p.Totals)
			}
			if p.TemplateVersion != docpayload.TemplateDocumentV1 || p.Number != "RE-20260001" || p.NumberValue != 1 || p.NumberYear != 2026 {
				t.Errorf("payload template %q number %q %d/%d", p.TemplateVersion, p.Number, p.NumberValue, p.NumberYear)
			}
			if p.IssueDate != "2026-11-20" {
				t.Errorf("issue_date = %q, want 2026-11-20", p.IssueDate)
			}
		})
	}
}

func TestIssueStandardFreezesTheMoneyTotals(t *testing.T) {
	t.Parallel()

	f := newFixture(t, storage.VATRegimeStandard)
	// The worked example of docs/concepts/solobill.md's Rounding section.
	draft := f.draft(t, storage.Document{DiscountPercentPermyriad: ptr(int32(1500))},
		storage.DocumentLine{Description: "Consulting", QuantityThousandths: 1000, Unit: "piece", UnitPriceCents: 12004, VATRatePermyriad: 1900},
		storage.DocumentLine{Description: "Support", QuantityThousandths: 1000, Unit: "piece", UnitPriceCents: 7004, VATRatePermyriad: 1900},
	)

	issued, err := f.issuer.Issue(f.ctx, draft.ID, now)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	p := payloadOf(t, issued)
	if p.Totals.NetAfterDocumentDiscountCents != 16157 || p.Totals.VATTotalCents != 3070 || p.Totals.GrandTotalCents != 19227 {
		t.Errorf("totals = %+v, want 161.57 net, 30.70 VAT, 192.27", p.Totals)
	}
	if p.Lines[0].DocumentDiscountShareCents != 1801 || p.Lines[1].DocumentDiscountShareCents != 1050 {
		t.Errorf("discount shares = %d, %d, want 1801 and 1050", p.Lines[0].DocumentDiscountShareCents, p.Lines[1].DocumentDiscountShareCents)
	}
	if p.TaxNote != "" {
		t.Errorf("tax_note = %q, want none under the standard regime", p.TaxNote)
	}
	if p.Seller.TaxNumber != "12/345/67890" || p.Customer.Name != "Customer Ltd" {
		t.Errorf("snapshots = %+v / %+v", p.Seller, p.Customer)
	}
}

func TestIssuePicksThePeriodOfTheIssueDate(t *testing.T) {
	t.Parallel()

	switchDay := day("2026-10-14")
	tests := []struct {
		name   string
		draft  storage.VATRegime
		now    time.Time
		refuse storage.VATRegime
	}{
		{"kleinunternehmer the day before the switch", storage.VATRegimeKleinunternehmer, switchDay.Add(-12 * time.Hour), ""},
		{"kleinunternehmer on the switch day", storage.VATRegimeKleinunternehmer, switchDay.Add(10 * time.Hour), storage.VATRegimeStandard},
		{"standard the day before the switch", storage.VATRegimeStandard, switchDay.Add(-12 * time.Hour), storage.VATRegimeKleinunternehmer},
		{"standard on the switch day", storage.VATRegimeStandard, switchDay.Add(10 * time.Hour), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, storage.VATRegimeKleinunternehmer)
			f.periods.list = append(f.periods.list, storage.VATRegimePeriod{ID: uuid.New(), TenantID: f.tenant, ValidFrom: switchDay, Regime: storage.VATRegimeStandard})
			f.profile.KleinunternehmerID = ptr("KU-1")
			draft := f.draft(t, storage.Document{VATRegime: tt.draft})

			_, err := f.issuer.Issue(f.ctx, draft.ID, tt.now)
			if tt.refuse == "" {
				if err != nil {
					t.Fatalf("Issue() error = %v, want issued", err)
				}

				return
			}
			var mismatch *docissue.RegimeMismatchError
			if !errors.As(err, &mismatch) || mismatch.Regime != tt.refuse {
				t.Fatalf("Issue() error = %v, want a mismatch naming %q", err, tt.refuse)
			}
			if got := f.store.Counter(f.tenant, storage.DocumentTypeInvoice, 2026); got != 0 {
				t.Errorf("counter = %d after a refused issue, want 0", got)
			}
		})
	}
}

func TestIssueWithoutAPeriodIsRegimeMissing(t *testing.T) {
	t.Parallel()

	f := newFixture(t, storage.VATRegimeKleinunternehmer)
	f.periods.list[0].ValidFrom = day("2027-01-01")
	draft := f.draft(t, storage.Document{})

	if _, err := f.issuer.Issue(f.ctx, draft.ID, now); !errors.Is(err, docissue.ErrRegimeMissing) {
		t.Fatalf("Issue() error = %v, want ErrRegimeMissing", err)
	}
}

func TestIssueRefusesAnIncompleteSellerProfile(t *testing.T) {
	t.Parallel()

	f := newFixture(t, storage.VATRegimeStandard)
	f.profile.TaxNumber = nil
	f.profile.Street = ""
	draft := f.draft(t, storage.Document{})

	_, err := f.issuer.Issue(f.ctx, draft.ID, now)
	var invalid *docissue.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("Issue() error = %v, want a ValidationError", err)
	}
	for _, path := range []string{"seller.tax_number", "seller.street"} {
		if _, ok := invalid.Details[path]; !ok {
			t.Errorf("details = %v, want %q from sellerprofile.Validate", invalid.Details, path)
		}
	}
	if got := f.store.Counter(f.tenant, storage.DocumentTypeInvoice, 2026); got != 0 {
		t.Errorf("counter = %d, want 0: a refused issue never touches the counter", got)
	}
	if stored, _ := f.store.Get(draft.ID); stored.State != storage.DocumentStateDraft {
		t.Errorf("state = %q, want the draft to stay a draft", stored.State)
	}
}

func TestIssueRefusesWhatTheRendererCannotPrint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(f *fixture, d *storage.Document)
		path   string
	}{
		{"customer name with an emoji", func(f *fixture, _ *storage.Document) {
			c := f.customer
			c.Name = "Shop \U0001F600"
			f.issuer.Customers = &customers{rows: map[uuid.UUID]storage.Customer{c.ID: c}}
		}, "customer.name"},
		{"customer country that is not assigned", func(f *fixture, _ *storage.Document) {
			c := f.customer
			c.CountryCode = ptr("XX")
			f.issuer.Customers = &customers{rows: map[uuid.UUID]storage.Customer{c.ID: c}}
		}, "customer.address.country_code"},
		{"description over the limit", func(_ *fixture, d *storage.Document) {
			d.Lines[0].Description = strings.Repeat("a", docpayload.MaxTextChars+1)
		}, "lines[0].description"},
		{"notes over the line count", func(_ *fixture, d *storage.Document) {
			d.Notes = ptr(strings.Repeat("a\n", docpayload.MaxTextLines+1))
		}, "notes"},
		{"seller website over the seller limit", func(f *fixture, _ *storage.Document) {
			f.profile.Website = ptr("https://example.test/" + strings.Repeat("a", docpayload.MaxSellerChars))
		}, "seller.website"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, storage.VATRegimeKleinunternehmer)
			d := storage.Document{
				TenantID:    f.tenant,
				Type:        storage.DocumentTypeInvoice,
				State:       storage.DocumentStateDraft,
				CustomerID:  f.customer.ID,
				VATRegime:   storage.VATRegimeKleinunternehmer,
				ServiceDate: ptr(day("2026-11-01")),
				Lines:       []storage.DocumentLine{{Description: "Consulting", QuantityThousandths: 1000, Unit: "hour", UnitPriceCents: 9500}},
			}
			tt.mutate(f, &d)
			// Put, not Create: the draft routes refuse such text already,
			// the issue check must hold on its own.
			draft := f.store.Put(d)

			_, err := f.issuer.Issue(f.ctx, draft.ID, now)
			var invalid *docissue.ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("Issue() error = %v, want a ValidationError", err)
			}
			if _, ok := invalid.Details[tt.path]; !ok {
				t.Fatalf("details = %v, want %q", invalid.Details, tt.path)
			}
			if got := f.store.Counter(f.tenant, storage.DocumentTypeInvoice, 2026); got != 0 {
				t.Errorf("counter = %d, want 0", got)
			}
		})
	}
}

func TestIssueTwiceIsRefused(t *testing.T) {
	t.Parallel()

	f := newFixture(t, storage.VATRegimeKleinunternehmer)
	draft := f.draft(t, storage.Document{})
	first, err := f.issuer.Issue(f.ctx, draft.ID, now)
	if err != nil {
		t.Fatalf("first Issue() error = %v", err)
	}

	if _, err := f.issuer.Issue(f.ctx, draft.ID, now); !errors.Is(err, docissue.ErrNotDraft) {
		t.Fatalf("second Issue() error = %v, want ErrNotDraft", err)
	}
	if got := f.store.Counter(f.tenant, storage.DocumentTypeInvoice, 2026); got != 1 {
		t.Errorf("counter = %d, want 1", got)
	}
	again, _ := f.store.Get(draft.ID)
	if !bytes.Equal(again.FrozenPayload, first.FrozenPayload) {
		t.Errorf("the frozen payload changed on a second issue")
	}
}

func TestIssueRefusesATakenDisplayNumber(t *testing.T) {
	t.Parallel()

	f := newFixture(t, storage.VATRegimeKleinunternehmer)
	// An imported document already holds the number the counter renders
	// next.
	f.store.Put(storage.Document{
		TenantID: f.tenant, Type: storage.DocumentTypeInvoice, State: storage.DocumentStateIssued,
		CustomerID: f.customer.ID, VATRegime: storage.VATRegimeKleinunternehmer, Imported: true,
		DisplayNumber: ptr("RE-20260001"), OriginalNumber: ptr("RE-20260001"),
		Number: ptr(int64(1001)), NumberYear: ptr(int32(2026)),
	})
	draft := f.draft(t, storage.Document{})

	if _, err := f.issuer.Issue(f.ctx, draft.ID, now); !errors.Is(err, docissue.ErrNumberTaken) {
		t.Fatalf("Issue() error = %v, want ErrNumberTaken", err)
	}
}

func TestIssueDateIsTheGermanCalendarDay(t *testing.T) {
	t.Parallel()

	f := newFixture(t, storage.VATRegimeKleinunternehmer)
	draft := f.draft(t, storage.Document{})
	// 23:30 UTC on 31 December is 00:30 on 1 January in Germany.
	issued, err := f.issuer.Issue(f.ctx, draft.ID, time.Date(2026, 12, 31, 23, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	p := payloadOf(t, issued)
	if p.IssueDate != "2027-01-01" || *issued.DisplayNumber != "RE-20270001" || *issued.NumberYear != 2027 {
		t.Fatalf("issue date %q number %q year %d, want 2027-01-01, RE-20270001, 2027", p.IssueDate, *issued.DisplayNumber, *issued.NumberYear)
	}
}

func TestIssueOfferUsesItsOwnCounterAndTemplate(t *testing.T) {
	t.Parallel()

	f := newFixture(t, storage.VATRegimeKleinunternehmer)
	invoice := f.draft(t, storage.Document{})
	offer := f.draft(t, storage.Document{Type: storage.DocumentTypeOffer, ValidUntil: ptr(day("2026-12-31"))})

	if _, err := f.issuer.Issue(f.ctx, invoice.ID, now); err != nil {
		t.Fatalf("Issue(invoice) error = %v", err)
	}
	issued, err := f.issuer.Issue(f.ctx, offer.ID, now)
	if err != nil {
		t.Fatalf("Issue(offer) error = %v", err)
	}
	if *issued.DisplayNumber != "AN-20260001" {
		t.Errorf("offer number = %q, want AN-20260001", *issued.DisplayNumber)
	}
	p := payloadOf(t, issued)
	if p.ServicePeriod != nil || p.ValidUntil != "2026-12-31" {
		t.Errorf("offer payload service %v valid_until %q", p.ServicePeriod, p.ValidUntil)
	}
}

func TestIssueUsesTheProfileTemplate(t *testing.T) {
	t.Parallel()

	f := newFixture(t, storage.VATRegimeKleinunternehmer)
	f.profile.NumberFormats = map[storage.DocumentType]string{storage.DocumentTypeInvoice: "INV/{yy}-{seq:3}"}
	draft := f.draft(t, storage.Document{})

	issued, err := f.issuer.Issue(f.ctx, draft.ID, now)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if *issued.DisplayNumber != "INV/26-001" {
		t.Errorf("number = %q, want INV/26-001", *issued.DisplayNumber)
	}
}

// correctionDraft is a correction of root based on base, as the
// correction route creates it.
func (f *fixture) correctionDraft(t *testing.T, root uuid.UUID, base storage.Document, price int64) storage.Document {
	t.Helper()

	seq := int32(0)
	if base.CorrectionSeq != nil {
		seq = *base.CorrectionSeq
	}

	return f.draft(t, storage.Document{
		Type:               storage.DocumentTypeCorrection,
		CorrectsDocumentID: &root,
		CorrectionBaseID:   &base.ID,
		CorrectionBaseSeq:  &seq,
	}, storage.DocumentLine{Description: "Consulting, corrected", QuantityThousandths: 1000, Unit: "hour", UnitPriceCents: price})
}

func TestCorrectionChain(t *testing.T) {
	t.Parallel()

	f := newFixture(t, storage.VATRegimeKleinunternehmer)
	original, err := f.issuer.Issue(f.ctx, f.draft(t, storage.Document{}).ID, now)
	if err != nil {
		t.Fatalf("Issue(original) error = %v", err)
	}
	before, _ := f.store.Get(original.ID)

	draft1 := f.correctionDraft(t, original.ID, original, 8000)
	staleOnOriginal := f.correctionDraft(t, original.ID, original, 7000)

	first, err := f.issuer.Issue(f.ctx, draft1.ID, now.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("Issue(correction 1) error = %v", err)
	}
	if *first.DisplayNumber != "RE-20260001-1" || first.CorrectionSeq == nil || *first.CorrectionSeq != 1 {
		t.Fatalf("correction 1 = %q seq %v, want RE-20260001-1 and 1", *first.DisplayNumber, first.CorrectionSeq)
	}
	if *first.Number != 1 || *first.NumberYear != 2026 || *first.CorrectsDocumentID != original.ID {
		t.Errorf("correction 1 key %d/%d corrects %v, want the original's", *first.Number, *first.NumberYear, first.CorrectsDocumentID)
	}
	p := payloadOf(t, first)
	if p.Corrects == nil || p.Corrects.ID != original.ID.String() || p.Corrects.Number != "RE-20260001" || p.Corrects.IssueDate != "2026-11-20" {
		t.Errorf("corrects = %+v, want the original", p.Corrects)
	}
	if p.CorrectionSeq != 1 || p.Lines[0].UnitPriceCents != 8000 {
		t.Errorf("payload seq %d lines %+v, want 1 and the full corrected line", p.CorrectionSeq, p.Lines)
	}

	// The concept's example: draft on sequence 1, sequence 2 issued
	// meanwhile, the draft is refused.
	draft2 := f.correctionDraft(t, original.ID, first, 6000)
	draftA := f.correctionDraft(t, original.ID, first, 5000)
	second, err := f.issuer.Issue(f.ctx, draft2.ID, now.Add(48*time.Hour))
	if err != nil {
		t.Fatalf("Issue(correction 2) error = %v", err)
	}
	if *second.DisplayNumber != "RE-20260001-2" {
		t.Fatalf("correction 2 = %q, want RE-20260001-2", *second.DisplayNumber)
	}

	_, err = f.issuer.Issue(f.ctx, draftA.ID, now.Add(72*time.Hour))
	var changed *docissue.BaseChangedError
	if !errors.As(err, &changed) || changed.Seq != 2 {
		t.Fatalf("Issue(draft A) error = %v, want correction_base_changed with sequence 2", err)
	}
	if stored, _ := f.store.Get(draftA.ID); stored.State != storage.DocumentStateDraft {
		t.Errorf("draft A state = %q, want it to stay a draft", stored.State)
	}

	_, err = f.issuer.Issue(f.ctx, staleOnOriginal.ID, now.Add(72*time.Hour))
	if !errors.As(err, &changed) || changed.Seq != 2 {
		t.Fatalf("Issue(draft on the original) error = %v, want correction_base_changed with sequence 2", err)
	}

	// The original never changes, and no correction touched the counter.
	after, _ := f.store.Get(original.ID)
	if !bytes.Equal(before.FrozenPayload, after.FrozenPayload) || *after.DisplayNumber != "RE-20260001" ||
		after.State != storage.DocumentStateIssued || after.UpdatedAt != before.UpdatedAt {
		t.Errorf("the original changed: before %+v after %+v", before, after)
	}
	if got := f.store.Counter(f.tenant, storage.DocumentTypeInvoice, 2026); got != 1 {
		t.Errorf("invoice counter = %d, want 1: a correction never allocates", got)
	}
	if got := f.store.Counter(f.tenant, storage.DocumentTypeCorrection, 2026); got != 0 {
		t.Errorf("correction counter = %d, want 0", got)
	}

	chain, err := f.store.Chain(f.ctx, original.ID)
	if err != nil {
		t.Fatalf("Chain() error = %v", err)
	}
	statuses := docissue.Statuses(chain)
	if statuses[second.ID] != "effective" || statuses[first.ID] != "superseded" || statuses[original.ID] != "superseded" {
		t.Errorf("statuses = %v, want correction 2 effective and the rest superseded", statuses)
	}
}

func TestIssueLocksTheDraftAndTheRootFirst(t *testing.T) {
	t.Parallel()

	f := newFixture(t, storage.VATRegimeKleinunternehmer)
	original, err := f.issuer.Issue(f.ctx, f.draft(t, storage.Document{}).ID, now)
	if err != nil {
		t.Fatalf("Issue(original) error = %v", err)
	}
	draft := f.correctionDraft(t, original.ID, original, 8000)
	before := len(f.store.Locked())

	if _, err := f.issuer.Issue(f.ctx, draft.ID, now); err != nil {
		t.Fatalf("Issue(correction) error = %v", err)
	}
	locked := f.store.Locked()[before:]
	if len(locked) != 2 || locked[0] != draft.ID || locked[1] != original.ID {
		t.Fatalf("locked = %v, want the draft, then the chain's root", locked)
	}
}

func TestIssueFreezesTheLogoHash(t *testing.T) {
	t.Parallel()

	f := newFixture(t, storage.VATRegimeKleinunternehmer)
	logoID := uuid.New()
	f.issuer.SellerProfileLogos = logos{id: &logoID}
	f.issuer.Files = files{file: storage.File{ID: logoID, SHA256: strings.Repeat("ab", 32)}}
	draft := f.draft(t, storage.Document{})

	issued, err := f.issuer.Issue(f.ctx, draft.ID, now)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if got := payloadOf(t, issued).Seller.LogoSHA256; got != strings.Repeat("ab", 32) {
		t.Errorf("logo_sha256 = %q, want the logo file's hash", got)
	}
}

type logos struct{ id *uuid.UUID }

func (l logos) Logo(context.Context) (*uuid.UUID, error)  { return l.id, nil }
func (l logos) SetLogo(context.Context, *uuid.UUID) error { return errors.New("not supported") }

type files struct{ file storage.File }

func (f files) Create(context.Context, storage.File) (storage.File, error) {
	return storage.File{}, errors.New("not supported")
}

func (f files) FindByID(_ context.Context, id uuid.UUID) (storage.File, error) {
	if id != f.file.ID {
		return storage.File{}, storage.ErrNotFound
	}

	return f.file, nil
}

func (f files) FindBySHA256(context.Context, string) ([]storage.File, error) { return nil, nil }

// passChecker is a PayloadChecker that accepts every payload, the
// renderer's check as the tests assume it.
type passChecker struct{}

func (passChecker) CheckPayload(context.Context, docpayload.FrozenPayloadV1) (map[string]string, error) {
	return nil, nil
}

func TestIssueFailsClosedWithoutARendererCheck(t *testing.T) {
	t.Parallel()

	f := newFixture(t, storage.VATRegimeKleinunternehmer)
	f.issuer.Checker = nil
	draft := f.draft(t, storage.Document{})

	if _, err := f.issuer.Issue(f.ctx, draft.ID, now); !errors.Is(err, docissue.ErrRendererUnavailable) {
		t.Fatalf("Issue() error = %v, want ErrRendererUnavailable", err)
	}
	if locked := f.store.Locked(); len(locked) != 0 {
		t.Errorf("locked = %v, want nothing locked before the renderer check", locked)
	}
	if got := f.store.Counter(f.tenant, storage.DocumentTypeInvoice, 2026); got != 0 {
		t.Errorf("counter = %d, want 0", got)
	}
	if stored, _ := f.store.Get(draft.ID); stored.State != storage.DocumentStateDraft {
		t.Errorf("state = %q, want draft", stored.State)
	}
}

func TestIssueRefusesADeletedCustomer(t *testing.T) {
	t.Parallel()

	f := newFixture(t, storage.VATRegimeKleinunternehmer)
	draft := f.draft(t, storage.Document{})
	deleted := f.customer
	deleted.DeletedAt = ptr(now.Add(-time.Hour))
	f.issuer.Customers = &customers{rows: map[uuid.UUID]storage.Customer{deleted.ID: deleted}}

	_, err := f.issuer.Issue(f.ctx, draft.ID, now)
	var invalid *docissue.ValidationError
	if !errors.As(err, &invalid) || !strings.Contains(invalid.Details["customer_id"], "deleted") {
		t.Fatalf("Issue() error = %v, want a customer_id field error naming the deleted customer", err)
	}
	if got := f.store.Counter(f.tenant, storage.DocumentTypeInvoice, 2026); got != 0 {
		t.Errorf("counter = %d, want 0", got)
	}
}

func TestIssueNamesATaxExemptLineUnderTheStandardRegime(t *testing.T) {
	t.Parallel()

	f := newFixture(t, storage.VATRegimeStandard)
	// Put, not Create: the draft routes refuse such a line already.
	draft := f.store.Put(storage.Document{
		TenantID: f.tenant, Type: storage.DocumentTypeInvoice, State: storage.DocumentStateDraft,
		CustomerID: f.customer.ID, VATRegime: storage.VATRegimeStandard, ServiceDate: ptr(day("2026-11-01")),
		Lines: []storage.DocumentLine{
			{Description: "Consulting", QuantityThousandths: 1000, Unit: "hour", UnitPriceCents: 9500, VATRatePermyriad: 1900},
			{Description: "Exempt", QuantityThousandths: 1000, Unit: "piece", UnitPriceCents: 100},
		},
	})

	_, err := f.issuer.Issue(f.ctx, draft.ID, now)
	var invalid *docissue.ValidationError
	if !errors.As(err, &invalid) || invalid.Details["lines[1].vat_rate_permyriad"] != docissue.ReasonExemptLine {
		t.Fatalf("Issue() error = %v, want lines[1].vat_rate_permyriad: %s", err, docissue.ReasonExemptLine)
	}
	if _, ok := invalid.Details["lines[0].vat_rate_permyriad"]; ok {
		t.Errorf("details = %v, want only the exempt line named", invalid.Details)
	}
}

func TestCorrectionAcrossARegimeChangeIsRefused(t *testing.T) {
	t.Parallel()

	f := newFixture(t, storage.VATRegimeKleinunternehmer)
	original, err := f.issuer.Issue(f.ctx, f.draft(t, storage.Document{}).ID, now)
	if err != nil {
		t.Fatalf("Issue(original) error = %v", err)
	}
	f.periods.list = append(f.periods.list, storage.VATRegimePeriod{
		ID: uuid.New(), TenantID: f.tenant, ValidFrom: day("2026-12-01"), Regime: storage.VATRegimeStandard,
	})
	f.profile.VATID = ptr("DE123456789")

	for _, regime := range []storage.VATRegime{storage.VATRegimeKleinunternehmer, storage.VATRegimeStandard} {
		draft := f.correctionDraft(t, original.ID, original, 8000)
		// The draft keeps the original's regime, or the owner set the
		// period's regime: both are refused with the specific error.
		stored, _ := f.store.Get(draft.ID)
		stored.VATRegime = regime
		f.store.Put(stored)

		_, err := f.issuer.Issue(f.ctx, draft.ID, day("2026-12-02").Add(10*time.Hour))
		var changed *docissue.CorrectionRegimeError
		if !errors.As(err, &changed) {
			t.Fatalf("Issue(correction, draft %s) error = %v, want a CorrectionRegimeError", regime, err)
		}
		if changed.OriginalRegime != storage.VATRegimeKleinunternehmer || changed.PeriodRegime != storage.VATRegimeStandard ||
			changed.OriginalIssueDate != "2026-11-20" || changed.IssueDate != "2026-12-02" {
			t.Errorf("error = %+v, want both regimes and both dates", changed)
		}
	}
}

func TestACorrectionOfAnOfferIsRefused(t *testing.T) {
	t.Parallel()

	f := newFixture(t, storage.VATRegimeKleinunternehmer)
	offer, err := f.issuer.Issue(f.ctx, f.draft(t, storage.Document{Type: storage.DocumentTypeOffer}).ID, now)
	if err != nil {
		t.Fatalf("Issue(offer) error = %v", err)
	}
	if _, ok := docissue.RootOf(offer); ok {
		t.Fatalf("RootOf(offer) = true, want an offer to belong to no chain")
	}

	// A correction draft naming an offer, put past the routes.
	seq := int32(0)
	draft := f.store.Put(storage.Document{
		TenantID: f.tenant, Type: storage.DocumentTypeCorrection, State: storage.DocumentStateDraft,
		CustomerID: f.customer.ID, VATRegime: storage.VATRegimeKleinunternehmer,
		CorrectsDocumentID: &offer.ID, CorrectionBaseID: &offer.ID, CorrectionBaseSeq: &seq,
		Lines: []storage.DocumentLine{{Description: "Consulting", QuantityThousandths: 1000, Unit: "hour", UnitPriceCents: 9500}},
	})
	if _, err := f.issuer.Issue(f.ctx, draft.ID, now); err == nil {
		t.Fatalf("a correction of an offer was issued")
	}
}

func TestIssueDateAcrossDaylightSavingTime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{"winter time, one hour ahead of UTC", time.Date(2026, 3, 28, 23, 30, 0, 0, time.UTC), "2026-03-29"},
		{"night of the switch to summer time", time.Date(2026, 3, 29, 0, 30, 0, 0, time.UTC), "2026-03-29"},
		{"summer time, two hours ahead", time.Date(2026, 3, 29, 22, 30, 0, 0, time.UTC), "2026-03-30"},
		{"last evening of summer time", time.Date(2026, 10, 24, 21, 30, 0, 0, time.UTC), "2026-10-24"},
		{"summer time, just after midnight", time.Date(2026, 10, 24, 23, 30, 0, 0, time.UTC), "2026-10-25"},
		{"after the switch back to winter time", time.Date(2026, 10, 25, 22, 30, 0, 0, time.UTC), "2026-10-25"},
		{"winter time, just after midnight", time.Date(2026, 10, 25, 23, 30, 0, 0, time.UTC), "2026-10-26"},
		{"new year in winter time", time.Date(2026, 12, 31, 23, 0, 0, 0, time.UTC), "2027-01-01"},
		{"old year one second before", time.Date(2026, 12, 31, 22, 59, 59, 0, time.UTC), "2026-12-31"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := docissue.IssueDate(tt.now)
			if got.Format(docpayload.DateLayout) != tt.want {
				t.Errorf("IssueDate(%s) = %s, want %s", tt.now, got.Format(docpayload.DateLayout), tt.want)
			}
			if docissue.IssuedAt(tt.now).Year() != got.Year() {
				t.Errorf("IssuedAt(%s).Year() = %d, want the number year %d", tt.now, docissue.IssuedAt(tt.now).Year(), got.Year())
			}
		})
	}
}

// checker is a PayloadChecker that refuses every payload.
type checker struct{}

func (checker) CheckPayload(context.Context, docpayload.FrozenPayloadV1) (map[string]string, error) {
	return map[string]string{"lines[0].description": "word too wide for the cell"}, nil
}

func TestIssueMergesTheRendererCheck(t *testing.T) {
	t.Parallel()

	f := newFixture(t, storage.VATRegimeKleinunternehmer)
	f.issuer.Checker = checker{}
	draft := f.draft(t, storage.Document{})

	_, err := f.issuer.Issue(f.ctx, draft.ID, now)
	var invalid *docissue.ValidationError
	if !errors.As(err, &invalid) || invalid.Details["lines[0].description"] == "" {
		t.Fatalf("Issue() error = %v, want the renderer's detail", err)
	}
	if got := f.store.Counter(f.tenant, storage.DocumentTypeInvoice, 2026); got != 0 {
		t.Errorf("counter = %d, want 0", got)
	}
}
