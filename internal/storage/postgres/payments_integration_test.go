//go:build integration

// Issue #71: payments against a real PostgreSQL, through the restricted,
// NOBYPASSRLS application role, and through the admin role where only the
// triggers stand. Same reasons as integration_test.go's own build tag
// comment.
//
// Every statement that is expected to be refused runs in a scope or a
// transaction of its own, so an aborted transaction is never used again,
// and every refusal is asserted by its SQLSTATE or its storage sentinel.
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

// sqlStatePaymentCheckViolation is a CHECK constraint refusal. Prefixed so
// it never collides with the constants of other test files of this
// package.
const sqlStatePaymentCheckViolation = "23514"

// paymentEnv is issueEnv plus the restricted pool for raw statements, an
// issued invoice with an issued correction, a draft, and a second tenant
// with its own issued invoice.
type paymentEnv struct {
	issueEnv
	restrictedPool *pgxpool.Pool
	root           storage.Document
	correction     storage.Document
	draft          storage.Document
	other          issueEnv
	otherRoot      storage.Document
}

func newPaymentEnv(t *testing.T) paymentEnv {
	t.Helper()

	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed payments test")
	}
	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	restrictedPool, err := postgres.NewPool(ctx, withPoolMaxConns(t, setUpTestAppRole(ctx, t, adminPool, adminURL), 4))
	if err != nil {
		t.Fatalf("open restricted pool as %s: %v", testAppRole, err)
	}
	t.Cleanup(restrictedPool.Close)

	e := paymentEnv{
		issueEnv:       issueEnv{ctx: ctx, adminPool: adminPool, store: postgres.NewStore(restrictedPool)},
		restrictedPool: restrictedPool,
	}
	e.tenant, e.customer = e.seedTenant(t, "pay-a")

	if e.root, err = e.issue(e.issueEnv.draft(t, storage.Document{}).ID, e.issuer(), issueTestNow); err != nil {
		t.Fatalf("issue the original: %v", err)
	}
	seq := int32(0)
	correctionDraft := e.issueEnv.draft(t, storage.Document{
		Type:               storage.DocumentTypeCorrection,
		CorrectsDocumentID: &e.root.ID,
		CorrectionBaseID:   &e.root.ID,
		CorrectionBaseSeq:  &seq,
	})
	if e.correction, err = e.issue(correctionDraft.ID, e.issuer(), issueTestNow.Add(24*time.Hour)); err != nil {
		t.Fatalf("issue the correction: %v", err)
	}
	e.draft = e.issueEnv.draft(t, storage.Document{})

	e.other = e.issueEnv
	e.other.tenant, e.other.customer = e.seedTenant(t, "pay-b")
	if e.otherRoot, err = e.other.issue(e.other.draft(t, storage.Document{}).ID, e.other.issuer(), issueTestNow); err != nil {
		t.Fatalf("issue tenant B's invoice: %v", err)
	}

	return e
}

func paymentDay(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}

	return d
}

func (e paymentEnv) payment(documentID uuid.UUID, day string, amount int64) storage.Payment {
	return storage.Payment{
		TenantID:    e.tenant.ID,
		DocumentID:  documentID,
		ReceivedOn:  paymentDay(day),
		AmountCents: amount,
		Method:      storage.PaymentMethodBankTransfer,
	}
}

// create runs Create in a scope of its own and returns its error.
func (e paymentEnv) create(tenantID uuid.UUID, p storage.Payment) (storage.Payment, error) {
	var created storage.Payment
	err := e.store.WithTenant(e.ctx, tenantID, func(ctx context.Context) error {
		var err error
		created, err = e.store.Payments().Create(ctx, p)

		return err
	})

	return created, err
}

func (e paymentEnv) mustCreate(t *testing.T, p storage.Payment) storage.Payment {
	t.Helper()

	created, err := e.create(e.tenant.ID, p)
	if err != nil {
		t.Fatalf("Create(%+v) = %v", p, err)
	}

	return created
}

// rawErr runs one statement in a transaction of its own on pool, scoped
// to tenantID, and rolls it back.
func (e paymentEnv) rawErr(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID, sql string, args ...any) error {
	t.Helper()

	tx := beginScoped(e.ctx, t, pool, tenantID)
	defer func() { _ = tx.Rollback(e.ctx) }()
	_, err := tx.Exec(e.ctx, sql, args...)

	return err
}

