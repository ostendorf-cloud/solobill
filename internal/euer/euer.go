// Package euer computes the EÜR report of one tax year from the owner's
// ledger entries: payments of invoice chains, booked expenses, booked
// other income and the asset register. Every figure comes out labelled
// with its line key, the Zeile and Kennzahl of the year's mapping, and the
// ids of the entries behind it, so each amount traces back to its rows.
//
// Pure: no database, no clock, no I/O. The caller (internal/httpapi)
// loads the entries and hands them in. Every amount is an integer cent;
// shares are computed in exact rational arithmetic (math/big), never a
// float.
//
// The rules (docs/concepts/solobill.md, "Payments", "EÜR mapping as data
// per tax year", "Depreciation scope"):
//
//   - Zufluss counts by payments.received_on, refunds included. Abfluss
//     counts by expenses.paid_on, other income by its received_on.
//     Document totals and invoice dates are never used.
//   - An entry counts in the year of its date, unless the owner's
//     effective assignment names another year. A reversal of a payment
//     follows the payment it reverses.
//   - The VAT regime of an entry is the regime of the effective period on
//     the entry's own date, never the regime of the document. No period
//     on a counted entry's date, or none for the whole year, is
//     ErrRegimeMissing.
//   - Kleinunternehmer: every amount gross on its line, no VAT lines.
//     Regelbesteuerung: net amounts on their lines, the VAT received on
//     Zeile 17 and the input VAT of expenses on Zeile 57. Zeilen 18 and 58
//     are amounts the owner books under those keys. NOT VERIFIED: this
//     reading of the Anlage EÜR is the plan's, not a Steuerberater's.
//   - The VAT share of a payment comes from internal/paymentvat: the
//     chain's effective document on the payment's received_on
//     (docchain.Chain.EffectiveOn) and that document's buckets, rounded
//     cumulatively in recording order over the chain's whole history. A
//     later correction never recomputes an earlier payment. NOT VERIFIED:
//     the legal side (sections 13 and 17 UStG), see internal/paymentvat.
//   - Under the Regelbesteuerung the net share of a payment from the 0 %
//     buckets of a document issued under the Regelbesteuerung goes to
//     Zeile 16, the rest to Zeile 15. The 0 % share is rounded
//     cumulatively like the VAT share. NOT VERIFIED: that every 0 % line
//     belongs on Zeile 16.
//   - An expense with is_asset set never counts. Its depreciation does:
//     the year's figure of afa.Schedule for every asset of the register.
//     Every booked asset expense without an asset row is a warning.
//   - A reversal of a payment takes the year and the regime of the
//     payment it reverses, so it leaves the line that payment went to.
//   - A line keeps its amount as it goes into the field. The operator the
//     form prints next to Zeile 72 ("-") and Zeile 73 ("+") comes from
//     the mapping. Neither raises a missing receipt warning: both are
//     computed figures (docs/concepts/euer-keys/80.md).
//
// Every figure is a computation over the owner's own entries, not tax
// advice.
package euer

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/afa"
	"github.com/ostendorf-cloud/solobill/internal/category"
	"github.com/ostendorf-cloud/solobill/internal/docchain"
	"github.com/ostendorf-cloud/solobill/internal/docissue"
	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/paymentvat"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// FirstReportYear is the first tax year solobill reports. The imported
// years 2023 to 2025 are archive only (docs/concepts/solobill.md,
// "Imported documents and original numbers"), and solobill is not
// correct for a year before 2026 (Non goals).
const FirstReportYear = 2026

// SourceKind names the table a source row lives in.
type SourceKind string

const (
	// SourcePayment is a row of payments.
	SourcePayment SourceKind = "payment"
	// SourceExpense is a row of expenses.
	SourceExpense SourceKind = "expense"
	// SourceOtherIncome is a row of other_income.
	SourceOtherIncome SourceKind = "other_income"
	// SourceAsset is a row of assets: its depreciation in the year.
	SourceAsset SourceKind = "asset"
)

