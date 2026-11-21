//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ostendorf-cloud/govue/account"

	"github.com/ostendorf-cloud/solobill/internal/auth"
	"github.com/ostendorf-cloud/solobill/internal/auth/authtest"
)

// TestAdvanceTOTPCounterIsAtomicUnderConcurrency: several advances to the
// same counter race, and exactly one changes the row. Every other one is
// refused with account.ErrNotFound, which govue maps to the generic
// invalid credentials answer. Each call runs in its own WithTenant scope.
func TestAdvanceTOTPCounterIsAtomicUnderConcurrency(t *testing.T) {
	store, _, tenant := loginTestStore(t)
	ctx := context.Background()

	scoped := authtest.ScopedAccountStore{Store: auth.NewAccountStore(store), Scope: scopeTo(tenant.ID)}

	const hash = "$argon2id$v=19$m=65536,t=3,p=4$ZmFrZS1zYWx0$ZmFrZS1oYXNoLW5vdC1hLXJlYWwtb25l" // gitleaks:allow
	const secret = "FAKESECRETFAKESECRETFAKESECRETFA"                                           // gitleaks:allow

	created, err := scoped.Create(ctx, account.Record{Login: "race@login.example", PasswordHash: hash, TOTPSecret: secret})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := scoped.ConfirmTOTP(ctx, created.ID, secret, 100); err != nil {
		t.Fatalf("ConfirmTOTP: %v", err)
	}

	const attempts = 8

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)

	start := make(chan struct{})

	for range attempts {
		wg.Go(func() {
			<-start

			err := scoped.AdvanceTOTPCounter(ctx, created.ID, 150)

			mu.Lock()
			defer mu.Unlock()

			errs = append(errs, err)
		})
	}

	close(start)
	wg.Wait()

	var ok, refused int

	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, account.ErrNotFound):
			refused++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}

	if ok != 1 || refused != attempts-1 {
		t.Errorf("%d advances succeeded and %d were refused, want 1 and %d", ok, refused, attempts-1)
	}

	got, err := scoped.FindByLogin(ctx, "race@login.example")
	if err != nil {
		t.Fatalf("FindByLogin: %v", err)
	}
	if got.TOTPCounter != 150 {
		t.Errorf("counter = %d, want 150", got.TOTPCounter)
	}

	// A lower counter and the same counter again are both refused, each
	// in its own scope.
	for _, counter := range []int64{150, 149} {
		if err := scoped.AdvanceTOTPCounter(ctx, created.ID, counter); !errors.Is(err, account.ErrNotFound) {
			t.Errorf("AdvanceTOTPCounter(%d) = %v, want account.ErrNotFound", counter, err)
		}
	}
}
