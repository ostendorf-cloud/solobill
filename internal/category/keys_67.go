package category

// Keys of the contract (issue #67): the lines of the Anlage EÜR 2025 read
// for the plan. Documented in docs/concepts/euer-keys/67.md.

// Keys of the Betriebseinnahmen.
const (
	// IncomeKleinunternehmer is Zeile 12: Betriebseinnahmen als
	// Kleinunternehmer. Caveat: the read copy also shows a printed 111
	// next to Zeile 10; whether Kz 111 belongs to Zeile 10, Zeile 12 or
	// both is open and resolved by issue #80 in
	// docs/concepts/euer-keys/80.md. The key names the line, not the Kz.
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
	// HomeOfficeDailyAllowance is Zeile 66: Tagespauschale für die
	// Tätigkeit in der häuslichen Wohnung.
	HomeOfficeDailyAllowance Key = "euer.z66"
)

func init() {
	register(67,
		Category{Key: IncomeKleinunternehmer, Kind: KindIncome, LabelDE: "Betriebseinnahmen als Kleinunternehmer", LabelEN: "Revenue as a small business"},
		Category{Key: IncomeTaxable, Kind: KindIncome, LabelDE: "Umsatzsteuerpflichtige Betriebseinnahmen", LabelEN: "Revenue subject to VAT"},
		Category{Key: IncomeTaxFree, Kind: KindIncome, LabelDE: "Umsatzsteuerfreie oder nicht steuerbare Betriebseinnahmen", LabelEN: "VAT exempt or non taxable revenue"},
		Category{Key: VATReceived, Kind: KindDerived, LabelDE: "Vereinnahmte Umsatzsteuer", LabelEN: "VAT received"},
		Category{Key: VATRefunded, Kind: KindIncome, LabelDE: "Vom Finanzamt erstattete Umsatzsteuer", LabelEN: "VAT refunded by the tax office"},
		Category{Key: AssetDisposal, Kind: KindIncome, LabelDE: "Veräußerung oder Entnahme von Anlagevermögen", LabelEN: "Sale or withdrawal of fixed assets"},
		Category{Key: PrivateCarUse, Kind: KindIncome, LabelDE: "Private Kfz-Nutzung", LabelEN: "Private use of a business car"},
		Category{Key: OtherWithdrawals, Kind: KindIncome, LabelDE: "Sonstige Sach-, Nutzungs- und Leistungsentnahmen", LabelEN: "Other private withdrawals"},
		Category{Key: Goods, Kind: KindExpense, LabelDE: "Waren, Rohstoffe und Hilfsstoffe", LabelEN: "Goods and materials"},
		Category{Key: ExternalServices, Kind: KindExpense, LabelDE: "Bezogene Fremdleistungen", LabelEN: "Subcontracted services"},
		Category{Key: Staff, Kind: KindExpense, LabelDE: "Ausgaben für eigenes Personal", LabelEN: "Staff costs"},
		Category{Key: DepreciationIntangible, Kind: KindAsset, LabelDE: "AfA auf immaterielle Wirtschaftsgüter", LabelEN: "Depreciation of intangible assets"},
		Category{Key: DepreciationMovable, Kind: KindAsset, LabelDE: "AfA auf bewegliche Wirtschaftsgüter", LabelEN: "Depreciation of movable assets"},
		Category{Key: LowValueAssets, Kind: KindAsset, LabelDE: "Geringwertige Wirtschaftsgüter", LabelEN: "Low value assets"},
		Category{Key: PoolDissolution, Kind: KindAsset, LabelDE: "Auflösung Sammelposten", LabelEN: "Dissolution of the asset pool"},
		Category{Key: ResidualBookValue, Kind: KindAsset, LabelDE: "Restbuchwerte ausgeschiedener Anlagegüter", LabelEN: "Residual book value of disposed assets"},
		Category{Key: Rent, Kind: KindExpense, LabelDE: "Miete und Pacht für Geschäftsräume", LabelEN: "Rent for business premises"},
		Category{Key: Telecommunication, Kind: KindExpense, LabelDE: "Telekommunikation", LabelEN: "Telecommunication"},
		Category{Key: Travel, Kind: KindExpense, LabelDE: "Übernachtungs- und Reisenebenkosten", LabelEN: "Accommodation and travel expenses"},
		Category{Key: Training, Kind: KindExpense, LabelDE: "Fortbildungskosten", LabelEN: "Training"},
		Category{Key: LegalAndTaxAdvice, Kind: KindExpense, LabelDE: "Rechts- und Steuerberatung, Buchführung", LabelEN: "Legal and tax advice, bookkeeping"},
		Category{Key: LeasingMovable, Kind: KindExpense, LabelDE: "Miete und Leasing für bewegliche Wirtschaftsgüter", LabelEN: "Rent and leasing of movable assets"},
		Category{Key: Maintenance, Kind: KindExpense, LabelDE: "Erhaltungsaufwendungen", LabelEN: "Maintenance"},
		Category{Key: FeesAndInsurance, Kind: KindExpense, LabelDE: "Beiträge, Gebühren, Abgaben und Versicherungen", LabelEN: "Fees, contributions and insurance"},
		Category{Key: ITRunningCosts, Kind: KindExpense, LabelDE: "Laufende EDV-Kosten", LabelEN: "Running IT costs"},
		Category{Key: WorkEquipment, Kind: KindExpense, LabelDE: "Arbeitsmittel", LabelEN: "Work equipment and office supplies"},
		Category{Key: Disposal, Kind: KindExpense, LabelDE: "Abfallbeseitigung und Entsorgung", LabelEN: "Waste disposal"},
		Category{Key: PackagingAndTransport, Kind: KindExpense, LabelDE: "Verpackung und Transport", LabelEN: "Packaging and transport"},
		Category{Key: Advertising, Kind: KindExpense, LabelDE: "Werbekosten", LabelEN: "Advertising"},
		Category{Key: InputVAT, Kind: KindDerived, LabelDE: "Abziehbare Vorsteuer", LabelEN: "Deductible input VAT"},
		Category{Key: VATPaid, Kind: KindExpense, LabelDE: "An das Finanzamt gezahlte Umsatzsteuer", LabelEN: "VAT paid to the tax office"},
		Category{Key: OtherExpenses, Kind: KindExpense, LabelDE: "Übrige unbeschränkt abziehbare Betriebsausgaben", LabelEN: "Other fully deductible expenses"},
		Category{Key: GiftsNonDeductible, Kind: KindExpense, LabelDE: "Geschenke, nicht abziehbar", LabelEN: "Gifts, non deductible"},
		Category{Key: GiftsDeductible, Kind: KindExpense, LabelDE: "Geschenke, abziehbar", LabelEN: "Gifts, deductible"},
		Category{Key: EntertainmentNonDeductible, Kind: KindExpense, LabelDE: "Bewirtungsaufwendungen, nicht abziehbar", LabelEN: "Business meals, non deductible"},
		Category{Key: EntertainmentDeductible, Kind: KindExpense, LabelDE: "Bewirtungsaufwendungen, abziehbar", LabelEN: "Business meals, deductible"},
		Category{Key: HomeOfficeDailyAllowance, Kind: KindExpense, LabelDE: "Tagespauschale häusliche Wohnung", LabelEN: "Home office daily allowance"},
	)
}