// Warning codes of docs/api.md, "EÜR report".
const (
	WarningMissingReceipt           = "missing_receipt"
	WarningAssetExpenseWithoutAsset = "asset_expense_without_asset"
	WarningUnassignedBoundaryEntry  = "unassigned_boundary_entry"
)

var (
	// ErrArchiveYear refuses a year before FirstReportYear.
	ErrArchiveYear = errors.New("euer: no report before tax year 2026: the years 2023 to 2025 are imported archive years")
	// ErrNoMapping refuses a year without an embedded mapping.
	ErrNoMapping = errors.New("euer: no line mapping for the tax year")
	// ErrRegimeMissing is returned when no VAT regime period covers the
	// year or the date of a counted entry. The API answers it as 400
	// vat_regime_missing.
	ErrRegimeMissing = errors.New("euer: no VAT regime period covers the date")
)

// RegimeMissingError names the date without a regime.
type RegimeMissingError struct {
	Date time.Time
}

func (e *RegimeMissingError) Error() string {
	return fmt.Sprintf("euer: no VAT regime period covers %s", e.Date.Format(time.DateOnly))
}

// Unwrap makes errors.Is(err, ErrRegimeMissing) hold.
func (e *RegimeMissingError) Unwrap() error { return ErrRegimeMissing }

// Figures is what the report needs of one issued document of a chain,
// from its frozen payload.
type Figures struct {
	GrossCents int64
	VATCents   int64
	// TaxFreeNetCents is the net of the 0 % buckets of a document issued
	// under the Regelbesteuerung, 0 under the Kleinunternehmer regime,
	// whose lines carry no VAT for another reason.
	TaxFreeNetCents int64
}

// FiguresOf reads the figures of an issued document.
func FiguresOf(d storage.Document) (Figures, error) {
	f, err := paymentvat.FiguresOf(d)
	if err != nil {
		return Figures{}, fmt.Errorf("euer: %w", err)
	}
	out := Figures{GrossCents: f.GrossCents, VATCents: f.VATCents}
	if d.VATRegime != storage.VATRegimeStandard {
		return out, nil
	}
	p, err := docpayload.Unmarshal(d.FrozenPayload)
	if err != nil {
		return Figures{}, fmt.Errorf("euer: document %s: %w", d.ID, err)
	}
	for _, b := range p.Totals.VAT {
		if b.RatePermyriad == 0 {
			out.TaxFreeNetCents += b.NetCents
		}
	}

	return out, nil
}

// Chain is one invoice chain: its issued documents and its whole payment
// history, never a slice of one year (see paymentvat.Split).
type Chain struct {
	Members  []docchain.Member
	Figures  map[uuid.UUID]Figures
	Payments []storage.Payment
}

// ChainOf builds a Chain from the issued documents of one chain, as
// storage.DocumentRepository.Chain returns them, and the chain's whole
// payment history. A member's issue date is its German calendar date
// (docissue.IssueDate), the day docchain.Chain.EffectiveOn compares.
func ChainOf(docs []storage.Document, payments []storage.Payment) (Chain, error) {
	c := Chain{Figures: map[uuid.UUID]Figures{}, Payments: payments}
	for _, d := range docs {
		if d.State != storage.DocumentStateIssued {
			continue
		}
		m := docchain.Member{ID: d.ID}
		if d.IssuedAt != nil {
			m.IssuedOn = docissue.IssueDate(*d.IssuedAt)
		}
		switch d.Type {
		case storage.DocumentTypeInvoice:
		case storage.DocumentTypeCorrection:
			if d.CorrectionSeq == nil {
				return Chain{}, fmt.Errorf("euer: correction %s has no sequence", d.ID)
			}
			m.Seq = int(*d.CorrectionSeq)
		default:
			continue
		}
		f, err := FiguresOf(d)
		if err != nil {
			return Chain{}, err
		}
		c.Members = append(c.Members, m)
		c.Figures[d.ID] = f
	}
	if _, err := docchain.New(c.Members); err != nil {
		return Chain{}, fmt.Errorf("euer: %w", err)
	}

	return c, nil
}

