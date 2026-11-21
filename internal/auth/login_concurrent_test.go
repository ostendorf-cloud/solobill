package auth

import (
	"errors"
	"sync"
	"testing"

	"github.com/ostendorf-cloud/govue/account"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// TestLoginWithOneCodeSucceedsOnceWhenConcurrent: the counter advance is
// one conditional write, so of several logins presenting the same code
// exactly one succeeds and the rest get the generic refusal. The fake
// serialises the write with a mutex; the same property against the real
// statement is TestAdvanceTOTPCounterIsAtomicUnderConcurrency in
// internal/storage/postgres.
func TestLoginWithOneCodeSucceedsOnceWhenConcurrent(t *testing.T) {
	t.Parallel()

	const attempts = 8

	f := newLoginFixture(t, storage.AccountStatusActive)
	code := f.code(t)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		success int
		refused int
		other   []error
	)

	start := make(chan struct{})

	for range attempts {
		wg.Go(func() {
			<-start

			_, err := f.service.Login(f.ctx(t), "owner@acme.example", f.password, code)

			mu.Lock()
			defer mu.Unlock()

			switch {
			case err == nil:
				success++
			case errors.Is(err, account.ErrInvalidCredentials):
				refused++
			default:
				other = append(other, err)
			}
		})
	}

	close(start)
	wg.Wait()

	if success != 1 || refused != attempts-1 || len(other) != 0 {
		t.Errorf("successes = %d, refusals = %d, other errors = %v; want 1, %d, none", success, refused, other, attempts-1)
	}
}
