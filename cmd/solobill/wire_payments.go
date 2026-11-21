package main

import (
	"context"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
)

// Issue #71: payments of an invoice chain.
func init() {
	wirers = append(wirers, func(_ context.Context, env wireEnv, repos *httpapi.Repositories) error {
		repos.Payments = env.store.Payments()

		return nil
	})
}
