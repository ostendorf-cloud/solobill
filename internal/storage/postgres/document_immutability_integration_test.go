//go:build integration

// Same reasons as integration_test.go's own build tag comment: needs a
// real, reachable Postgres, excluded from a plain "go test ./..." by
// the tag above, and DATABASE_URL still checked at runtime, t.Skip()ing
// without it.
package postgres_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres"
)

// pgIntegrityConstraintViolationClass mirrors
// internal/storage/postgres's own unexported constant of the same
// value: db/migrations' documents_block_issued_mutation and
// document_lines_block_issued_document_mutation trigger functions raise
// this class (USING ERRCODE = 'integrity_constraint_violation'), not
// pgUniqueViolation and not row level security's own
// "insufficient_privilege"/"42501" class a WITH CHECK rejection raises;
// see the subtests below for why telling these apart is the point, not
// an afterthought.
const pgIntegrityConstraintViolationClass = "23000"

// pgInsufficientPrivilegeClass is the Postgres error class a missing
// GRANT raises ("permission denied for table ..."), checked before any
// trigger on that table ever runs: a privilege check happens once, at
// statement start, independent of which rows the statement would touch
// or what state they are in. document_lines has no UPDATE grant at all
// for solobill_app (see docker-compose.yaml's "grant-app-role" and this
// package's own setUpTestAppRole), so a raw UPDATE against it always
// fails this way, for a draft's own line exactly as much as for an
// issued one; see the subtest below that pins this apart from
// pgIntegrityConstraintViolationClass.
const pgInsufficientPrivilegeClass = "42501"

