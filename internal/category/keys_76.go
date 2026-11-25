package category

// Keys owned by the expenses issue, #76: the car cost lines, Zeilen 68 to
// 73 of the Anlage EÜR 2025 ("Kfz-Kosten und andere Fahrtkosten"), read
// from the BMF letter of 29 August 2025. Documented with the source in
// docs/concepts/euer-keys/76.md.
//
// The form subtracts Zeile 72 and adds Zeile 73. A key carries no sign:
// the EÜR report (issue #80) applies the sign of the line. Computing the
// figures of Zeilen 71 to 73 (Fahrtenbuch, 1 percent rule,
// Entfernungspauschale) stays out of the milestone, so the owner enters
// them as expenses.

const (
	// CarLeasing is Zeile 68: Leasingkosten.
	CarLeasing Key = "euer.z68"
	// CarTaxesInsuranceToll is Zeile 69: Steuern, Versicherungen und
	// Maut.
	CarTaxesInsuranceToll Key = "euer.z69"
	// CarOtherActualCosts is Zeile 70: sonstige tatsächliche Fahrtkosten
	// ohne AfA und Zinsen, for example repairs, servicing, fuel, flights
	// and public transport.
	CarOtherActualCosts Key = "euer.z70"
	// CarPrivateVehicle is Zeile 71: Fahrtkosten für nicht zum
	// Betriebsvermögen gehörende Fahrzeuge (Nutzungseinlage).
	CarPrivateVehicle Key = "euer.z71"
	// CarCommuteCosts is Zeile 72: Fahrtkosten für Wege zwischen Wohnung
	// und erster Betriebsstätte und Familienheimfahrten, pauschaliert
	// oder tatsächlich. The form subtracts this line.
	CarCommuteCosts Key = "euer.z72"
	// CarCommuteAllowance is Zeile 73: mindestens abziehbare Fahrtkosten
	// für Wege zwischen Wohnung und erster Betriebsstätte
	// (Entfernungspauschale) und Familienheimfahrten. The form adds this
	// line.
	CarCommuteAllowance Key = "euer.z73"
)

func init() {
	register(76,
		Category{Key: CarLeasing, Kind: KindExpense, LabelDE: "Kfz: Leasingkosten", LabelEN: "Car: leasing"},
		Category{Key: CarTaxesInsuranceToll, Kind: KindExpense, LabelDE: "Kfz: Steuern, Versicherungen und Maut", LabelEN: "Car: taxes, insurance and tolls"},
		Category{Key: CarOtherActualCosts, Kind: KindExpense, LabelDE: "Sonstige tatsächliche Fahrtkosten ohne AfA und Zinsen", LabelEN: "Other actual travel costs without depreciation and interest"},
		Category{Key: CarPrivateVehicle, Kind: KindExpense, LabelDE: "Fahrtkosten für nicht betriebliche Fahrzeuge (Nutzungseinlage)", LabelEN: "Travel costs of vehicles outside the business assets"},
		Category{Key: CarCommuteCosts, Kind: KindExpense, LabelDE: "Fahrtkosten Wohnung und erste Betriebsstätte, Familienheimfahrten (Abzug)", LabelEN: "Commuting and home trips, deducted from car costs"},
		Category{Key: CarCommuteAllowance, Kind: KindExpense, LabelDE: "Mindestens abziehbare Fahrtkosten (Entfernungspauschale)", LabelEN: "Minimum deductible commuting costs (distance allowance)"},
	)
}