func TestPaymentRepository(t *testing.T) {
	e := newPaymentEnv(t)
	repo := e.store.Payments()

	note := "first instalment"
	first := e.payment(e.root.ID, "2026-11-25", 10000)
	first.Note = &note
	first = e.mustCreate(t, first)
	if first.ID == uuid.Nil || first.TenantID != e.tenant.ID || first.DocumentID != e.root.ID ||
		!first.ReceivedOn.Equal(paymentDay("2026-11-25")) || first.Note == nil || *first.Note != note ||
		first.ReversesPaymentID != nil || first.Imported || first.CreatedAt.IsZero() {
		t.Fatalf("Create() = %+v", first)
	}
	early := e.mustCreate(t, e.payment(e.root.ID, "2026-11-21", 5000))
	refund := e.mustCreate(t, e.payment(e.root.ID, "2026-12-31", -1000))

	t.Run("ListByChain orders by received_on", func(t *testing.T) {
		e.inTenant(t, e.tenant.ID, func(ctx context.Context) error {
			got, err := repo.ListByChain(ctx, e.root.ID)
			if err != nil {
				return err
			}
			if len(got) != 3 || got[0].ID != early.ID || got[1].ID != first.ID || got[2].ID != refund.ID {
				t.Errorf("ListByChain() = %+v", got)
			}

			return nil
		})
	})

	t.Run("ListReceivedBetween includes both bounds", func(t *testing.T) {
		e.inTenant(t, e.tenant.ID, func(ctx context.Context) error {
			got, err := repo.ListReceivedBetween(ctx, paymentDay("2026-11-21"), paymentDay("2026-11-25"))
			if err != nil {
				return err
			}
			if len(got) != 2 || got[0].ID != early.ID || got[1].ID != first.ID {
				t.Errorf("ListReceivedBetween() = %+v", got)
			}

			return nil
		})
	})

	t.Run("only an issued invoice is a chain root", func(t *testing.T) {
		for name, id := range map[string]uuid.UUID{"a correction": e.correction.ID, "a draft": e.draft.ID, "no document": uuid.New()} {
			if _, err := e.create(e.tenant.ID, e.payment(id, "2026-11-25", 100)); !errors.Is(err, storage.ErrConflict) {
				t.Errorf("payment on %s: error = %v, want ErrConflict", name, err)
			}
		}
	})

	t.Run("a reversal mirrors one unreversed payment of its chain", func(t *testing.T) {
		reversal := func(of storage.Payment, day string, amount int64) storage.Payment {
			p := e.payment(e.root.ID, day, amount)
			p.ReversesPaymentID = &of.ID

			return p
		}

		// Each refusal goes through the repository, which maps the
		// trigger's class 23000 to a bare storage.ErrConflict: the SQLSTATE
		// itself is asserted on raw statements below.
		for name, p := range map[string]storage.Payment{
			"another amount":                 reversal(first, "2026-11-25", -9999),
			"the same sign":                  reversal(first, "2026-11-25", 10000),
			"an earlier date":                reversal(first, "2026-11-24", -10000),
			"a later date":                   reversal(first, "2026-11-26", -10000),
			"a refund reversed by a refund":  reversal(refund, "2026-12-31", -1000),
			"a refund reversed another date": reversal(refund, "2027-01-02", 1000),
		} {
			if _, err := e.create(e.tenant.ID, p); !errors.Is(err, storage.ErrConflict) {
				t.Errorf("%s: error = %v, want ErrConflict", name, err)
			}
		}
		err := e.rawErr(t, e.restrictedPool, e.tenant.ID, `INSERT INTO payments (tenant_id, document_id, received_on,
			amount_cents, method, reverses_payment_id) VALUES ($1, $2, '2026-11-26', -10000, 'cash', $3)`,
			e.tenant.ID, e.root.ID, first.ID)
		requireSQLState(t, err, pgIntegrityConstraintViolationClass, "raw reversal on another day")

		// A refund recorded by mistake is reversed by a positive row on
		// the refund's own day.
		undo := e.mustCreate(t, reversal(refund, "2026-12-31", 1000))
		if undo.AmountCents != 1000 || undo.ReversesPaymentID == nil || *undo.ReversesPaymentID != refund.ID {
			t.Fatalf("reversal of the refund = %+v", undo)
		}

		reversed := e.mustCreate(t, reversal(first, "2026-11-25", -10000))
		if reversed.ReversesPaymentID == nil || *reversed.ReversesPaymentID != first.ID {
			t.Fatalf("reversal = %+v", reversed)
		}
		if _, err := e.create(e.tenant.ID, reversal(first, "2026-11-25", -10000)); !errors.Is(err, storage.ErrConflict) {
			t.Errorf("second reversal: error = %v, want ErrConflict", err)
		}
		if _, err := e.create(e.tenant.ID, reversal(reversed, "2026-11-25", 10000)); !errors.Is(err, storage.ErrConflict) {
			t.Errorf("reversal of a reversal: error = %v, want ErrConflict", err)
		}
		// The raw second reversal passes the trigger and meets the unique
		// index of payments_reversed_once.
		err = e.rawErr(t, e.restrictedPool, e.tenant.ID, `INSERT INTO payments (tenant_id, document_id, received_on,
			amount_cents, method, reverses_payment_id) VALUES ($1, $2, '2026-11-25', -10000, 'cash', $3)`,
			e.tenant.ID, e.root.ID, first.ID)
		requireSQLState(t, err, "23505", "raw second reversal")
	})

	t.Run("another tenant sees and touches nothing", func(t *testing.T) {
		e.inTenant(t, e.other.tenant.ID, func(ctx context.Context) error {
			got, err := repo.ListByChain(ctx, e.root.ID)
			if err != nil {
				return err
			}
			if len(got) != 0 {
				t.Errorf("tenant B lists %+v of tenant A's chain", got)
			}
			got, err = repo.ListReceivedBetween(ctx, paymentDay("2000-01-01"), paymentDay("2099-12-31"))
			if err != nil {
				return err
			}
			if len(got) != 0 {
				t.Errorf("tenant B lists %+v", got)
			}

			return nil
		})

		// A row of tenant A written in B's scope, raw, so the SQLSTATE is
		// visible. In PostgreSQL's ExecInsert the BEFORE ROW triggers run
		// before the row level security WITH CHECK, which runs before the
		// CHECK constraints and the unique indexes. payments_check_insert
		// looks the invoice up under B's row level security, finds none and
		// raises first, so its 23000 comes back, not the policy's 42501.
		err := e.rawErr(t, e.restrictedPool, e.other.tenant.ID, `INSERT INTO payments (tenant_id, document_id, received_on,
			amount_cents, method) VALUES ($1, $2, '2026-11-25', 100, 'cash')`, e.tenant.ID, e.root.ID)
		requireSQLState(t, err, pgIntegrityConstraintViolationClass, "tenant B writing a payment of tenant A")
		// Through the repository the trigger's 23000 is storage.ErrConflict.
		if _, err := e.create(e.other.tenant.ID, e.payment(e.root.ID, "2026-11-25", 100)); !errors.Is(err, storage.ErrConflict) {
			t.Errorf("tenant B writing a payment of tenant A through the repository: error = %v, want ErrConflict", err)
		}

		// B's own row naming A's invoice: the trigger does not see it.
		p := e.payment(e.root.ID, "2026-11-25", 100)
		p.TenantID = e.other.tenant.ID
		if _, err := e.create(e.other.tenant.ID, p); !errors.Is(err, storage.ErrConflict) {
			t.Errorf("tenant B paying tenant A's invoice: error = %v, want ErrConflict", err)
		}
		err = e.rawErr(t, e.restrictedPool, e.other.tenant.ID, `INSERT INTO payments (tenant_id, document_id, received_on,
			amount_cents, method) VALUES ($1, $2, '2026-11-25', 100, 'cash')`, e.other.tenant.ID, e.root.ID)
		requireSQLState(t, err, pgIntegrityConstraintViolationClass, "raw payment of tenant B on tenant A's invoice")

		// B's own reversal naming A's payment.
		p = e.payment(e.otherRoot.ID, "2026-11-25", -5000)
		p.TenantID = e.other.tenant.ID
		p.ReversesPaymentID = &early.ID
		if _, err := e.create(e.other.tenant.ID, p); !errors.Is(err, storage.ErrConflict) {
			t.Errorf("tenant B reversing tenant A's payment: error = %v, want ErrConflict", err)
		}
	})

	t.Run("the table checks its own shape", func(t *testing.T) {
		for name, sql := range map[string]string{
			"zero amount":    `INSERT INTO payments (tenant_id, document_id, received_on, amount_cents, method) VALUES ($1, $2, '2026-11-25', 0, 'cash')`,
			"unknown method": `INSERT INTO payments (tenant_id, document_id, received_on, amount_cents, method) VALUES ($1, $2, '2026-11-25', 100, 'wire')`,
			"over the bound": `INSERT INTO payments (tenant_id, document_id, received_on, amount_cents, method) VALUES ($1, $2, '2026-11-25', 100000000001, 'cash')`,
		} {
			requireSQLState(t, e.rawErr(t, e.restrictedPool, e.tenant.ID, sql, e.tenant.ID, e.root.ID), sqlStatePaymentCheckViolation, name)
		}
	})
}

