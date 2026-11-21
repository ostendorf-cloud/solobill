package euer_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/category"
	"github.com/ostendorf-cloud/solobill/internal/docchain"
	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/euer"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// The ledgers below are hand computed. Every expected figure is worked
// out in the comment next to it, never by the code under test.

func id(n int) uuid.UUID { return uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012d", n)) }

func day(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}

	return d
}

func dayPtr(s string) *time.Time { d := day(s); return &d }

func ptr[T any](v T) *T { return &v }

// stamp is the recording time of the n-th row: payments are split in
// this order (paymentvat).
func stamp(n int) time.Time { return time.Date(2026, 1, 1, 0, 0, n, 0, time.UTC) }

func period(n int, from string, r storage.VATRegime) storage.VATRegimePeriod {
	return storage.VATRegimePeriod{ID: id(n), ValidFrom: day(from), Regime: r, CreatedAt: stamp(n)}
}

func pay(n, root int, received string, amount int64) storage.Payment {
	return storage.Payment{ID: id(n), DocumentID: id(root), ReceivedOn: day(received), AmountCents: amount, CreatedAt: stamp(n)}
}

func reversal(n, root, of int, received string, amount int64) storage.Payment {
	p := pay(n, root, received, amount)
	p.ReversesPaymentID = ptr(id(of))

	return p
}

// kuChain is an original invoice under the Kleinunternehmer regime: no
// VAT, no tax free bucket.
func kuChain(root int, issued string, gross int64, payments ...storage.Payment) euer.Chain {
	return euer.Chain{
		Members:  []docchain.Member{{ID: id(root), Seq: 0, IssuedOn: day(issued)}},
		Figures:  map[uuid.UUID]euer.Figures{id(root): {GrossCents: gross}},
		Payments: payments,
	}
}

func expense(n int, paid string, key category.Key, gross, net, vat int64) storage.Expense {
	return storage.Expense{
		ID: id(n), State: storage.BookingStateBooked, PaidOn: dayPtr(paid), Category: key,
		GrossCents: gross, NetCents: net, VATCents: vat, FileID: ptr(id(9000 + n)),
	}
}

func income(n int, received string, key category.Key, gross, net, vat int64) storage.OtherIncome {
	return storage.OtherIncome{
		ID: id(n), State: storage.BookingStateBooked, ReceivedOn: dayPtr(received), Category: key,
		GrossCents: gross, NetCents: net, VATCents: vat,
	}
}

func assign(n int, kind storage.EuerEntryKind, entry, year int) storage.EuerAssignment {
	return storage.EuerAssignment{ID: id(n), EntryKind: kind, EntryID: id(entry), TaxYear: int32(year), CreatedAt: stamp(n)}
}

func amounts(r euer.Report) map[category.Key]int64 {
	out := map[category.Key]int64{}
	for _, l := range r.Lines {
		out[l.Key] = l.AmountCents
	}

	return out
}

func line(t *testing.T, r euer.Report, key category.Key) euer.Line {
	t.Helper()

	for _, l := range r.Lines {
		if l.Key == key {
			return l
		}
	}
	t.Fatalf("no line %q in %v", key, amounts(r))

	return euer.Line{}
}

// sourceAmounts maps each source id of a line to its amount.
func sourceAmounts(l euer.Line) map[uuid.UUID]int64 {
	out := map[uuid.UUID]int64{}
	for _, s := range l.Sources {
		out[s.ID] += s.AmountCents
	}

	return out
}

func wantAmounts(t *testing.T, r euer.Report, want map[category.Key]int64) {
	t.Helper()

	if got := amounts(r); !maps.Equal(got, want) {
		t.Errorf("lines =\n%v\nwant\n%v", got, want)
	}
}

func compute(t *testing.T, in euer.Input) euer.Report {
	t.Helper()

	r, err := euer.Compute(in)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	return r
}

