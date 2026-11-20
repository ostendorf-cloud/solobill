// Package category is the closed set of EÜR line keys solobill books
// income, expenses and depreciation against.
//
// A key names a line of the Anlage EÜR, never a form number and never a
// Kennzahl: "euer.z27" is the line "Waren, Rohstoffe und Hilfsstoffe".
// The line numbers in the keys are those of the Anlage EÜR 2025, the
// year the set was first defined against. A key is stable forever, even
// when a later form moves its line. The mapping from a key to the line
// and the Kennzahl of one tax year is data per tax year, owned by the
// EÜR report (issue #80), not this package.
//
// A line with two Kennzahlen for "nicht abziehbar" and "abziehbar" gets
// two keys with the suffixes ".nd" and ".d".
//
// Every key is listed in docs/concepts/solobill.md, section "EÜR line
// keys". A test fails when a key is missing there.
package category

import "slices"

// Key is one EÜR line key, for example "euer.z27".
type Key string

// Kind is who produces an amount for a key.
type Kind string

const (
	// KindIncome is a Betriebseinnahme the owner records as other income
	// (issue #76), outside an invoice.
	KindIncome Kind = "income"
	// KindExpense is a Betriebsausgabe the owner records as an expense
	// (issue #76).
	KindExpense Kind = "expense"
	// KindAsset is an amount the asset register computes (issue #78).
	// It is never chosen on an expense.
	KindAsset Kind = "asset"
	// KindDerived is an amount the EÜR report computes from invoices,
	// payments and VAT figures (issue #80). It is never chosen by hand.
	KindDerived Kind = "derived"
)

// Keys of the Betriebseinnahmen.
const (
	// IncomeKleinunternehmer is Zeile 12: Betriebseinnahmen als
	// Kleinunternehmer.
	IncomeKleinunternehmer Key = "euer.z12"
	// IncomeTaxable is Zeile 15: umsatzsteuerpflichtige
	// Betriebseinnahmen.
	IncomeTaxable Key = "euer.z15"
	// IncomeTaxFree is Zeile 16: umsatzsteuerfreie, nicht
	// umsatzsteuerbare Betriebseinnahmen und solche nach section 13b
	// UStG.
	IncomeTaxFree Key = "euer.z16"
	// VATReceived is Zeile 17: vereinnahmte Umsatzsteuer.
	VATReceived Key = "euer.z17"
	// VATRefunded is Zeile 18: vom Finanzamt erstattete Umsatzsteuer.
	VATRefunded Key = "euer.z18"
	// AssetDisposal is Zeile 19: Veräußerung oder Entnahme von
	// Anlagevermögen.
	AssetDisposal Key = "euer.z19"
	// PrivateCarUse is Zeile 20: private Kfz-Nutzung.
	PrivateCarUse Key = "euer.z20"
	// OtherWithdrawals is Zeile 21: sonstige Sach-, Nutzungs- und
	// Leistungsentnahmen.
	OtherWithdrawals Key = "euer.z21"
)

