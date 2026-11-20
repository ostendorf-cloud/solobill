//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres"
)

// testFileSHA256 is the SHA-256 of "test", a valid lowercase hex value.
const testFileSHA256 = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

// TestFileRepository proves the files table end to end through the
// restricted role: create and read back, the duplicate lookup, tenant
// isolation, the object key shape, and that the role can neither update
// nor delete a row.
func TestFileRepository(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed files test")
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

	tenantA, err := store.Tenants().Create(ctx, storage.Tenant{Slug: "files-a-" + uuid.NewString(), Name: "Files A"})
	if err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	tenantB, err := store.Tenants().Create(ctx, storage.Tenant{Slug: "files-b-" + uuid.NewString(), Name: "Files B"})
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	newFile := func(tenantID uuid.UUID) storage.File {
		id := uuid.New()

		return storage.File{
			ID:               id,
			TenantID:         tenantID,
			Purpose:          storage.FilePurposeReceipt,
			ObjectKey:        tenantID.String() + "/" + id.String(),
			SHA256:           testFileSHA256,
			SizeBytes:        4,
			ContentType:      "application/pdf",
			OriginalFilename: "rechnung.pdf",
			UploadedAt:       time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC),
		}
	}

	fileA := newFile(tenantA.ID)
	if err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
		got, err := store.Files().Create(ctx, fileA)
		if err != nil {
			return err
		}
		if got.ID != fileA.ID || got.SHA256 != fileA.SHA256 || got.ObjectKey != fileA.ObjectKey {
			t.Errorf("Create() = %+v, want %+v", got, fileA)
		}

		return nil
	}); err != nil {
		t.Fatalf("create file under tenant A: %v", err)
	}

	t.Run("read back and duplicate lookup", func(t *testing.T) {
		if err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
			got, err := store.Files().FindByID(ctx, fileA.ID)
			if err != nil {
				return err
			}
			if !got.UploadedAt.Equal(fileA.UploadedAt) || got.OriginalFilename != "rechnung.pdf" {
				t.Errorf("FindByID() = %+v", got)
			}
			dups, err := store.Files().FindBySHA256(ctx, testFileSHA256)
			if err != nil {
				return err
			}
			if len(dups) != 1 || dups[0].ID != fileA.ID {
				t.Errorf("FindBySHA256() = %v, want only file A", dups)
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("another tenant cannot see it", func(t *testing.T) {
		if err := store.WithTenant(ctx, tenantB.ID, func(ctx context.Context) error {
			if _, err := store.Files().FindByID(ctx, fileA.ID); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("FindByID() under tenant B error = %v, want ErrNotFound", err)
			}
			dups, err := store.Files().FindBySHA256(ctx, testFileSHA256)
			if err != nil {
				return err
			}
			if len(dups) != 0 {
				t.Errorf("FindBySHA256() under tenant B = %v, want none", dups)
			}
			list, err := store.ListFiles(ctx)
			if err != nil {
				return err
			}
			for _, f := range list {
				if f.ID == fileA.ID {
					t.Error("ListFiles() under tenant B returned file A")
				}
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("a row for another tenant cannot be inserted", func(t *testing.T) {
		err := store.WithTenant(ctx, tenantB.ID, func(ctx context.Context) error {
			_, err := store.Files().Create(ctx, newFile(tenantA.ID))

			return err
		})
		if err == nil {
			t.Fatal("Create() of a tenant A row under tenant B succeeded, want a row level security refusal")
		}
	})

	t.Run("an object key not derived from tenant and id is refused", func(t *testing.T) {
		bad := newFile(tenantA.ID)
		bad.ObjectKey = tenantA.ID.String() + "/../rechnung.pdf"
		err := store.WithTenant(ctx, tenantA.ID, func(ctx context.Context) error {
			_, err := store.Files().Create(ctx, bad)

			return err
		})
		if err == nil || !strings.Contains(err.Error(), "files_object_key_shape") {
			t.Fatalf("Create() with a free object key error = %v, want files_object_key_shape", err)
		}
	})

	t.Run("the app role can neither update nor delete", func(t *testing.T) {
		for _, stmt := range []string{
			`UPDATE files SET sha256 = repeat('0', 64) WHERE id = $1`,
			`DELETE FROM files WHERE id = $1`,
		} {
			// The privilege check runs before row level security, so no
			// tenant scope is needed to see the refusal.
			_, err := restrictedPool.Exec(ctx, stmt, fileA.ID)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
				t.Errorf("%q error = %v, want insufficient_privilege (42501)", stmt, err)
			}
		}
	})

	t.Run("the trigger refuses even a privileged role", func(t *testing.T) {
		_, err := adminPool.Exec(ctx, `UPDATE files SET original_filename = 'x' WHERE id = $1`, fileA.ID)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Errorf("admin UPDATE error = %v, want the files_block_mutation refusal", err)
		}
	})

	t.Run("tenant ids include both tenants", func(t *testing.T) {
		ids, err := store.TenantIDs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[uuid.UUID]bool{}
		for _, id := range ids {
			seen[id] = true
		}
		if !seen[tenantA.ID] || !seen[tenantB.ID] {
			t.Errorf("TenantIDs() = %v, want both test tenants", ids)
		}
	})
}