// kleinunternehmerLedger is the owner's own case: Kleinunternehmer since
// 2025, the year 2026.
func kleinunternehmerLedger() euer.Input {
	// Chain 100: 237.50 EUR, paid in two parts, plus 5.00 EUR recorded by
	// mistake and reversed on the same day.
	chainA := kuChain(100, "2026-03-01", 23750,
		pay(101, 100, "2026-03-10", 10000),
		pay(102, 100, "2026-04-02", 13750),
		pay(103, 100, "2026-05-05", 500),
		reversal(104, 100, 103, "2026-05-05", -500),
	)
	// Chain 200: paid around the turn into 2027. 200.00 EUR on 28
	// December are assigned to 2027, 300.00 EUR on 5 January 2027 to
	// 2026.
	chainB := kuChain(200, "2026-12-15", 50000,
		pay(201, 200, "2026-12-28", 20000),
		pay(202, 200, "2027-01-05", 30000),
	)
	// Chain 300: paid around the turn into 2026. 40.00 EUR on 30 December
	// 2025 are assigned to 2026, 70.00 EUR on 4 January 2026 to 2025.
	chainC := kuChain(300, "2025-12-01", 11000,
		pay(301, 300, "2025-12-30", 4000),
		pay(302, 300, "2026-01-04", 7000),
	)

	// An asset expense with its asset row, and one without.
	withAsset := expense(404, "2026-03-10", category.WorkEquipment, 119000, 119000, 0)
	withAsset.IsAsset = true
	withoutAsset := expense(405, "2026-08-01", category.WorkEquipment, 60000, 60000, 0)
	withoutAsset.IsAsset = true
	noReceipt := expense(402, "2026-06-01", category.Telecommunication, 5000, 5000, 0)
	noReceipt.FileID = nil
	draft := expense(403, "2026-07-01", category.WorkEquipment, 999, 999, 0)
	draft.State = storage.BookingStateDraft
	storno := expense(407, "2026-04-20", category.WorkEquipment, -2000, -2000, 0)
	storno.StornoOfID = ptr(id(408))
	storno.FileID = nil
	draftIncome := income(502, "2026-09-02", category.AssetDisposal, 777, 777, 0)
	draftIncome.State = storage.BookingStateDraft

	return euer.Input{
		Year:    2026,
		Periods: []storage.VATRegimePeriod{period(1, "2025-01-01", storage.VATRegimeKleinunternehmer)},
		Chains:  []euer.Chain{chainA, chainB, chainC},
		Expenses: []storage.Expense{
			expense(401, "2026-02-01", category.WorkEquipment, 11900, 11900, 0),
			noReceipt,
			draft,
			withAsset,
			withoutAsset,
			expense(406, "2026-12-23", category.FeesAndInsurance, 3000, 3000, 0),
			expense(408, "2026-04-01", category.WorkEquipment, 2000, 2000, 0),
			storno,
			// Paid in 2025: another year.
			expense(409, "2025-11-30", category.WorkEquipment, 4444, 4444, 0),
			// Paid on 8 January 2027, assigned to 2026.
			expense(410, "2027-01-08", category.Telecommunication, 1000, 1000, 0),
		},
		OtherIncome: []storage.OtherIncome{
			income(501, "2026-09-01", category.AssetDisposal, 15000, 15000, 0),
			draftIncome,
			// Received on 29 December 2026, assigned to 2027.
			income(503, "2026-12-29", category.AssetDisposal, 888, 888, 0),
		},
		Assets: []storage.Asset{
			// 1,190.00 EUR gross (no Vorsteuer deduction), 36 months from
			// March 2026: 10 months in 2026, 1190.00 * 10 / 36 = 330.5555...
			// rounded to 330.56.
			{
				ID: id(601), AcquiredOn: day("2026-03-10"), CostBasisCents: 119000, Method: storage.DepreciationLinear,
				Group: storage.AssetGroupOfficeEquipment, UsefulLifeMonths: ptr(int32(36)), ExpenseID: ptr(id(404)),
			},
			// GWG of 700.00 EUR: in full in 2026.
			{
				ID: id(602), AcquiredOn: day("2026-05-01"), CostBasisCents: 70000, Method: storage.DepreciationLowValue,
				Group: storage.AssetGroupOtherMovable,
			},
			// Sammelposten of 500.00 EUR formed in 2025: 100.00 EUR in 2026.
			{
				ID: id(603), AcquiredOn: day("2025-06-01"), CostBasisCents: 50000, Method: storage.DepreciationPool,
				Group: storage.AssetGroupOtherMovable,
			},
		},
		Assignments: []storage.EuerAssignment{
			assign(701, storage.EuerEntryPayment, 201, 2027),
			assign(702, storage.EuerEntryPayment, 202, 2026),
			assign(703, storage.EuerEntryPayment, 301, 2026),
			assign(704, storage.EuerEntryPayment, 302, 2025),
			assign(705, storage.EuerEntryExpense, 410, 2026),
			assign(706, storage.EuerEntryOtherIncome, 503, 2027),
		},
	}
}

