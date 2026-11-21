package category

// Keys owned by the EÜR report, issue #80. Documented in
// docs/concepts/euer-keys/80.md. The contract registers the two Zeile 65
// keys here already; which of Kz 162 and 172 is the deductible one is
// open and #80 resolves it in that file. #80 also adds Zeile 13 here if
// it decides the line is chosen rather than derived.

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