// Input is everything one report reads. Entries outside the year are
// welcome: the report picks what counts in Year itself. The caller loads
// at least every entry of LoadRange(Year) and every chain those payments
// belong to, in full.
type Input struct {
	Year int
	// Periods is the VAT regime history, superseded rows included.
	Periods     []storage.VATRegimePeriod
	Chains      []Chain
	Expenses    []storage.Expense
	OtherIncome []storage.OtherIncome
	Assets      []storage.Asset
	// Assignments are the effective assignments (ListEffective).
	Assignments []storage.EuerAssignment
}

// Source is one entry behind a line, with the part of its amount the
// line holds.
type Source struct {
	Kind        SourceKind
	ID          uuid.UUID
	Date        time.Time
	AmountCents int64
}

// Line is one figure of the report.
type Line struct {
	Key      category.Key
	Zeile    int
	Kennzahl string
	// Operator is the sign the form prints next to the Kennzahl, "-",
	// "+" or empty (MappingLine.Operator). AmountCents stays as it goes
	// into the field.
	Operator    string
	Verified    bool
	LabelDE     string
	LabelEN     string
	AmountCents int64
	Sources     []Source
}

// BoundaryEntry is an entry of one of the two windows of the year, for
// the owner to assign.
type BoundaryEntry struct {
	Kind        SourceKind
	ID          uuid.UUID
	Date        time.Time
	AmountCents int64
	// AssignedTaxYear is the effective assignment, nil when the entry
	// counts in the year of its date.
	AssignedTaxYear *int
}

// Warning is a finding the owner looks at. It never changes a figure.
type Warning struct {
	Code string
	Kind SourceKind
	ID   uuid.UUID
	// RelatedIDs are further entries of the same kind the finding
	// covers: the stornos of an original without a receipt, or the
	// original of such a storno counted in another year.
	RelatedIDs []uuid.UUID
}

// Period is one effective VAT regime period that touches the year.
type Period struct {
	ValidFrom time.Time
	Regime    storage.VATRegime
}

// Report is the EÜR of one tax year.
type Report struct {
	Year     int
	Mapping  Mapping
	Regimes  []Period
	Lines    []Line
	Boundary []BoundaryEntry
	Warnings []Warning
}

// entryKey identifies an entry an assignment points to.
type entryKey struct {
	kind storage.EuerEntryKind
	id   uuid.UUID
}

// builder collects the sources of each line.
type builder struct {
	sources map[category.Key][]Source
}

func (b *builder) add(key category.Key, s Source) {
	if b.sources == nil {
		b.sources = map[category.Key][]Source{}
	}
	b.sources[key] = append(b.sources[key], s)
}

// Compute builds the report of in.Year.
func Compute(in Input) (Report, error) {
	if in.Year < FirstReportYear {
		return Report{}, ErrArchiveYear
	}
	mapping, ok := MappingFor(in.Year)
	if !ok {
		return Report{}, ErrNoMapping
	}

	periods := effectivePeriods(in.Periods)
	yearEnd := date(in.Year, time.December, 31)
	if _, ok := regimeOn(periods, yearEnd); !ok {
		return Report{}, &RegimeMissingError{Date: yearEnd}
	}

	assigned := assignmentsByEntry(in.Assignments)
	yearOf := func(kind storage.EuerEntryKind, id uuid.UUID, d time.Time) int {
		if a, ok := assigned[entryKey{kind, id}]; ok {
			return int(a.TaxYear)
		}

		return d.Year()
	}
	regime := func(d time.Time) (storage.VATRegime, error) {
		r, ok := regimeOn(periods, d)
		if !ok {
			return "", &RegimeMissingError{Date: d}
		}

		return r, nil
	}

	var b builder
	var boundary []BoundaryEntry
	var warnings []Warning
	assignedYear := func(kind storage.EuerEntryKind, id uuid.UUID) *int {
		if a, ok := assigned[entryKey{kind, id}]; ok {
			y := int(a.TaxYear)

			return &y
		}

		return nil
	}
	addBoundary := func(kind SourceKind, ek storage.EuerEntryKind, id uuid.UUID, d time.Time, amount int64) {
		if !inWindowsOf(in.Year, d) {
			return
		}
		y := assignedYear(ek, id)
		boundary = append(boundary, BoundaryEntry{Kind: kind, ID: id, Date: d, AmountCents: amount, AssignedTaxYear: y})
		if y == nil {
			warnings = append(warnings, Warning{Code: WarningUnassignedBoundaryEntry, Kind: kind, ID: id})
		}
	}

	if err := addPayments(in, &b, yearOf, regime, addBoundary); err != nil {
		return Report{}, err
	}
	expenseWarnings, err := addExpenses(in, &b, yearOf, regime, addBoundary)
	if err != nil {
		return Report{}, err
	}
	warnings = append(warnings, expenseWarnings...)
	if err := addOtherIncome(in, &b, yearOf, regime, addBoundary); err != nil {
		return Report{}, err
	}
	if err := addAssets(in, &b); err != nil {
		return Report{}, err
	}

	lines, err := buildLines(mapping, b)
	if err != nil {
		return Report{}, err
	}

	sort.SliceStable(boundary, func(i, j int) bool {
		if !boundary[i].Date.Equal(boundary[j].Date) {
			return boundary[i].Date.Before(boundary[j].Date)
		}

		return boundary[i].ID.String() < boundary[j].ID.String()
	})
	sort.SliceStable(warnings, func(i, j int) bool {
		if warnings[i].Code != warnings[j].Code {
			return warnings[i].Code < warnings[j].Code
		}

		return warnings[i].ID.String() < warnings[j].ID.String()
	})

	return Report{
		Year:     in.Year,
		Mapping:  mapping,
		Regimes:  regimesOfYear(periods, in.Year),
		Lines:    lines,
		Boundary: boundary,
		Warnings: warnings,
	}, nil
}