func TestKleinunternehmerLedger(t *testing.T) {
	t.Parallel()

	r := compute(t, kleinunternehmerLedger())

	wantAmounts(t, r, map[category.Key]int64{
		// 100.00 + 137.50 + 5.00 - 5.00 (chain 100) + 300.00 (5 January
		// 2027, assigned to 2026) + 40.00 (30 December 2025, assigned to
		// 2026). Not: 200.00 (assigned to 2027), 70.00 (assigned to 2025).
		category.IncomeKleinunternehmer: 57750,
		// The sale, gross. Not: the draft, 8.88 assigned to 2027.
		category.AssetDisposal: 15000,
		// 119.00 + 20.00 - 20.00 (storno), gross. The asset expenses,
		// the draft and the 2025 expense do not count.
		category.WorkEquipment: 11900,
		// 50.00 + 10.00 paid on 8 January 2027, assigned to 2026.
		category.Telecommunication:   6000,
		category.FeesAndInsurance:    3000,
		category.DepreciationMovable: 33056,
		category.LowValueAssets:      70000,
		category.PoolDissolution:     10000,
	})

	z12 := sourceAmounts(line(t, r, category.IncomeKleinunternehmer))
	wantSources := map[uuid.UUID]int64{
		id(101): 10000, id(102): 13750, id(103): 500, id(104): -500, id(202): 30000, id(301): 4000,
	}
	if !maps.Equal(z12, wantSources) {
		t.Errorf("Zeile 12 sources = %v, want %v", z12, wantSources)
	}
	if l := line(t, r, category.IncomeKleinunternehmer); l.Zeile != 12 || l.Kennzahl != "111" || l.Verified {
		t.Errorf("Zeile 12 = %+v, want Zeile 12, Kz 111, not verified", l)
	}
	for _, l := range r.Lines {
		for _, s := range l.Sources {
			if s.ID == id(404) || s.ID == id(405) {
				t.Errorf("asset expense %s counted on %s", s.ID, l.Key)
			}
		}
	}
	if src := line(t, r, category.DepreciationMovable).Sources; len(src) != 1 || src[0].Kind != euer.SourceAsset || src[0].ID != id(601) {
		t.Errorf("Zeile 33 sources = %+v, want asset 601", src)
	}

	// No VAT lines for a Kleinunternehmer.
	for _, k := range []category.Key{category.IncomeTaxable, category.IncomeTaxFree, category.VATReceived, category.InputVAT} {
		if _, ok := amounts(r)[k]; ok {
			t.Errorf("Kleinunternehmer report has %s", k)
		}
	}

	if r.Mapping.TaxYear != 2026 || r.Mapping.Status != euer.StatusUnverified {
		t.Errorf("mapping = %d %s, want 2026 unverified", r.Mapping.TaxYear, r.Mapping.Status)
	}
	if want := []euer.Period{{ValidFrom: day("2025-01-01"), Regime: storage.VATRegimeKleinunternehmer}}; !slices.Equal(r.Regimes, want) {
		t.Errorf("regimes = %+v, want %+v", r.Regimes, want)
	}
}

