//go:build integration

// Same reasons as integration_test.go's own build tag comment: needs a
// real, reachable Postgres, excluded from a plain "go test ./..." by the
// tag above, and DATABASE_URL still checked at runtime, t.Skip()ing
// without it.
package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres"
)

// withPoolMaxConns appends pgxpool's own "pool_max_conns" connection
// string parameter to rawURL, overriding pgxpool.New's default of
// max(4, runtime.NumCPU()): see
// TestDocumentNumbering_ConcurrentIssueIsGapless's own doc comment for
// why this test needs more than that default actually gives it on this
// repository's 2-vCPU runner.
func withPoolMaxConns(t *testing.T, rawURL string, n int) string {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse connection string: %v", err)
	}

	query := parsed.Query()
	query.Set("pool_max_conns", fmt.Sprintf("%d", n))
	parsed.RawQuery = query.Encode()

	return parsed.String()
}

// createDocumentTestCustomer creates a minimal, active customer under
// tenantID, the one piece of data every document test in this package
// needs before it can create a document at all (documents.customer_id
// is NOT NULL; see db/migrations). Shared by every *_test.go file in
// this package that exercises storage.DocumentRepository.
func createDocumentTestCustomer(ctx context.Context, t *testing.T, store *postgres.Store, tenantID uuid.UUID) (storage.Customer, error) {
	t.Helper()

	var customer storage.Customer

	err := store.WithTenant(ctx, tenantID, func(ctx context.Context) error {
		var err error
		customer, err = store.Customers().Create(ctx, storage.Customer{
			TenantID: tenantID,
			Name:     "Test Customer " + uuid.NewString(),
		})

		return err
	})

	return customer, err
}

// nowForTest returns the current time truncated to microsecond
// precision, matching what a round trip through Postgres' own
// timestamptz actually preserves: comparing a value read back against
// one kept only in Go would otherwise risk a spurious mismatch on
// nanosecond-level precision Postgres never stores.
func nowForTest() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

// testFrozenPayload returns a fixed payload and the hex-encoded SHA-256
// of its own bytes, computed here rather than hardcoded, so the two can
// never drift from each other. Every Issue call in this package's tests
// uses this rather than its own literal pair: db/migrations'
// documents_frozen_payload_sha256_matches CHECK (see
// 20261113160000_create_documents.sql) now rejects any payload and hash
// that do not actually match.
func testFrozenPayload() ([]byte, string) {
	payload := []byte(`{"total":1190}`)
	sum := sha256.Sum256(payload)

	return payload, hex.EncodeToString(sum[:])
}

