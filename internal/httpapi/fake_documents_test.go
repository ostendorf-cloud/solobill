package httpapi_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/docissue/docissuetest"
	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// The documents tests run against docissuetest.Store, the in memory
// DocumentRepository and DocumentNumberRepository of internal/docissue's
// own tests. It filters by the tenant the handler opened the scope for,
// read back through httpapi.ScopedTenant, the way row level security
// filters by the tenant of the transaction. Its seller profile is the
// profile fake of issue #68, read when the issue asks for it, because
// that hook runs after this one.
//
// The profile routes of issue #68 open the scope through
// Repositories.Scope directly, without the tag httpapi.ScopedTenant
// reads, and list the documents to see which types are issued. An
// untagged scope therefore reads as uuid.Nil, a tenant no row has: the
// fake answers no rows and refuses every write, instead of failing the
// profile routes with ErrNoTenantScope.
//
// Issuing fails closed without a renderer that checks payloads. A test
// opts in with withPayloadChecker before it builds its app; the hook
// then sets checkingRenderer. Every other app keeps the contract's own
// sentinel renderer (testhooks_contract_test.go), which checks nothing.
func init() {
	repositoryHooks = append(repositoryHooks, func(t *testing.T, repos *httpapi.Repositories) {
		if _, ok := payloadCheckerByTest.Load(t); ok {
			repos.Renderer = checkingRenderer{}
		}
		tenant := func(ctx context.Context) (uuid.UUID, bool) {
			if id, ok := httpapi.ScopedTenant(ctx); ok {
				return id, true
			}

			return uuid.Nil, true
		}
		store := docissuetest.New(tenant, func(ctx context.Context) (storage.SellerProfile, error) {
			if repos.SellerProfiles == nil {
				return storage.SellerProfile{}, storage.ErrNotFound
			}

			return repos.SellerProfiles.Get(ctx)
		})
		repos.Documents = store
		repos.DocumentNumbers = store
	})
}

// payloadCheckerByTest holds the tests whose app gets checkingRenderer.
var payloadCheckerByTest sync.Map

// withPayloadChecker makes the next app of t carry checkingRenderer.
func withPayloadChecker(t *testing.T) {
	t.Helper()

	payloadCheckerByTest.Store(t, true)
	t.Cleanup(func() { payloadCheckerByTest.Delete(t) })
}

// checkingRenderer stands in for the renderer of issue #74: it accepts
// every payload in CheckPayload and renders nothing.
type checkingRenderer struct{}

func (checkingRenderer) Render(context.Context, []byte, bool) ([]byte, error) {
	return nil, errors.New("checkingRenderer: rendering is not supported")
}

func (checkingRenderer) CheckPayload(context.Context, docpayload.FrozenPayloadV1) (map[string]string, error) {
	return nil, nil
}

// documentStore reads the fake back out of the app.
func documentStore(t *testing.T, app testApp) *docissuetest.Store {
	t.Helper()

	store, ok := app.repos.DocumentNumbers.(*docissuetest.Store)
	if !ok {
		t.Fatalf("DocumentNumbers = %T, want the fake", app.repos.DocumentNumbers)
	}

	return store
}