func TestKleinunternehmerBoundaryAndWarnings(t *testing.T) {
	t.Parallel()

	r := compute(t, kleinunternehmerLedger())

	type entry struct {
		kind euer.SourceKind
		id   uuid.UUID
		date string
		year int
	}
	var got []entry
	for _, b := range r.Boundary {
		y := 0
		if b.AssignedTaxYear != nil {
			y = *b.AssignedTaxYear
		}
		got = append(got, entry{b.Kind, b.ID, b.Date.Format(time.DateOnly), y})
	}
	// Both windows of 2026, by date. The reversal 104 is no entry of its
	// own; 0 is "not assigned".
	want := []entry{
		{euer.SourcePayment, id(301), "2025-12-30", 2026},
		{euer.SourcePayment, id(302), "2026-01-04", 2025},
		{euer.SourceExpense, id(406), "2026-12-23", 0},
		{euer.SourcePayment, id(201), "2026-12-28", 2027},
		{euer.SourceOtherIncome, id(503), "2026-12-29", 2027},
		{euer.SourcePayment, id(202), "2027-01-05", 2026},
		{euer.SourceExpense, id(410), "2027-01-08", 2026},
	}
	if !slices.Equal(got, want) {
		t.Errorf("boundary =\n%v\nwant\n%v", got, want)
	}

	wantWarnings := []euer.Warning{
		{Code: euer.WarningAssetExpenseWithoutAsset, Kind: euer.SourceExpense, ID: id(405)},
		{Code: euer.WarningMissingReceipt, Kind: euer.SourceExpense, ID: id(402)},
		{Code: euer.WarningUnassignedBoundaryEntry, Kind: euer.SourceExpense, ID: id(406)},
	}
	if !slices.Equal(r.Warnings, wantWarnings) {
		t.Errorf("warnings =\n%+v\nwant\n%+v", r.Warnings, wantWarnings)
	}
}