type (
	yearFunc     func(kind storage.EuerEntryKind, id uuid.UUID, d time.Time) int
	regimeFunc   func(d time.Time) (storage.VATRegime, error)
	boundaryFunc func(kind SourceKind, ek storage.EuerEntryKind, id uuid.UUID, d time.Time, amount int64)
)

// addPayments splits every chain's payments and adds those that count in
// the year.
func addPayments(in Input, b *builder, yearOf yearFunc, regime regimeFunc, boundary boundaryFunc) error {
	for _, c := range in.Chains {
		vatFigures := make(map[uuid.UUID]paymentvat.Figures, len(c.Figures))
		for id, f := range c.Figures {
			vatFigures[id] = paymentvat.Figures{GrossCents: f.GrossCents, VATCents: f.VATCents}
		}
		shares, err := paymentvat.Split(c.Members, vatFigures, c.Payments)
		if err != nil {
			return fmt.Errorf("euer: %w", err)
		}
		taxFree := taxFreeShares(c, shares)

		byID := make(map[uuid.UUID]storage.Payment, len(c.Payments))
		for _, p := range c.Payments {
			byID[p.ID] = p
		}
		for i, s := range shares {
			p := byID[s.PaymentID]
			d := dateOnly(p.ReceivedOn)

			// A reversal undoes a recording mistake: it follows the year
			// and the regime of the payment it reverses, so it leaves the
			// line that payment went to, even across a regime switch.
			year, regimeDay := 0, d
			if p.ReversesPaymentID != nil {
				orig := byID[*p.ReversesPaymentID]
				regimeDay = dateOnly(orig.ReceivedOn)
				year = yearOf(storage.EuerEntryPayment, orig.ID, regimeDay)
			} else {
				year = yearOf(storage.EuerEntryPayment, p.ID, d)
				boundary(SourcePayment, storage.EuerEntryPayment, p.ID, d, p.AmountCents)
			}
			if year != in.Year {
				continue
			}

			r, err := regime(regimeDay)
			if err != nil {
				return err
			}
			src := func(amount int64) Source {
				return Source{Kind: SourcePayment, ID: p.ID, Date: d, AmountCents: amount}
			}
			if r == storage.VATRegimeKleinunternehmer {
				b.add(category.IncomeKleinunternehmer, src(s.GrossCents))

				continue
			}
			free := taxFree[i]
			if taxable := s.NetCents - free; taxable != 0 || free == 0 {
				b.add(category.IncomeTaxable, src(taxable))
			}
			if free != 0 {
				b.add(category.IncomeTaxFree, src(free))
			}
			if s.VATCents != 0 {
				b.add(category.VATReceived, src(s.VATCents))
			}
		}
	}

	return nil
}

