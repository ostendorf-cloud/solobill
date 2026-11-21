package main

import (
	"context"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
)

// Issue #78: the asset register.
func init() {
	wirers = append(wirers, func(_ context.Context, env wireEnv, repos *httpapi.Repositories) error {
		repos.Assets = env.store.Assets()

		return nil
	})
}
