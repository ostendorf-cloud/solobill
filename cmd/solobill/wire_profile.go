package main

import (
	"context"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
)

// Issue #68: the seller profile and the VAT regime history.
func init() {
	wirers = append(wirers, func(_ context.Context, env wireEnv, repos *httpapi.Repositories) error {
		repos.SellerProfiles = env.store.SellerProfiles()
		repos.VATRegimePeriods = env.store.VATRegimePeriods()

		return nil
	})
}