// taxFreeShares is the net share of each payment from the 0 % buckets of
// its ratio document, in the order of shares, rounded cumulatively half
// away from zero like the VAT share: no cent is lost or invented, and a
// reversal removes the exact share of the payment it reverses.
func taxFreeShares(c Chain, shares []paymentvat.Share) []int64 {
	byID := make(map[uuid.UUID]storage.Payment, len(c.Payments))
	for _, p := range c.Payments {
		byID[p.ID] = p
	}
	exactOf := map[uuid.UUID]*big.Rat{}
	sum := new(big.Rat)
	var handedOut int64
	out := make([]int64, len(shares))
	for i, s := range shares {
		p := byID[s.PaymentID]
		var exact *big.Rat
		if p.ReversesPaymentID != nil {
			exact = new(big.Rat).Neg(exactOf[*p.ReversesPaymentID])
		} else {
			f := c.Figures[s.RatioDocumentID]
			exact = new(big.Rat)
			if f.GrossCents != 0 && f.TaxFreeNetCents != 0 {
				exact.SetFrac(big.NewInt(f.TaxFreeNetCents), big.NewInt(f.GrossCents))
				exact.Mul(exact, new(big.Rat).SetInt64(p.AmountCents))
			}
		}
		exactOf[p.ID] = exact
		sum.Add(sum, exact)
		v := roundHalfAwayFromZero(sum) - handedOut
		handedOut += v
		out[i] = v
	}

	return out
}

// addExpenses adds every booked expense that counts in the year and
// returns its warnings.
func addExpenses(in Input, b *builder, yearOf yearFunc, regime regimeFunc, boundary boundaryFunc) ([]Warning, error) {
	hasAsset := map[uuid.UUID]bool{}
	for _, a := range in.Assets {
		if a.ExpenseID != nil {
			hasAsset[*a.ExpenseID] = true
		}
	}

	var warnings []Warning
	var missing []storage.Expense
	for _, e := range in.Expenses {
		if e.State != storage.BookingStateBooked || e.PaidOn == nil {
			continue
		}
		d := dateOnly(*e.PaidOn)
		if !e.IsAsset {
			boundary(SourceExpense, storage.EuerEntryExpense, e.ID, d, e.GrossCents)
		}
		if yearOf(storage.EuerEntryExpense, e.ID, d) != in.Year {
			continue
		}
		isStorno := e.StornoOfID != nil
		if e.FileID == nil && (e.IsAsset || !noReceiptKeys[e.Category]) {
			missing = append(missing, e)
		}
		if e.IsAsset {
			// The register counts it, through its depreciation.
			if !isStorno && !hasAsset[e.ID] {
				warnings = append(warnings, Warning{Code: WarningAssetExpenseWithoutAsset, Kind: SourceExpense, ID: e.ID})
			}

			continue
		}

		r, err := regime(d)
		if err != nil {
			return nil, err
		}
		src := func(amount int64) Source {
			return Source{Kind: SourceExpense, ID: e.ID, Date: d, AmountCents: amount}
		}
		if r == storage.VATRegimeKleinunternehmer {
			b.add(e.Category, src(e.GrossCents))

			continue
		}
		b.add(e.Category, src(e.NetCents))
		if e.VATCents != 0 {
			b.add(category.InputVAT, src(e.VATCents))
		}
	}

	return append(warnings, missingReceiptWarnings(missing)...), nil
}

// noReceiptKeys are the expense keys whose amount is a computed figure,
// not a payment, so it has no receipt: Zeile 72 (the commuting costs
// the form subtracts) and Zeile 73 (the Entfernungspauschale it adds).
// See docs/concepts/euer-keys/80.md.
var noReceiptKeys = map[category.Key]bool{
	category.CarCommuteCosts:     true,
	category.CarCommuteAllowance: true,
}

