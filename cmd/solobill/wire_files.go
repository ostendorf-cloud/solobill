package main

import (
	"context"
	"fmt"

	"github.com/ostendorf-cloud/solobill/internal/config"
	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/objectstore"
)

// init wires issue #75: the files repository and the object store behind
// its gate.
func init() {
	wirers = append(wirers, wireFiles)
}

// wireFiles sets Repositories.Files and Repositories.Objects. The process
// always starts: an unconfigured, unreachable or unprotected bucket only
// gates the file routes (docs/concepts/solobill.md, "Receipt storage and
// GoBD"). The bucket is checked once here, before the server listens, and
// then every minute until ctx ends.
//
// Repositories.SellerProfileLogos stays nil: its column,
// seller_profiles.logo_file_id, can only be added once the seller_profiles
// table of issue #68 exists. The logo routes answer 501 until then.
func wireFiles(ctx context.Context, env wireEnv, repos *httpapi.Repositories) error {
	store, err := buildObjectStore(env.cfg.ObjectStore)
	if err != nil {
		return fmt.Errorf("files: %w", err)
	}

	monitorCfg := objectstore.MonitorConfig{
		RequireVersioning: env.cfg.ObjectStore.RequireVersioning,
		RequireObjectLock: env.cfg.ObjectStore.ObjectLock,
		Logger:            env.logger,
	}
	guarded := &objectstore.Guarded{}
	// A nil *Client must not become a non-nil storage.ObjectStore.
	if store != nil {
		monitorCfg.Checker = store
		guarded.Store = store
	}
	guarded.Monitor = objectstore.NewMonitor(monitorCfg)

	guarded.Monitor.Check(ctx)
	go guarded.Monitor.Run(ctx, objectstore.DefaultCheckInterval)

	repos.Files = env.store.Files()
	repos.Objects = guarded

	return nil
}

// buildObjectStore builds the S3 client from cfg, or returns nil when no
// S3_ENDPOINT is configured. It contacts nothing.
func buildObjectStore(cfg config.ObjectStoreConfig) (*objectstore.Client, error) {
	if !cfg.Enabled {
		return nil, nil //nolint:nilnil // no store configured is a state the gate reports, not an error
	}

	client, err := objectstore.New(objectstore.Config{
		Endpoint:        cfg.Endpoint,
		Bucket:          cfg.Bucket,
		Region:          cfg.Region,
		UsePathStyle:    cfg.UsePathStyle,
		AccessKeyID:     cfg.AccessKeyID,
		SecretAccessKey: cfg.SecretAccessKey,
		ObjectLock:      cfg.ObjectLock,
	})
	if err != nil {
		return nil, fmt.Errorf("build object store client: %w", err)
	}

	return client, nil
}