// TestDocumentNumbering_ConcurrentIssueIsGapless is issue #15's own
// deliverable: a single-threaded loop allocating numbers one at a time
// would pass trivially and prove nothing about the thing that actually
// breaks under concurrency. This test issues numberOfGoroutines
// documents concurrently, every one of them for the same tenant, the
// same storage.DocumentTypeInvoice and the same year, which is exactly
// the (tenant, type, year) key IncrementDocumentNumber's own
// UPDATE ... RETURNING serializes on (see
// internal/storage/postgres/numbering.go and
// queries/document_numbers.sql), and asserts the resulting numbers are
// exactly the contiguous range [1, numberOfGoroutines], with no gap and
// no repeat: if that serialisation were missing or wrong, two goroutines
// could read the same last_number before either commits and both
// allocate the same number, or a crashed/raced allocation could leave a
// gap.
//
// numberOfGoroutines concurrent goroutines are not numberOfGoroutines
// concurrent transactions by themselves: restrictedPool below is given
// an explicit MaxConns of numberOfGoroutines (pgxpool's own default,
// max(4, runtime.NumCPU()), would otherwise cap this test's own
// connections at 4 on the 2-vCPU runner this repository's jobs run on,
// so sixteen of the twenty goroutines would simply queue for a
// connection rather than ever reaching Postgres concurrently at all).
// With MaxConns raised, every goroutine's Issue call can actually be in
// flight against Postgres at the same time, which is what makes this
// test's own premise - the locking is what prevents a collision, not
// an accident of how few of them ever overlapped - true.
func TestDocumentNumbering_ConcurrentIssueIsGapless(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed gapless numbering test")
	}

	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	const (
		numberOfGoroutines = 20
		year               = 2030 // far from any other test's own year, avoiding collision
	)

	restrictedURL := setUpTestAppRole(ctx, t, adminPool, adminURL)

	restrictedPool, err := postgres.NewPool(ctx, withPoolMaxConns(t, restrictedURL, numberOfGoroutines))
	if err != nil {
		t.Fatalf("open restricted pool as %s: %v", testAppRole, err)
	}
	t.Cleanup(restrictedPool.Close)

	store := postgres.NewStore(restrictedPool)

	tenant, err := store.Tenants().Create(ctx, storage.Tenant{Slug: "tenant-gapless-" + uuid.NewString(), Name: "Tenant Gapless"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	customer, err := createDocumentTestCustomer(ctx, t, store, tenant.ID)
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}

	numbers := make([]int64, numberOfGoroutines)
	errs := make([]error, numberOfGoroutines)

	var wg sync.WaitGroup

	for i := 0; i < numberOfGoroutines; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			errs[i] = store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
				document, err := store.Documents().Create(ctx, storage.Document{
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

				payload, payloadHash := testFrozenPayload()

				issued, err := store.Documents().Issue(ctx, document.ID, storage.DocumentTypeInvoice, year, nowForTest(), payload, payloadHash)
				if err != nil {
					return err
				}

				numbers[i] = *issued.Number

				return nil
			})
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: Issue failed: %v", i, err)
		}
	}

	seen := make(map[int64]int, numberOfGoroutines)
	for _, n := range numbers {
		seen[n]++
	}

	for n := int64(1); n <= numberOfGoroutines; n++ {
		switch count := seen[n]; count {
		case 1:
			delete(seen, n)
		case 0:
			t.Errorf("number %d was never issued: there is a gap in [1, %d]", n, numberOfGoroutines)
		default:
			t.Errorf("number %d was issued %d times, want exactly once", n, count)
		}
	}

	for n, count := range seen {
		t.Errorf("number %d was issued %d time(s) but is outside the expected contiguous range [1, %d]", n, count, numberOfGoroutines)
	}

	var finalCounter int64

	if err := store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
		var err error
		finalCounter, err = store.DocumentNumbers().Current(ctx, storage.DocumentTypeInvoice, year)

		return err
	}); err != nil {
		t.Fatalf("read final counter: %v", err)
	}

	if finalCounter != numberOfGoroutines {
		t.Errorf("final document_numbers counter = %d, want %d (every goroutine's own allocation counted exactly once)", finalCounter, numberOfGoroutines)
	}
}

// TestDocumentNumbering_RollbackLeavesCounterUnchanged is the other half
// of issue #15's own numbering requirement: a transaction that
// allocates a number and then rolls back must leave document_numbers
// exactly where it found it, so the number it almost issued is free for
// the very next successful call, not skipped.
func TestDocumentNumbering_RollbackLeavesCounterUnchanged(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		t.Skip("DATABASE_URL not set; skipping the Postgres-backed numbering rollback test")
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

	tenant, err := store.Tenants().Create(ctx, storage.Tenant{Slug: "tenant-rollback-" + uuid.NewString(), Name: "Tenant Rollback"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	const year = 2029 // far from any other test's own year, avoiding collision

	errSentinel := errors.New("deliberate rollback")

	// First allocation, then a deliberate failure: WithTenant rolls the
	// whole transaction back, this allocation included, so last_number
	// must still read 0 afterward, not 1.
	err = store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
		allocated, err := store.DocumentNumbers().AllocateNext(ctx, storage.DocumentTypeInvoice, year)
		if err != nil {
			return err
		}

		if allocated != 1 {
			t.Fatalf("allocated = %d, want 1 (this is the key's first ever allocation)", allocated)
		}

		return errSentinel
	})
	if !errors.Is(err, errSentinel) {
		t.Fatalf("WithTenant error = %v, want errors.Is(err, errSentinel)", err)
	}

	var afterRollback int64

	if err := store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
		var err error
		afterRollback, err = store.DocumentNumbers().Current(ctx, storage.DocumentTypeInvoice, year)

		return err
	}); err != nil {
		t.Fatalf("read counter after rollback: %v", err)
	}

	if afterRollback != 0 {
		t.Fatalf("counter after a rolled-back allocation = %d, want 0 (the allocation must never have taken effect)", afterRollback)
	}

	// The next, successful allocation must still get 1, not 2: the
	// rolled-back call's number was never actually consumed.
	var nextAllocated int64

	if err := store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
		var err error
		nextAllocated, err = store.DocumentNumbers().AllocateNext(ctx, storage.DocumentTypeInvoice, year)

		return err
	}); err != nil {
		t.Fatalf("allocate after rollback: %v", err)
	}

	if nextAllocated != 1 {
		t.Fatalf("next allocation after the rollback = %d, want 1 (the rolled-back allocation's number must not be skipped)", nextAllocated)
	}
}