// missingReceiptWarnings bundles the expenses of the year without a
// receipt. A storno copies the file of its original, so an original and
// its stornos lack the same receipt: one warning on the original names
// its stornos in RelatedIDs. A storno whose original counts in another
// year gets its own warning naming the original.
func missingReceiptWarnings(missing []storage.Expense) []Warning {
	originals := map[uuid.UUID]int{}
	var out []Warning
	for _, e := range missing {
		if e.StornoOfID == nil {
			originals[e.ID] = len(out)
			out = append(out, Warning{Code: WarningMissingReceipt, Kind: SourceExpense, ID: e.ID})
		}
	}
	for _, e := range missing {
		if e.StornoOfID == nil {
			continue
		}
		if i, ok := originals[*e.StornoOfID]; ok {
			out[i].RelatedIDs = append(out[i].RelatedIDs, e.ID)

			continue
		}
		out = append(out, Warning{Code: WarningMissingReceipt, Kind: SourceExpense, ID: e.ID, RelatedIDs: []uuid.UUID{*e.StornoOfID}})
	}
	for i := range out {
		sort.Slice(out[i].RelatedIDs, func(a, b int) bool { return out[i].RelatedIDs[a].String() < out[i].RelatedIDs[b].String() })
	}

	return out
}

// addOtherIncome adds every booked other income entry that counts in the
// year.
func addOtherIncome(in Input, b *builder, yearOf yearFunc, regime regimeFunc, boundary boundaryFunc) error {
	for _, o := range in.OtherIncome {
		if o.State != storage.BookingStateBooked || o.ReceivedOn == nil {
			continue
		}
		d := dateOnly(*o.ReceivedOn)
		boundary(SourceOtherIncome, storage.EuerEntryOtherIncome, o.ID, d, o.GrossCents)
		if yearOf(storage.EuerEntryOtherIncome, o.ID, d) != in.Year {
			continue
		}
		r, err := regime(d)
		if err != nil {
			return err
		}
		src := func(amount int64) Source {
			return Source{Kind: SourceOtherIncome, ID: o.ID, Date: d, AmountCents: amount}
		}
		if r == storage.VATRegimeKleinunternehmer {
			b.add(o.Category, src(o.GrossCents))

			continue
		}
		b.add(o.Category, src(o.NetCents))
		if o.VATCents != 0 {
			b.add(category.VATReceived, src(o.VATCents))
		}
	}

	return nil
}

// addAssets adds the year's depreciation of every asset, as afa.Schedule
// computes it.
func addAssets(in Input, b *builder) error {
	for _, a := range in.Assets {
		years, err := afa.Schedule(toAFAAsset(a))
		if err != nil {
			return fmt.Errorf("euer: asset %s: %w", a.ID, err)
		}
		for _, y := range years {
			if y.Year != in.Year || y.DepreciationCents == 0 {
				continue
			}
			b.add(y.Key, Source{
				Kind:        SourceAsset,
				ID:          a.ID,
				Date:        date(in.Year, time.December, 31),
				AmountCents: y.DepreciationCents,
			})
		}
	}

	return nil
}

// toAFAAsset is the afa view of a stored asset, the same conversion the
// asset routes use.
func toAFAAsset(a storage.Asset) afa.Asset {
	out := afa.Asset{
		AcquiredOn:     a.AcquiredOn,
		CostBasisCents: a.CostBasisCents,
		Method:         afa.Method(a.Method),
		Group:          afa.Group(a.Group),
		DisposedOn:     a.DisposedOn,
	}
	if a.UsefulLifeMonths != nil {
		out.UsefulLifeMonths = int(*a.UsefulLifeMonths)
	}
	if a.OpeningBookValueCents != nil && a.OpeningBookValueYear != nil {
		out.Opening = &afa.Opening{Year: int(*a.OpeningBookValueYear), BookValueCents: *a.OpeningBookValueCents}
	}

	return out
}