// Keys of the Betriebsausgaben.
const (
	// Goods is Zeile 27: Waren, Rohstoffe und Hilfsstoffe.
	Goods Key = "euer.z27"
	// ExternalServices is Zeile 29: bezogene Fremdleistungen.
	ExternalServices Key = "euer.z29"
	// Staff is Zeile 30: Ausgaben für eigenes Personal.
	Staff Key = "euer.z30"
	// DepreciationIntangible is Zeile 32: AfA auf immaterielle
	// Wirtschaftsgüter.
	DepreciationIntangible Key = "euer.z32"
	// DepreciationMovable is Zeile 33: AfA auf bewegliche
	// Wirtschaftsgüter.
	DepreciationMovable Key = "euer.z33"
	// LowValueAssets is Zeile 36: geringwertige Wirtschaftsgüter nach
	// section 6 Abs. 2 EStG.
	LowValueAssets Key = "euer.z36"
	// PoolDissolution is Zeile 37: Auflösung Sammelposten nach section
	// 6 Abs. 2a EStG.
	PoolDissolution Key = "euer.z37"
	// ResidualBookValue is Zeile 38: Restbuchwerte der ausgeschiedenen
	// Anlagegüter.
	ResidualBookValue Key = "euer.z38"
	// Rent is Zeile 39: Miete und Pacht für Geschäftsräume.
	Rent Key = "euer.z39"
	// Telecommunication is Zeile 43: Telekommunikation.
	Telecommunication Key = "euer.z43"
	// Travel is Zeile 44: Übernachtungs- und Reisenebenkosten.
	Travel Key = "euer.z44"
	// Training is Zeile 45: Fortbildungskosten.
	Training Key = "euer.z45"
	// LegalAndTaxAdvice is Zeile 46: Rechts- und Steuerberatung,
	// Buchführung.
	LegalAndTaxAdvice Key = "euer.z46"
	// LeasingMovable is Zeile 47: Miete und Leasing für bewegliche
	// Wirtschaftsgüter ohne Kfz.
	LeasingMovable Key = "euer.z47"
	// Maintenance is Zeile 48: Erhaltungsaufwendungen.
	Maintenance Key = "euer.z48"
	// FeesAndInsurance is Zeile 49: Beiträge, Gebühren, Abgaben und
	// Versicherungen.
	FeesAndInsurance Key = "euer.z49"
	// ITRunningCosts is Zeile 50: laufende EDV-Kosten.
	ITRunningCosts Key = "euer.z50"
	// WorkEquipment is Zeile 51: Arbeitsmittel wie Bürobedarf, Porto,
	// Fachliteratur.
	WorkEquipment Key = "euer.z51"
	// Disposal is Zeile 52: Abfallbeseitigung und Entsorgung.
	Disposal Key = "euer.z52"
	// PackagingAndTransport is Zeile 53: Verpackung und Transport.
	PackagingAndTransport Key = "euer.z53"
	// Advertising is Zeile 54: Werbekosten.
	Advertising Key = "euer.z54"
	// InputVAT is Zeile 57: gezahlte, abziehbare Vorsteuer.
	InputVAT Key = "euer.z57"
	// VATPaid is Zeile 58: an das Finanzamt gezahlte Umsatzsteuer,
	// including the USt-Vorauszahlungen.
	VATPaid Key = "euer.z58"
	// OtherExpenses is Zeile 60: übrige unbeschränkt abziehbare
	// Betriebsausgaben.
	OtherExpenses Key = "euer.z60"
	// GiftsNonDeductible is Zeile 62, nicht abziehbar: Geschenke.
	GiftsNonDeductible Key = "euer.z62.nd"
	// GiftsDeductible is Zeile 62, abziehbar: Geschenke.
	GiftsDeductible Key = "euer.z62.d"
	// EntertainmentNonDeductible is Zeile 63, nicht abziehbar:
	// Bewirtungsaufwendungen.
	EntertainmentNonDeductible Key = "euer.z63.nd"
	// EntertainmentDeductible is Zeile 63, abziehbar:
	// Bewirtungsaufwendungen.
	EntertainmentDeductible Key = "euer.z63.d"
	// HomeOfficeNonDeductible is Zeile 65, nicht abziehbar: häusliches
	// Arbeitszimmer.
	HomeOfficeNonDeductible Key = "euer.z65.nd"
	// HomeOfficeDeductible is Zeile 65, abziehbar: häusliches
	// Arbeitszimmer.
	HomeOfficeDeductible Key = "euer.z65.d"
	// HomeOfficeDailyAllowance is Zeile 66: Tagespauschale für die
	// Tätigkeit in der häuslichen Wohnung.
	HomeOfficeDailyAllowance Key = "euer.z66"
)

// Category is one key with its kind and its labels. LabelDE is the
// wording of the 2025 form line, shortened; LabelEN is a translation for
// the English user interface, never printed on a tax form.
type Category struct {
	Key     Key
	Kind    Kind
	LabelDE string
	LabelEN string
}

