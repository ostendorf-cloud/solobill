package main

import (
	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres"
)

// repositories wires httpapi.Repositories from the postgres store. A
// field stays nil until the issue owning it adds its implementation;
// that issue sets its own line here and nothing else in this file. The
// fields are separated by blank lines on purpose, so two branches
// setting neighbouring fields do not produce a merge conflict.
func repositories(store *postgres.Store) httpapi.Repositories {
	return httpapi.Repositories{
		Scope: store,

		// Issue #68 (I2): SellerProfiles, VATRegimePeriods.

		Customers: store.Customers(),

		CatalogueItems: store.CatalogueItems(),

		Documents: store.Documents(),

		DocumentNumbers: store.DocumentNumbers(),

		// Issue #71 (I5): Payments.

		// Issue #74 (I8): Renderer.

		// Issue #75 (I9): Files, Objects.

		// Issue #76 (I10a): Expenses, OtherIncome.

		// Issue #78 (I11a): Assets.

		// Issue #81 (I13): Imports, ImportedDocuments.
	}
}
