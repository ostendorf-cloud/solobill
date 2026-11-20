package httpapi

import (
	"context"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// Repositories is every repository the year end milestone's handlers
// call, fixed once by the contract issue (#67) so no later issue edits
// Deps. A nil field means the issue owning it has not merged yet: the
// routes depending on it still answer 501 from their stub. cmd/solobill
// wires each field from internal/storage/postgres.Store once the
// implementation exists.
type Repositories struct {
	// Scope opens the tenant scoped transaction every call below needs.
	Scope storage.TenantScope

	// Issue #68 (I2).
	SellerProfiles   storage.SellerProfileRepository
	VATRegimePeriods storage.VATRegimePeriodRepository

	// Issue #69 (I3).
	Customers      storage.CustomerRepository
	CatalogueItems storage.CatalogueItemRepository

	// Issue #70 (I4).
	Documents       storage.DocumentRepository
	DocumentNumbers storage.DocumentNumberRepository

	// Issue #71 (I5).
	Payments storage.PaymentRepository

	// Issue #74 (I8).
	Renderer DocumentRenderer

	// Issue #75 (I9).
	Files   storage.FileRepository
	Objects storage.ObjectStore

	// Issue #76 (I10a).
	Expenses    storage.ExpenseRepository
	OtherIncome storage.OtherIncomeRepository

	// Issue #78 (I11a).
	Assets storage.AssetRepository

	// Issue #81 (I13).
	Imports           storage.ImportRepository
	ImportedDocuments storage.ImportedDocumentRepository
}

// DocumentRenderer turns a frozen payload into a PDF. Defined here, at
// its consumer. Implemented by issue #74 (I8) with Typst.
type DocumentRenderer interface {
	// Render renders payload (a stored docpayload.FrozenPayloadV1, as
	// frozen) with the template version the payload names. draft renders
	// the "Entwurf" watermark and no number.
	Render(ctx context.Context, payload []byte, draft bool) ([]byte, error)
}
