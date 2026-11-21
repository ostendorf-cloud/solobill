package storage

import (
	"time"

	"github.com/google/uuid"
)

// DepreciationMethod is how an asset's cost becomes an expense. The set
// is the owner's decision for this milestone: no degressive method.
type DepreciationMethod string

const (
	// DepreciationLinear spreads the cost evenly over UsefulLifeMonths,
	// pro rata by month in the acquisition year (section 7 Abs. 1 Satz 4
	// EStG: the acquisition month counts in full).
	DepreciationLinear DepreciationMethod = "linear"
	// DepreciationLowValue is a GWG under section 6 Abs. 2 EStG: full
	// deduction in the acquisition year.
	DepreciationLowValue DepreciationMethod = "gwg"
	// DepreciationPool is the Sammelposten under section 6 Abs. 2a EStG:
	// one fifth in the year of forming and each of the four following
	// years. A disposal does not reduce the pool.
	DepreciationPool DepreciationMethod = "sammelposten"
	// DepreciationImmediate is an item up to 250 EUR expensed at once.
	DepreciationImmediate DepreciationMethod = "immediate"
)

// AssetGroup is the Anlage AVEÜR group an asset is reported in.
type AssetGroup string

const (
	// AssetGroupIntangible is an immaterielles Wirtschaftsgut.
	AssetGroupIntangible AssetGroup = "intangible"
	// AssetGroupOfficeEquipment is Büroausstattung.
	AssetGroupOfficeEquipment AssetGroup = "office_equipment"
	// AssetGroupOtherMovable is an anderes bewegliches Wirtschaftsgut.
	AssetGroupOtherMovable AssetGroup = "other_movable"
)

// Asset is one entry of the asset register. Issue #78 (I11a) creates
// the table; the pure computation lives in internal/afa.
//
// CostBasisCents is net of deductible Vorsteuer under the standard
// regime and gross under the Kleinunternehmer regime, who has no
// Vorsteuer deduction. That reading of section 6 Abs. 2 EStG and section
// 9b EStG is an interpretation the owner confirms with a Steuerberater;
// see docs/concepts/solobill.md.
type Asset struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	Name       string
	AcquiredOn time.Time
	// CostBasisCents is the acquisition cost in integer cents.
	CostBasisCents int64
	Method         DepreciationMethod
	Group          AssetGroup
	// UsefulLifeMonths is set, and only set, for DepreciationLinear. The
	// owner enters it per asset from the AfA tables.
	UsefulLifeMonths *int32
	// ExpenseID is the expense (with IsAsset set) the asset came from,
	// nil for an imported asset.
	ExpenseID *uuid.UUID
	// OpeningBookValueCents and OpeningBookValueYear carry an imported
	// asset's book value at 1 January of that year, so the schedule
	// continues from it instead of recomputing earlier years.
	OpeningBookValueCents *int64
	OpeningBookValueYear  *int32
	DisposedOn            *time.Time
	// DisposalProceedsCents is what a sale brought in, booked as other
	// income under category.AssetDisposal.
	DisposalProceedsCents *int64
	Imported              bool
	CreatedAt             time.Time
	UpdatedAt             time.Time
}
