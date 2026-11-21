package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ostendorf-cloud/govue/health"

	"github.com/ostendorf-cloud/solobill/internal/config"
	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres"
)

// wireEnv is what a feature's wiring may read: the configuration, the
// logger, the postgres store and the health registry a feature adds its
// readiness checks to.
type wireEnv struct {
	cfg    config.Config
	logger *slog.Logger
	store  *postgres.Store
	health *health.Registry
}

// wireFunc sets a feature's fields on repos. It may add readiness checks
// to env.health. It fails startup by returning an error.
type wireFunc func(ctx context.Context, env wireEnv, repos *httpapi.Repositories) error

// wirers is every feature's wiring. A feature issue adds a file
// wire_<feature>.go whose init appends its own wireFunc here. It never
// edits this file or main.go: see AGENTS.md's conflict zones.
var wirers []wireFunc

// buildRepositories wires the repositories that exist since the
// contract issue (#67), then runs every feature's wireFunc.
func buildRepositories(ctx context.Context, env wireEnv) (httpapi.Repositories, error) {
	repos := httpapi.Repositories{
		Scope:           env.store,
		Customers:       env.store.Customers(),
		CatalogueItems:  env.store.CatalogueItems(),
		Documents:       env.store.Documents(),
		DocumentNumbers: env.store.DocumentNumbers(),
	}

	for _, wire := range wirers {
		if err := wire(ctx, env, &repos); err != nil {
			return httpapi.Repositories{}, fmt.Errorf("wire: %w", err)
		}
	}

	return repos, nil
}