// buildLines turns the collected sources into lines in form order. A
// key the year does not map is an error: every registered key has a
// mapping row, a test makes sure of it.
func buildLines(m Mapping, b builder) ([]Line, error) {
	var lines []Line
	for _, c := range category.All() {
		sources := b.sources[c.Key]
		if len(sources) == 0 {
			continue
		}
		ml, ok := m.Line(c.Key)
		if !ok {
			return nil, fmt.Errorf("euer: the %d mapping has no line for %q", m.TaxYear, c.Key)
		}
		sort.SliceStable(sources, func(i, j int) bool {
			if !sources[i].Date.Equal(sources[j].Date) {
				return sources[i].Date.Before(sources[j].Date)
			}

			return sources[i].ID.String() < sources[j].ID.String()
		})
		var sum int64
		for _, s := range sources {
			sum += s.AmountCents
		}
		lines = append(lines, Line{
			Key:         c.Key,
			Zeile:       ml.Zeile,
			Kennzahl:    ml.Kennzahl,
			Operator:    ml.Operator,
			Verified:    ml.Verified,
			LabelDE:     c.LabelDE,
			LabelEN:     c.LabelEN,
			AmountCents: sum,
			Sources:     sources,
		})
		delete(b.sources, c.Key)
	}
	for key := range b.sources {
		return nil, fmt.Errorf("euer: an entry is booked under the unknown key %q", key)
	}

	return lines, nil
}

// effectivePeriods drops every period another row supersedes and sorts
// the rest like VATRegimePeriodRepository.RegimeOn reads them.
func effectivePeriods(all []storage.VATRegimePeriod) []storage.VATRegimePeriod {
	superseded := map[uuid.UUID]bool{}
	for _, p := range all {
		if p.SupersedesID != nil {
			superseded[*p.SupersedesID] = true
		}
	}
	out := make([]storage.VATRegimePeriod, 0, len(all))
	for _, p := range all {
		if !superseded[p.ID] {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.ValidFrom.Equal(b.ValidFrom) {
			return a.ValidFrom.Before(b.ValidFrom)
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}

		return a.ID.String() < b.ID.String()
	})

	return out
}

// regimeOn is the regime of the latest effective period starting on or
// before d.
func regimeOn(periods []storage.VATRegimePeriod, d time.Time) (storage.VATRegime, bool) {
	day := dateOnly(d)
	var best *storage.VATRegimePeriod
	for i := range periods {
		if !dateOnly(periods[i].ValidFrom).After(day) {
			best = &periods[i]
		}
	}
	if best == nil {
		return "", false
	}

	return best.Regime, true
}

// regimesOfYear are the effective periods in force during year: the one
// on 1 January, if any, and every one starting inside the year.
func regimesOfYear(periods []storage.VATRegimePeriod, year int) []Period {
	start := date(year, time.January, 1)
	var out []Period
	if r, ok := regimeOn(periods, start); ok {
		out = append(out, Period{ValidFrom: latestStartOnOrBefore(periods, start), Regime: r})
	}
	for _, p := range periods {
		d := dateOnly(p.ValidFrom)
		if d.After(start) && d.Year() == year {
			out = append(out, Period{ValidFrom: d, Regime: p.Regime})
		}
	}

	return out
}

func latestStartOnOrBefore(periods []storage.VATRegimePeriod, d time.Time) time.Time {
	var best time.Time
	for _, p := range periods {
		if v := dateOnly(p.ValidFrom); !v.After(d) {
			best = v
		}
	}

	return best
}

// assignmentsByEntry indexes the effective assignments. Should two rows
// of one entry both be effective, the newer one wins.
func assignmentsByEntry(list []storage.EuerAssignment) map[entryKey]storage.EuerAssignment {
	out := map[entryKey]storage.EuerAssignment{}
	for _, a := range list {
		k := entryKey{a.EntryKind, a.EntryID}
		if prev, ok := out[k]; ok && prev.CreatedAt.After(a.CreatedAt) {
			continue
		}
		out[k] = a
	}

	return out
}

// inWindowsOf reports whether d lies in one of the two windows of year.
func inWindowsOf(year int, d time.Time) bool {
	day := dateOnly(d)
	for _, w := range windowsOf(year) {
		if !day.Before(w[0]) && !day.After(w[1]) {
			return true
		}
	}

	return false
}

// roundHalfAwayFromZero rounds r to whole cents, a half away from zero.
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
