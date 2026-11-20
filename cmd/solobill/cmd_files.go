package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/logging"

	"github.com/ostendorf-cloud/solobill/internal/config"
	"github.com/ostendorf-cloud/solobill/internal/objectstore"
	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres"
)

// init registers "solobill files", issue #75's integrity scan.
func init() {
	registerCommand("files", func(ctx context.Context, args []string) error {
		return filesCommand(ctx, args, os.Stdout, runFilesVerify)
	})
}

func filesUsage(w io.Writer) {
	fmt.Fprint(w, "usage: solobill files verify\n\n"+
		"verify reads every stored file, recomputes its SHA-256 and compares it with the database.\n"+
		"It exits non-zero on a mismatch or a missing object and logs each failing file id.\n"+
		"Configured through the environment: DATABASE_*, S3_* and LOG_LEVEL.\n")
}

// filesCommand dispatches "solobill files <subcommand>" with the same
// usage rules as the server: -h and --help print the usage and exit 0,
// any other argument starting with "-" prints it and exits 2, and an
// unknown or missing subcommand exits 2.
func filesCommand(ctx context.Context, args []string, out io.Writer, verify func(context.Context) error) error {
	if len(args) == 0 {
		filesUsage(out)

		return exitCode{code: 2, err: errors.New("solobill files: missing subcommand, known: verify")}
	}

	switch first := args[0]; {
	case first == "-h" || first == "--help":
		filesUsage(out)

		return exitCode{code: 0}
	case strings.HasPrefix(first, "-"):
		filesUsage(out)

		return exitCode{code: 2, err: fmt.Errorf("solobill files: unknown flag %q", first)}
	case first != "verify":
		return exitCode{code: 2, err: fmt.Errorf("solobill files: unknown subcommand %q, known: verify", first)}
	}

	if len(args) > 1 {
		if args[1] == "-h" || args[1] == "--help" {
			filesUsage(out)

			return exitCode{code: 0}
		}
		filesUsage(out)

		return exitCode{code: 2, err: fmt.Errorf("solobill files verify: takes no arguments, got %q", args[1])}
	}

	return verify(ctx)
}

// runFilesVerify loads the configuration, opens the database and the
// store, and runs verifyFiles. The scan reads the store directly, not
// through the gate of the file routes: it must also run while versioning
// or object lock is missing, which is when it matters most.
func runFilesVerify(ctx context.Context) error {
	cfg, err := config.LoadFilesVerify(os.Getenv)
	if err != nil {
		return err
	}

	logger := logging.New(os.Stdout, cfg.LogLevel)

	pool, err := postgres.NewPoolWithCredentials(ctx, cfg.DatabaseURL, cfg.DatabaseUser, cfg.DatabasePassword)
	if err != nil {
		return fmt.Errorf("solobill files verify: open database: %w", err)
	}
	defer pool.Close()

	store, err := buildObjectStore(cfg.ObjectStore, logger)
	if err != nil {
		return fmt.Errorf("solobill files verify: %w", err)
	}

	return verifyFiles(ctx, postgres.NewStore(pool), store, logger)
}

// fileCatalog is what verifyFiles reads the files rows through.
// *postgres.Store implements it.
type fileCatalog interface {
	storage.TenantScope
	TenantIDs(ctx context.Context) ([]uuid.UUID, error)
	ListFiles(ctx context.Context) ([]storage.File, error)
}

// objectReader is what verifyFiles reads objects through: the current
// object for a row without a version id, the recorded version otherwise.
// *objectstore.Client implements it.
type objectReader interface {
	Get(ctx context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error)
	storage.VersionedObjectReader
}

// verifyFiles reads every file of every tenant, recomputes its SHA-256 and
// size, and logs each mismatch or missing object at ERROR with the file
// id. It returns an exit code 1 error when any file failed. The rows of a
// tenant are listed inside its scope; the objects are read after that
// transaction ended, so a slow store holds no database transaction open.
func verifyFiles(ctx context.Context, catalog fileCatalog, objects objectReader, logger *slog.Logger) error {
	tenants, err := catalog.TenantIDs(ctx)
	if err != nil {
		return fmt.Errorf("solobill files verify: list tenants: %w", err)
	}

	var checked, failed int
	for _, tenantID := range tenants {
		var files []storage.File
		err := catalog.WithTenant(ctx, tenantID, func(ctx context.Context) error {
			var err error
			files, err = catalog.ListFiles(ctx)

			return err
		})
		if err != nil {
			return fmt.Errorf("solobill files verify: list files of tenant %s: %w", tenantID, err)
		}

		for _, file := range files {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("solobill files verify: %w", err)
			}
			checked++
			if err := verifyFile(ctx, objects, tenantID, file); err != nil {
				failed++
				logger.ErrorContext(ctx, "file integrity check failed",
					"file_id", file.ID, "tenant_id", tenantID, "reason", err.Error())
			}
		}
	}

	logger.InfoContext(ctx, "files verify finished", "checked", checked, "failed", failed)
	if failed > 0 {
		return exitCode{code: 1, err: fmt.Errorf("solobill files verify: %d of %d files failed, see the log for each file id", failed, checked)}
	}

	return nil
}

func verifyFile(ctx context.Context, objects objectReader, tenantID uuid.UUID, file storage.File) error {
	if want := objectstore.Key(tenantID, file.ID); file.TenantID != tenantID || file.ObjectKey != want {
		return fmt.Errorf("row has tenant %s and object key %q, want %s and %q", file.TenantID, file.ObjectKey, tenantID, want)
	}

	// The version the row recorded, so the scan checks what was uploaded,
	// not whatever a later overwrite put under the key.
	var (
		body io.ReadCloser
		err  error
	)
	if file.ObjectVersionID != nil && *file.ObjectVersionID != "" {
		body, _, err = objects.GetVersion(ctx, file.ObjectKey, *file.ObjectVersionID)
	} else {
		body, _, err = objects.Get(ctx, file.ObjectKey)
	}
	if errors.Is(err, storage.ErrNotFound) {
		return errors.New("object missing from the store")
	}
	if err != nil {
		return fmt.Errorf("read object: %w", err)
	}
	defer func() { _ = body.Close() }()

	return objectstore.Verify(body, file.SHA256, file.SizeBytes)
}