// TestDocumentImmutability_RawSQL is issue #15's own requirement: a
// repository method that refuses a write is not enough if raw SQL run
// through the same, otherwise-trusted connection can still do it. Every
// subtest here goes around storage.DocumentRepository entirely, straight
// at documents and document_lines through restrictedPool, the same role
// cmd/solobill itself connects as, to prove db/migrations'
// documents_block_issued_mutation and
// document_lines_block_issued_document_mutation triggers are what
// actually refuse the write, not a convention only this package's own
// Go code happens to follow.
//
// Each assertion below checks the rejection's own Postgres error code
// and message, not merely that an error occurred: this table's
// row-level-security policy would itself refuse an UPDATE that tried to
// move tenant_id to another tenant (a completely different, unrelated
// reason to fail), and a plain CHECK constraint violation on an
// unrelated column would also just be "an error". Only
// pgIntegrityConstraintViolationClass with the trigger's own distinctive
// message text pins the rejection to the immutability trigger itself,
// per the coordinator's own note on telling a trigger's rejection apart
// from an unrelated one.
func TestDocumentImmutability_RawSQL(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed immutability test")
	}

	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	restrictedURL := setUpTestAppRole(ctx, t, adminPool, adminURL)

	restrictedPool, err := postgres.NewPool(ctx, restrictedURL)
	if err != nil {
		t.Fatalf("open restricted pool as %s: %v", testAppRole, err)
	}
	t.Cleanup(restrictedPool.Close)

	store := postgres.NewStore(restrictedPool)

	tenant, err := store.Tenants().Create(ctx, storage.Tenant{Slug: "tenant-immutable-" + uuid.NewString(), Name: "Tenant Immutable"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	customer, err := createDocumentTestCustomer(ctx, t, store, tenant.ID)
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}

	var issued storage.Document

	if err := store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
		draft, err := store.Documents().Create(ctx, storage.Document{
			TenantID:   tenant.ID,
			Type:       storage.DocumentTypeInvoice,
			CustomerID: customer.ID,
			VATRegime:  storage.VATRegimeStandard,
		}, []storage.DocumentLine{
			{Description: "Line", QuantityThousandths: 1000, UnitPriceCents: 1000, Unit: "piece", VATRatePermyriad: 1900},
		})
		if err != nil {
			return err
		}

		issued, err = issueDraftForTest(ctx, store, draft.ID, storage.DocumentTypeInvoice, 2028)

		return err
	}); err != nil {
		t.Fatalf("create and issue a document: %v", err)
	}

	issuedLineID := issued.Lines[0].ID

	t.Run("a raw UPDATE of the issued document is rejected by the trigger, not by something else", func(t *testing.T) {
		tx := beginScoped(ctx, t, restrictedPool, tenant.ID)
		defer func() { _ = tx.Rollback(ctx) }()

		_, err := tx.Exec(ctx, `UPDATE documents SET vat_regime = 'kleinunternehmer' WHERE id = $1`, issued.ID)

		assertBlockedByImmutabilityTrigger(t, err, "documents")
	})

	t.Run("a raw DELETE of the issued document is rejected by the trigger, not by something else", func(t *testing.T) {
		tx := beginScoped(ctx, t, restrictedPool, tenant.ID)
		defer func() { _ = tx.Rollback(ctx) }()

		_, err := tx.Exec(ctx, `DELETE FROM documents WHERE id = $1`, issued.ID)

		assertBlockedByImmutabilityTrigger(t, err, "documents")
	})

	t.Run("a raw UPDATE of a document line is refused by the grant, not by the trigger", func(t *testing.T) {
		tx := beginScoped(ctx, t, restrictedPool, tenant.ID)
		defer func() { _ = tx.Rollback(ctx) }()

		_, err := tx.Exec(ctx, `UPDATE document_lines SET description = 'tampered' WHERE id = $1`, issuedLineID)

		assertRefusedByMissingGrant(t, err, "document_lines")
	})

	t.Run("a raw DELETE of an issued document's line is rejected by the trigger, not by something else", func(t *testing.T) {
		tx := beginScoped(ctx, t, restrictedPool, tenant.ID)
		defer func() { _ = tx.Rollback(ctx) }()

		_, err := tx.Exec(ctx, `DELETE FROM document_lines WHERE id = $1`, issuedLineID)

		assertBlockedByImmutabilityTrigger(t, err, "document_lines")
	})

	t.Run("a raw INSERT of a new line onto the issued document is rejected by the trigger, not by something else", func(t *testing.T) {
		tx := beginScoped(ctx, t, restrictedPool, tenant.ID)
		defer func() { _ = tx.Rollback(ctx) }()

		_, err := tx.Exec(ctx,
			`INSERT INTO document_lines (tenant_id, document_id, position, description, quantity_thousandths, unit_price_cents, unit, vat_rate_permyriad)
			 VALUES ($1, $2, 999, 'Sneaked in', 1000, 500, 'piece', 1900)`,
			tenant.ID, issued.ID)

		assertBlockedByImmutabilityTrigger(t, err, "document_lines")
	})

	t.Run("a raw UPDATE moving an issued document's line onto a draft is rejected by the trigger, checking both parents", func(t *testing.T) {
		// solobill_app itself can never reach this path: document_lines'
		// own grant withholds UPDATE entirely (see the subtest above), so
		// this only matters for a role that owns document_lines (and
		// documents, for the trigger's own lookup) outright, exactly the
		// shape charts/solobill/README.md's "One role, or two" documents
		// as this chart's own default deployment, where the role the
		// Deployment connects as is also the role the migration Job ran
		// as and therefore owns every table db/migrations creates.
		// testOwnerRole, made the owner of both tables for this subtest's
		// duration via makeTemporaryOwner, reproduces that shape: it
		// holds every privilege ownership itself carries, UPDATE
		// included, with no GRANT needed, so the trigger is the only
		// thing left that can refuse this write.
		//
		// Before this fix, document_lines_block_issued_document_mutation
		// resolved its target document via
		// COALESCE(NEW.document_id, OLD.document_id), which, because
		// document_id is NOT NULL, always took NEW.document_id on an
		// UPDATE and never consulted OLD.document_id: moving a line onto
		// a draft checked only the draft, found it not issued, and let
		// the move through, silently dropping a line from an issued
		// document. This subtest pins the fixed behaviour: the UPDATE
		// must be refused because the line's OLD parent (the issued
		// document) is issued, even though its NEW parent is a draft.
		ownerURL := setUpTestOwnerRole(ctx, t, adminPool, adminURL)
		makeTemporaryOwner(ctx, t, adminPool, "documents", testOwnerRole)
		makeTemporaryOwner(ctx, t, adminPool, "document_lines", testOwnerRole)

		ownerPool, err := postgres.NewPool(ctx, ownerURL)
		if err != nil {
			t.Fatalf("open pool as %s: %v", testOwnerRole, err)
		}
		defer ownerPool.Close()

		var draft storage.Document

		if err := store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
			var err error
			draft, err = store.Documents().Create(ctx, storage.Document{
				TenantID:   tenant.ID,
				Type:       storage.DocumentTypeInvoice,
				CustomerID: customer.ID,
				VATRegime:  storage.VATRegimeStandard,
			}, nil)

			return err
		}); err != nil {
			t.Fatalf("create a second draft to move the line onto: %v", err)
		}

		tx := beginScoped(ctx, t, ownerPool, tenant.ID)
		defer func() { _ = tx.Rollback(ctx) }()

		_, err = tx.Exec(ctx, `UPDATE document_lines SET document_id = $1 WHERE id = $2`, draft.ID, issuedLineID)

		assertBlockedByImmutabilityTrigger(t, err, "document_lines")
	})

	t.Run("a draft document's line can still be deleted and reinserted: the trigger only blocks an issued document", func(t *testing.T) {
		var draft storage.Document

		if err := store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
			var err error
			draft, err = store.Documents().Create(ctx, storage.Document{
				TenantID:   tenant.ID,
				Type:       storage.DocumentTypeInvoice,
				CustomerID: customer.ID,
				VATRegime:  storage.VATRegimeStandard,
			}, []storage.DocumentLine{
				{Description: "Draft line", QuantityThousandths: 1000, UnitPriceCents: 1000, Unit: "piece", VATRatePermyriad: 1900},
			})

			return err
		}); err != nil {
			t.Fatalf("create draft: %v", err)
		}

		tx := beginScoped(ctx, t, restrictedPool, tenant.ID)
		defer func() { _ = tx.Rollback(ctx) }()

		// This is documentRepository.Update's own path (see
		// internal/storage/postgres/documents.go): it never raw-UPDATEs
		// an existing line, it deletes every line of the document and
		// recreates the caller's current set. Run directly here, against
		// the restricted role, it proves that is also the only thing
		// solobill_app itself can actually do to a draft's own line, not
		// merely a convention the Go code happens to follow.
		if _, err := tx.Exec(ctx, `DELETE FROM document_lines WHERE id = $1`, draft.Lines[0].ID); err != nil {
			t.Fatalf("DELETE a draft's own line: %v, want it to succeed", err)
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO document_lines (tenant_id, document_id, position, description, quantity_thousandths, unit_price_cents, unit, vat_rate_permyriad)
			 VALUES ($1, $2, 0, 'Replacement line', 1000, 1000, 'piece', 1900)`,
			tenant.ID, draft.ID); err != nil {
			t.Fatalf("INSERT a replacement line on the same still-draft document: %v, want it to succeed", err)
		}
	})
}

// beginScoped opens a transaction on pool and scopes it to tenantID,
// the same set_config('app.tenant_id', ..., true) every repository
// method's own transaction carries; see postgres.Store.WithTenant.
func beginScoped(ctx context.Context, t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID) pgx.Tx {
	t.Helper()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID.String()); err != nil {
		t.Fatalf("set_config: %v", err)
	}

	return tx
}

// assertBlockedByImmutabilityTrigger fails t unless err is exactly the
// rejection db/migrations' documents_block_issued_mutation or
// document_lines_block_issued_document_mutation trigger raises against
// tableName: the right Postgres error class
// (pgIntegrityConstraintViolationClass, not a unique violation, not row
// level security's own distinct rejection), and the trigger's own
// message text, naming "issued" and "immutable", not some other
// integrity_constraint_violation this schema might one day also raise
// under that same generic class.
func assertBlockedByImmutabilityTrigger(t *testing.T, err error, tableName string) {
	t.Helper()

	if err == nil {
		t.Fatalf("write against an issued %s succeeded, want it rejected by the immutability trigger", tableName)
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("err = %v, want a *pgconn.PgError", err)
	}

	if pgErr.Code != pgIntegrityConstraintViolationClass {
		t.Fatalf("pgErr.Code = %q, want %q (the immutability trigger's own class, not a different rejection such as a unique violation %q or row level security's own %q)",
			pgErr.Code, pgIntegrityConstraintViolationClass, "23505", "42501")
	}

	if !strings.Contains(pgErr.Message, "issued") || !strings.Contains(pgErr.Message, "immutable") {
		t.Fatalf("pgErr.Message = %q, want it to name the issued document as immutable: this must be the trigger's own RAISE EXCEPTION, not some other integrity_constraint_violation", pgErr.Message)
	}
}

// assertRefusedByMissingGrant fails t unless err is exactly the
// rejection a missing table privilege raises against tableName:
// pgInsufficientPrivilegeClass with Postgres' own "permission denied"
// wording, not pgIntegrityConstraintViolationClass. solobill_app holds
// no UPDATE grant on document_lines at all (see docker-compose.yaml's
// "grant-app-role" and this package's own setUpTestAppRole), so this is
// what a raw UPDATE against it must always produce, regardless of
// whether the targeted line's document happens to be a draft or
// issued: the rejection has to come from the grant itself, before
// document_lines_block_issued_document_mutation ever gets a chance to
// run.
func assertRefusedByMissingGrant(t *testing.T, err error, tableName string) {
	t.Helper()

	if err == nil {
		t.Fatalf("write against %s succeeded, want it refused by the missing grant", tableName)
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("err = %v, want a *pgconn.PgError", err)
	}

	if pgErr.Code != pgInsufficientPrivilegeClass {
		t.Fatalf("pgErr.Code = %q, want %q (a missing grant's own class, not the immutability trigger's %q)",
			pgErr.Code, pgInsufficientPrivilegeClass, pgIntegrityConstraintViolationClass)
	}

	if !strings.Contains(pgErr.Message, "permission denied") {
		t.Fatalf("pgErr.Message = %q, want it to name a permission denial: this must be the grant's own rejection, not the trigger's", pgErr.Message)
	}
}