// payload freezes the totals of a document under the regelbesteuerung.
func standardDoc(t *testing.T, n int, buckets ...docpayload.VATBucket) storage.Document {
	t.Helper()

	var net, vat int64
	for _, b := range buckets {
		net += b.NetCents
		vat += b.VATCents
	}
	data, err := json.Marshal(docpayload.FrozenPayloadV1{
		Version: docpayload.Version1,
		Totals: docpayload.Totals{
			NetTotalCents: net, NetAfterDocumentDiscountCents: net, VAT: buckets,
			VATTotalCents: vat, GrandTotalCents: net + vat,
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	return storage.Document{ID: id(n), State: storage.DocumentStateIssued, VATRegime: storage.VATRegimeStandard, FrozenPayload: data}
}

func figures(t *testing.T, d storage.Document) euer.Figures {
	t.Helper()

	f, err := euer.FiguresOf(d)
	if err != nil {
		t.Fatalf("FiguresOf: %v", err)
	}

	return f
}

func TestFiguresOf(t *testing.T) {
	t.Parallel()

	d := standardDoc(t, 1,
		docpayload.VATBucket{RatePermyriad: 1900, NetCents: 10000, VATCents: 1900},
		docpayload.VATBucket{RatePermyriad: 0, NetCents: 5000},
	)
	if got, want := figures(t, d), (euer.Figures{GrossCents: 16900, VATCents: 1900, TaxFreeNetCents: 5000}); got != want {
		t.Errorf("standard = %+v, want %+v", got, want)
	}

	// The same 0 % bucket under the Kleinunternehmer regime is no tax
	// free revenue: no VAT is charged for another reason.
	d.VATRegime = storage.VATRegimeKleinunternehmer
	if got, want := figures(t, d), (euer.Figures{GrossCents: 16900, VATCents: 1900}); got != want {
		t.Errorf("kleinunternehmer = %+v, want %+v", got, want)
	}

	d.State = storage.DocumentStateDraft
	if _, err := euer.FiguresOf(d); err == nil {
		t.Error("a draft has figures")
	}
}

// standardLedger is the Regelbesteuerung with a refund after a
// correction.
//
// Chain 100: the original 100 (issued 1 February) is 100.00 net at 19 %
// plus a 50.00 tax free line: gross 169.00, VAT 19.00, tax free 50.00.
// The correction 110 (issued 1 March) lowers the 19 % line to 80.00:
// gross 145.20, VAT 15.20, tax free 50.00.
//
// Recording order: 101, 102, 103, 104.
//   - 101 on 10 February, 169.00, ratio of 100: VAT 19.00 exactly, tax
//     free 50.00 exactly, Zeile 15 100.00.
//   - 102 on 5 March, the refund of -23.80, ratio of 110 (effective on 5
//     March): VAT exact -23.80 * 15.20 / 145.20 = -2.4914..., running
//     19.00 - 2.4914 = 16.5085 rounds to 16.51, so -2.49. Tax free exact
//     -23.80 * 50.00 / 145.20 = -8.1955..., running 41.8044 rounds to
//     41.80, so -8.20. Net -23.80 + 2.49 = -21.31, Zeile 15 -21.31 + 8.20
//     = -13.11.
//   - 103 on 20 February, 1.00 recorded by mistake after the correction,
//     ratio of 100 (effective on 20 February): VAT exact 1.00 * 19 / 169 =
//     0.11242..., running 16.6209 rounds to 16.62, so 0.11. Tax free exact
//     1.00 * 50 / 169 = 0.29585..., running 42.1002 rounds to 42.10, so
//     0.30. Net 0.89, Zeile 15 0.59.
//   - 104 reverses 103 on its own day with its exact shares: running VAT
//     back to 16.5085, 16.51, so -0.11, tax free back to 41.80, so -0.30.
//     Net -0.89, Zeile 15 -0.59.
//
// Zeile 15: 100.00 - 13.11 + 0.59 - 0.59 = 86.89. Zeile 16: 50.00 - 8.20
// = 41.80. Zeile 17 from payments: 19.00 - 2.49 = 16.51. Together 145.20,
// the money kept. The earlier payment keeps the ratio of its own day, so
// the lines differ from the correction's own 80.00, 50.00 and 15.20: the
// difference the correction causes shows only through the refund.
func standardLedger(t *testing.T) euer.Input {
	t.Helper()

	original := standardDoc(t, 100,
		docpayload.VATBucket{RatePermyriad: 1900, NetCents: 10000, VATCents: 1900},
		docpayload.VATBucket{RatePermyriad: 0, NetCents: 5000},
	)
	correction := standardDoc(t, 110,
		docpayload.VATBucket{RatePermyriad: 1900, NetCents: 8000, VATCents: 1520},
		docpayload.VATBucket{RatePermyriad: 0, NetCents: 5000},
	)
	chain := euer.Chain{
		Members: []docchain.Member{
			{ID: id(100), Seq: 0, IssuedOn: day("2026-02-01")},
			{ID: id(110), Seq: 1, IssuedOn: day("2026-03-01")},
		},
		Figures: map[uuid.UUID]euer.Figures{id(100): figures(t, original), id(110): figures(t, correction)},
		Payments: []storage.Payment{
			pay(101, 100, "2026-02-10", 16900),
			pay(102, 100, "2026-03-05", -2380),
			pay(103, 100, "2026-02-20", 100),
			reversal(104, 100, 103, "2026-02-20", -100),
		},
	}

	return euer.Input{
		Year:    2026,
		Periods: []storage.VATRegimePeriod{period(1, "2026-01-01", storage.VATRegimeStandard)},
		Chains:  []euer.Chain{chain},
		Expenses: []storage.Expense{
			// 119.00 gross: 100.00 net on Zeile 50, 19.00 on Zeile 57.
			expense(401, "2026-04-01", category.ITRunningCosts, 11900, 10000, 1900),
			// The VAT paid to the tax office, booked by the owner.
			expense(402, "2026-05-01", category.VATPaid, 1651, 1651, 0),
		},
		OtherIncome: []storage.OtherIncome{
			income(501, "2026-06-01", category.VATRefunded, 500, 500, 0),
			// A sale for 119.00: 100.00 on Zeile 19, 19.00 on Zeile 17.
			income(502, "2026-07-01", category.AssetDisposal, 11900, 10000, 1900),
		},
		Assets: []storage.Asset{
			// 100.00 net over 36 months from January: 12 / 36 = 33.333...
			// rounded to 33.33.
			{
				ID: id(601), AcquiredOn: day("2026-01-15"), CostBasisCents: 10000, Method: storage.DepreciationLinear,
				Group: storage.AssetGroupOfficeEquipment, UsefulLifeMonths: ptr(int32(36)),
			},
		},
	}
}

func TestStandardLedgerWithARefundAfterACorrection(t *testing.T) {
	t.Parallel()

	r := compute(t, standardLedger(t))

	wantAmounts(t, r, map[category.Key]int64{
		category.IncomeTaxable: 8689,
		category.IncomeTaxFree: 4180,
		// 16.51 from the payments plus 19.00 from the sale.
		category.VATReceived:         3551,
		category.VATRefunded:         500,
		category.AssetDisposal:       10000,
		category.ITRunningCosts:      10000,
		category.InputVAT:            1900,
		category.VATPaid:             1651,
		category.DepreciationMovable: 3333,
	})

	wantSources := map[category.Key]map[uuid.UUID]int64{
		category.IncomeTaxable: {id(101): 10000, id(102): -1311, id(103): 59, id(104): -59},
		category.IncomeTaxFree: {id(101): 5000, id(102): -820, id(103): 30, id(104): -30},
		category.VATReceived:   {id(101): 1900, id(102): -249, id(103): 11, id(104): -11, id(502): 1900},
	}
	for key, want := range wantSources {
		if got := sourceAmounts(line(t, r, key)); !maps.Equal(got, want) {
			t.Errorf("%s sources = %v, want %v", key, got, want)
		}
	}

	// The payments' parts add up to the money kept: 169.00 - 23.80.
	var money int64
	for _, k := range []category.Key{category.IncomeTaxable, category.IncomeTaxFree, category.VATReceived} {
		for _, s := range line(t, r, k).Sources {
			if s.Kind == euer.SourcePayment {
				money += s.AmountCents
			}
		}
	}
	if money != 14520 {
		t.Errorf("the payments' parts sum to %d, want 14520", money)
	}
}

// TestRegimeSwitchInsideTheYear: Kleinunternehmer until 30 June,
// Regelbesteuerung from 1 July. Each entry takes the regime of its own
// date, never the regime of its document.
func TestRegimeSwitchInsideTheYear(t *testing.T) {
	t.Parallel()

	std := standardDoc(t, 200, docpayload.VATBucket{RatePermyriad: 1900, NetCents: 10000, VATCents: 1900})
	in := euer.Input{
		Year: 2026,
		Periods: []storage.VATRegimePeriod{
			period(1, "2026-01-01", storage.VATRegimeKleinunternehmer),
			period(2, "2026-07-01", storage.VATRegimeStandard),
		},
		Chains: []euer.Chain{
			// Issued and paid as a Kleinunternehmer.
			kuChain(100, "2026-03-01", 50000, pay(101, 100, "2026-03-15", 50000)),
			// Issued in June, paid in July after the switch: 119.00 split
			// into 100.00 and 19.00 by the payment's date.
			{
				Members:  []docchain.Member{{ID: id(200), IssuedOn: day("2026-07-02")}},
				Figures:  map[uuid.UUID]euer.Figures{id(200): figures(t, std)},
				Payments: []storage.Payment{pay(201, 200, "2026-07-10", 11900)},
			},
			// A Kleinunternehmer invoice paid after the switch: no VAT in
			// it, all of it on Zeile 15, none on Zeile 16.
			kuChain(300, "2026-06-20", 7000, pay(301, 300, "2026-07-03", 7000)),
		},
		Expenses: []storage.Expense{
			// Before the switch: gross, with the VAT inside.
			expense(401, "2026-06-30", category.WorkEquipment, 11900, 11900, 0),
			// After it: net, and the VAT on Zeile 57.
			expense(402, "2026-07-01", category.WorkEquipment, 11900, 10000, 1900),
		},
	}
	r := compute(t, in)

	wantAmounts(t, r, map[category.Key]int64{
		category.IncomeKleinunternehmer: 50000,
		category.IncomeTaxable:          17000,
		category.VATReceived:            1900,
		category.WorkEquipment:          21900,
		category.InputVAT:               1900,
	})
	want := []euer.Period{
		{ValidFrom: day("2026-01-01"), Regime: storage.VATRegimeKleinunternehmer},
		{ValidFrom: day("2026-07-01"), Regime: storage.VATRegimeStandard},
	}
	if !slices.Equal(r.Regimes, want) {
		t.Errorf("regimes = %+v, want %+v", r.Regimes, want)
	}
}

// TestAReversalFollowsThePaymentItReverses: a payment of 30 December
// assigned to the next year takes its reversal with it, and the reversal
// is no boundary entry of its own.
func TestAReversalFollowsThePaymentItReverses(t *testing.T) {
	t.Parallel()

	in := euer.Input{
		Periods: []storage.VATRegimePeriod{period(1, "2025-01-01", storage.VATRegimeKleinunternehmer)},
		Chains: []euer.Chain{kuChain(100, "2026-12-01", 9000,
			pay(101, 100, "2026-12-30", 9000),
			reversal(102, 100, 101, "2026-12-30", -9000),
			pay(103, 100, "2026-12-30", 9000),
		)},
		Assignments: []storage.EuerAssignment{assign(701, storage.EuerEntryPayment, 101, 2027)},
	}

	in.Year = 2026
	r := compute(t, in)
	if got := sourceAmounts(line(t, r, category.IncomeKleinunternehmer)); !maps.Equal(got, map[uuid.UUID]int64{id(103): 9000}) {
		t.Errorf("2026 Zeile 12 sources = %v, want only 103", got)
	}
	for _, b := range r.Boundary {
		if b.ID == id(102) {
			t.Error("the reversal is listed as a boundary entry")
		}
	}
	if len(r.Boundary) != 2 {
		t.Errorf("boundary = %+v, want 101 and 103", r.Boundary)
	}

	// 2027 has no mapping yet, so the attribution is checked through the
	// error-free 2026 report above and the year rule below.
	in.Year = 2027
	if _, err := euer.Compute(in); !errors.Is(err, euer.ErrNoMapping) {
		t.Errorf("2027: err = %v, want ErrNoMapping", err)
	}
}

func TestComputeRefusals(t *testing.T) {
	t.Parallel()

	ku := []storage.VATRegimePeriod{period(1, "2025-01-01", storage.VATRegimeKleinunternehmer)}
	tests := []struct {
		name     string
		in       euer.Input
		want     error
		wantDate string
	}{
		{"imported archive year 2025", euer.Input{Year: 2025, Periods: ku}, euer.ErrArchiveYear, ""},
		{"imported archive year 2023", euer.Input{Year: 2023, Periods: ku}, euer.ErrArchiveYear, ""},
		{"a year without a mapping", euer.Input{Year: 2027, Periods: ku}, euer.ErrNoMapping, ""},
		{"no period at all", euer.Input{Year: 2026}, euer.ErrRegimeMissing, "2026-12-31"},
		{"the only period starts after the year", euer.Input{Year: 2026, Periods: []storage.VATRegimePeriod{
			period(1, "2027-01-01", storage.VATRegimeKleinunternehmer),
		}}, euer.ErrRegimeMissing, "2026-12-31"},
		{"a payment before the first period", euer.Input{
			Year:    2026,
			Periods: []storage.VATRegimePeriod{period(1, "2026-07-01", storage.VATRegimeKleinunternehmer)},
			Chains:  []euer.Chain{kuChain(100, "2026-03-01", 100, pay(101, 100, "2026-03-02", 100))},
		}, euer.ErrRegimeMissing, "2026-03-02"},
		{"an expense before the first period", euer.Input{
			Year:     2026,
			Periods:  []storage.VATRegimePeriod{period(1, "2026-07-01", storage.VATRegimeKleinunternehmer)},
			Expenses: []storage.Expense{expense(401, "2026-02-02", category.WorkEquipment, 100, 100, 0)},
		}, euer.ErrRegimeMissing, "2026-02-02"},
		{"other income before the first period", euer.Input{
			Year:        2026,
			Periods:     []storage.VATRegimePeriod{period(1, "2026-07-01", storage.VATRegimeKleinunternehmer)},
			OtherIncome: []storage.OtherIncome{income(501, "2026-02-03", category.AssetDisposal, 100, 100, 0)},
		}, euer.ErrRegimeMissing, "2026-02-03"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := euer.Compute(tt.in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if tt.wantDate != "" {
				var rm *euer.RegimeMissingError
				if !errors.As(err, &rm) || rm.Date.Format(time.DateOnly) != tt.wantDate {
					t.Errorf("err = %v, want the date %s", err, tt.wantDate)
				}
			}
		})
	}
}

// TestASupersededPeriodNoLongerCounts: a Kleinunternehmer period corrected
// by a standard one of the same day leaves only the standard regime.
func TestASupersededPeriodNoLongerCounts(t *testing.T) {
	t.Parallel()

	wrong := period(1, "2026-01-01", storage.VATRegimeKleinunternehmer)
	fixed := period(2, "2026-01-01", storage.VATRegimeStandard)
	fixed.SupersedesID = &wrong.ID
	r := compute(t, euer.Input{
		Year:     2026,
		Periods:  []storage.VATRegimePeriod{wrong, fixed},
		Expenses: []storage.Expense{expense(401, "2026-02-02", category.WorkEquipment, 11900, 10000, 1900)},
	})
	wantAmounts(t, r, map[category.Key]int64{category.WorkEquipment: 10000, category.InputVAT: 1900})
}

func TestCheckAssignment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		date string
		year int
		want error
	}{
		{"2026-12-21", 2026, euer.ErrOutsideWindow},
		{"2026-12-22", 2026, nil},
		{"2026-12-22", 2027, nil},
		{"2026-12-22", 2025, euer.ErrWrongTaxYear},
		{"2026-12-31", 2027, nil},
		{"2026-12-31", 2028, euer.ErrWrongTaxYear},
		{"2027-01-01", 2026, nil},
		{"2027-01-10", 2027, nil},
		{"2027-01-10", 2026, nil},
		{"2027-01-10", 2028, euer.ErrWrongTaxYear},
		{"2027-01-11", 2026, euer.ErrOutsideWindow},
		{"2026-06-15", 2026, euer.ErrOutsideWindow},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s to %d", tt.date, tt.year), func(t *testing.T) {
			t.Parallel()

			if err := euer.CheckAssignment(day(tt.date), tt.year); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestLoadRange(t *testing.T) {
	t.Parallel()

	from, to := euer.LoadRange(2026)
	if from != day("2025-12-22") || to != day("2027-01-10") {
		t.Errorf("LoadRange(2026) = %s to %s", from.Format(time.DateOnly), to.Format(time.DateOnly))
	}
}
