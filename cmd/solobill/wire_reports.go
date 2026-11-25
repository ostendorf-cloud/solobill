package main

import (
	"context"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
)

// Issue #80: the EÜR report and the year assignments. The report reads
// the repositories the other features wire; this file adds only its own.
func init() {
	wirers = append(wirers, func(_ context.Context, env wireEnv, repos *httpapi.Repositories) error {
		repos.EuerAssignments = env.store.EuerAssignments()

		return nil
	})
}
