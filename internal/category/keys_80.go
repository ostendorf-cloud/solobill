package category

// Keys owned by the EÜR report, issue #80. Documented in
// docs/concepts/euer-keys/80.md, which also records how the open points
// of the contract are handled:
//   - Zeile 65: which of Kz 162 and 172 is the deductible one is not
//     verified. The mapping (internal/euer/mapping) shows no Kennzahl for
//     either key until the owner checks the form.
//   - Zeile 13: no key. Every Kleinunternehmer receipt goes to Zeile 12,
//     an open question for the owner.
//   - Kz 111: stays on Zeile 12 as keys_67.go registered it, marked
//     unverified in the mapping.

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
