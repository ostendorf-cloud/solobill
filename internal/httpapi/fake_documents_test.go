package httpapi_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/docissue/docissuetest"
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
func init() {
	repositoryHooks = append(repositoryHooks, func(_ *testing.T, repos *httpapi.Repositories) {
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

// documentStore reads the fake back out of the app.
func documentStore(t *testing.T, app testApp) *docissuetest.Store {
	t.Helper()

	store, ok := app.repos.DocumentNumbers.(*docissuetest.Store)
	if !ok {
		t.Fatalf("DocumentNumbers = %T, want the fake", app.repos.DocumentNumbers)
	}

	return store
}
