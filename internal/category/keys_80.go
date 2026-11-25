package category

// Keys owned by the EÜR report, issue #80. Documented in
// docs/concepts/euer-keys/80.md, which also records how the open points
// of the contract were settled from the form of 29 August 2025:
//   - Zeile 65: Kz 162 is the non deductible part, Kz 172 the deductible
//     one, by the form's two columns.
//   - Zeile 13: no key. It is a "davon" line inside Zeile 12, and nothing
//     in solobill marks a receipt as nicht steuerbar. An open question
//     for the owner.
//   - Kz 111: the amount field of Zeile 12, as keys_67.go registered it.

const (
	// HomeOfficeNonDeductible is Zeile 65, nicht abziehbar: häusliches
	// Arbeitszimmer.
	HomeOfficeNonDeductible Key = "euer.z65.nd"
	// HomeOfficeDeductible is Zeile 65, abziehbar: häusliches
	// Arbeitszimmer.
	HomeOfficeDeductible Key = "euer.z65.d"
)

func init() {
	register(80,
		Category{Key: HomeOfficeNonDeductible, Kind: KindExpense, LabelDE: "Häusliches Arbeitszimmer, nicht abziehbar", LabelEN: "Home office, non deductible"},
		Category{Key: HomeOfficeDeductible, Kind: KindExpense, LabelDE: "Häusliches Arbeitszimmer, abziehbar", LabelEN: "Home office, deductible"},
	)
}