// TestPaymentsAreAppendOnly: the application role has no UPDATE, DELETE or
// TRUNCATE grant, and payments_block_mutation and payments_block_truncate
// refuse the same for the admin role, which the grants do not limit.
func TestPaymentsAreAppendOnly(t *testing.T) {
	e := newPaymentEnv(t)
	paid := e.mustCreate(t, e.payment(e.root.ID, "2026-11-25", 10000))

	for name, sql := range map[string]string{
		"UPDATE":   `UPDATE payments SET amount_cents = 1 WHERE id = $1`,
		"DELETE":   `DELETE FROM payments WHERE id = $1`,
		"TRUNCATE": `TRUNCATE payments`,
	} {
		args := []any{paid.ID}
		if name == "TRUNCATE" {
			args = nil
		}
		requireSQLState(t, e.rawErr(t, e.restrictedPool, e.tenant.ID, sql, args...), sqlStateInsufficientPrivilege,
			"the application role's "+name)

		err := e.rawErr(t, e.adminPool, e.tenant.ID, sql, args...)
		var pgErr *pgconn.PgError
		if name == "TRUNCATE" {
			// Postgres itself refuses a plain TRUNCATE (0A000) once a
			// later table references payments by foreign key; the trigger
			// refuses it before that. CASCADE below always reaches the
			// trigger.
			if !errors.As(err, &pgErr) || (pgErr.Code != "0A000" && pgErr.Code != pgIntegrityConstraintViolationClass) {
				t.Errorf("the admin role's TRUNCATE: error = %v, want 0A000 or 23000", err)
			}

			continue
		}
		requireSQLState(t, err, pgIntegrityConstraintViolationClass, "the admin role's "+name)
		if errors.As(err, &pgErr) &&
			(!strings.Contains(pgErr.Message, "append-only") || !strings.Contains(pgErr.Message, "immutable")) {
			t.Errorf("%s message = %q, want payments_block_mutation's own", name, pgErr.Message)
		}
	}

	// TRUNCATE ... CASCADE reaches the trigger as well.
	requireSQLState(t, e.rawErr(t, e.adminPool, e.tenant.ID, `TRUNCATE payments CASCADE`),
		pgIntegrityConstraintViolationClass, "the admin role's TRUNCATE CASCADE")

	e.inTenant(t, e.tenant.ID, func(ctx context.Context) error {
		got, err := e.store.Payments().ListByChain(ctx, e.root.ID)
		if err != nil {
			return err
		}
		if len(got) != 1 || got[0].AmountCents != 10000 {
			t.Errorf("payments after the refusals = %+v, want the one unchanged row", got)
		}

		return nil
	})
}

// inTenant runs fn in a WithTenant scope and fails the test on an error.
func (e paymentEnv) inTenant(t *testing.T, tenantID uuid.UUID, fn func(ctx context.Context) error) {
	t.Helper()

	if err := e.store.WithTenant(e.ctx, tenantID, fn); err != nil {
		t.Fatal(err)
	}
}
