package main

import (
	"context"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
)

// Issue #76: expenses and other income.
func init() {
	wirers = append(wirers, func(_ context.Context, env wireEnv, repos *httpapi.Repositories) error {
		repos.Expenses = env.store.Expenses()
		repos.OtherIncome = env.store.OtherIncome()

		return nil
	})
}
