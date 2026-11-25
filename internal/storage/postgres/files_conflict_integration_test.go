//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres"
)

// TestFileCreateConflictAbortsTheTransaction pins what the PDF route
// relies on when two first downloads of one document race to store its
// PDF under the same derived id: the second Create is storage.ErrConflict,
// the transaction it ran in is aborted so nothing more can be read in
// it, and a new transaction reads the row the first one stored.
func TestFileCreateConflictAbortsTheTransaction(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed files conflict test")
	}

	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	restrictedPool, err := postgres.NewPool(ctx, setUpTestAppRole(ctx, t, adminPool, adminURL))
	if err != nil {
		t.Fatalf("open restricted pool as %s: %v", testAppRole, err)
	}
	t.Cleanup(restrictedPool.Close)

	store := postgres.NewStore(restrictedPool)
	tenant, err := store.Tenants().Create(ctx, storage.Tenant{Slug: "files-race-" + uuid.NewString(), Name: "Files race"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	id := uuid.New()
	file := storage.File{
		ID:               id,
		TenantID:         tenant.ID,
		Purpose:          storage.FilePurposeDocumentPDF,
		ObjectKey:        tenant.ID.String() + "/" + id.String(),
		SHA256:           testFileSHA256,
		SizeBytes:        4,
		ContentType:      "application/pdf",
		OriginalFilename: "RE-20260001.pdf",
		UploadedAt:       time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC),
	}
	if err := store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
		_, err := store.Files().Create(ctx, file)

		return err
	}); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	var sameTxRead error
	err = store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
		loser := file
		loser.OriginalFilename = "loser.pdf"
		if _, err := store.Files().Create(ctx, loser); !errors.Is(err, storage.ErrConflict) {
			t.Errorf("second Create error = %v, want storage.ErrConflict", err)
		}
		_, sameTxRead = store.Files().FindByID(ctx, id)

		return nil
	})
	if sameTxRead == nil {
		t.Error("FindByID in the transaction of the failed Create succeeded, want the aborted transaction's error")
	}
	_ = err // the commit of the aborted transaction fails or rolls back; either way nothing was written

	if err := store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
		got, err := store.Files().FindByID(ctx, id)
		if err != nil {
			return err
		}
		if got.OriginalFilename != file.OriginalFilename {
			t.Errorf("stored row = %q, want the first Create's", got.OriginalFilename)
		}

		return nil
	}); err != nil {
		t.Fatalf("FindByID in a new transaction: %v", err)
	}
}