// all is the closed set, in form order. Append only: a key is never
// renamed or removed, because booked rows reference it.
var all = []Category{
	{IncomeKleinunternehmer, KindIncome, "Betriebseinnahmen als Kleinunternehmer", "Revenue as a small business"},
	{IncomeTaxable, KindIncome, "Umsatzsteuerpflichtige Betriebseinnahmen", "Revenue subject to VAT"},
	{IncomeTaxFree, KindIncome, "Umsatzsteuerfreie oder nicht steuerbare Betriebseinnahmen", "VAT exempt or non taxable revenue"},
	{VATReceived, KindDerived, "Vereinnahmte Umsatzsteuer", "VAT received"},
	{VATRefunded, KindIncome, "Vom Finanzamt erstattete Umsatzsteuer", "VAT refunded by the tax office"},
	{AssetDisposal, KindIncome, "Veräußerung oder Entnahme von Anlagevermögen", "Sale or withdrawal of fixed assets"},
	{PrivateCarUse, KindIncome, "Private Kfz-Nutzung", "Private use of a business car"},
	{OtherWithdrawals, KindIncome, "Sonstige Sach-, Nutzungs- und Leistungsentnahmen", "Other private withdrawals"},
	{Goods, KindExpense, "Waren, Rohstoffe und Hilfsstoffe", "Goods and materials"},
	{ExternalServices, KindExpense, "Bezogene Fremdleistungen", "Subcontracted services"},
	{Staff, KindExpense, "Ausgaben für eigenes Personal", "Staff costs"},
	{DepreciationIntangible, KindAsset, "AfA auf immaterielle Wirtschaftsgüter", "Depreciation of intangible assets"},
	{DepreciationMovable, KindAsset, "AfA auf bewegliche Wirtschaftsgüter", "Depreciation of movable assets"},
	{LowValueAssets, KindAsset, "Geringwertige Wirtschaftsgüter", "Low value assets"},
	{PoolDissolution, KindAsset, "Auflösung Sammelposten", "Dissolution of the asset pool"},
	{ResidualBookValue, KindAsset, "Restbuchwerte ausgeschiedener Anlagegüter", "Residual book value of disposed assets"},
	{Rent, KindExpense, "Miete und Pacht für Geschäftsräume", "Rent for business premises"},
	{Telecommunication, KindExpense, "Telekommunikation", "Telecommunication"},
	{Travel, KindExpense, "Übernachtungs- und Reisenebenkosten", "Accommodation and travel expenses"},
	{Training, KindExpense, "Fortbildungskosten", "Training"},
	{LegalAndTaxAdvice, KindExpense, "Rechts- und Steuerberatung, Buchführung", "Legal and tax advice, bookkeeping"},
	{LeasingMovable, KindExpense, "Miete und Leasing für bewegliche Wirtschaftsgüter", "Rent and leasing of movable assets"},
	{Maintenance, KindExpense, "Erhaltungsaufwendungen", "Maintenance"},
	{FeesAndInsurance, KindExpense, "Beiträge, Gebühren, Abgaben und Versicherungen", "Fees, contributions and insurance"},
	{ITRunningCosts, KindExpense, "Laufende EDV-Kosten", "Running IT costs"},
	{WorkEquipment, KindExpense, "Arbeitsmittel", "Work equipment and office supplies"},
	{Disposal, KindExpense, "Abfallbeseitigung und Entsorgung", "Waste disposal"},
	{PackagingAndTransport, KindExpense, "Verpackung und Transport", "Packaging and transport"},
	{Advertising, KindExpense, "Werbekosten", "Advertising"},
	{InputVAT, KindDerived, "Abziehbare Vorsteuer", "Deductible input VAT"},
	{VATPaid, KindExpense, "An das Finanzamt gezahlte Umsatzsteuer", "VAT paid to the tax office"},
	{OtherExpenses, KindExpense, "Übrige unbeschränkt abziehbare Betriebsausgaben", "Other fully deductible expenses"},
	{GiftsNonDeductible, KindExpense, "Geschenke, nicht abziehbar", "Gifts, non deductible"},
	{GiftsDeductible, KindExpense, "Geschenke, abziehbar", "Gifts, deductible"},
	{EntertainmentNonDeductible, KindExpense, "Bewirtungsaufwendungen, nicht abziehbar", "Business meals, non deductible"},
	{EntertainmentDeductible, KindExpense, "Bewirtungsaufwendungen, abziehbar", "Business meals, deductible"},
	{HomeOfficeNonDeductible, KindExpense, "Häusliches Arbeitszimmer, nicht abziehbar", "Home office, non deductible"},
	{HomeOfficeDeductible, KindExpense, "Häusliches Arbeitszimmer, abziehbar", "Home office, deductible"},
	{HomeOfficeDailyAllowance, KindExpense, "Tagespauschale häusliche Wohnung", "Home office daily allowance"},
}

// All returns every category in form order. The slice is a copy.
func All() []Category {
	return slices.Clone(all)
}

// Lookup returns the category of key and whether it exists.
func Lookup(key Key) (Category, bool) {
	for _, c := range all {
		if c.Key == key {
			return c, true
		}
	}

	return Category{}, false
}

// Valid reports whether key exists and has the given kind. An expense
// accepts only KindExpense keys, other income only KindIncome keys.
func Valid(key Key, kind Kind) bool {
	c, ok := Lookup(key)

	return ok && c.Kind == kind
}
