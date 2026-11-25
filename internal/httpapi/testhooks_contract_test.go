package httpapi_test

import (
	"context"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
)

// sentinelRenderer is a renderer no real code builds, set by the
// contract's own repository hook, so a test can see whether the hooks'
// result actually reached NewHandler.
type sentinelRenderer struct{}

func (sentinelRenderer) RenderWithLogo(context.Context, []byte, bool, []byte) ([]byte, error) {
	return nil, nil
}

func init() {
	repositoryHooks = append(repositoryHooks, func(_ *testing.T, repos *httpapi.Repositories) {
		if repos.Renderer == nil {
			repos.Renderer = sentinelRenderer{}
		}
	})
}

// TestRepositoryHooksReachNewHandler proves buildTestApp passes what the
// hooks set into NewHandler, and that Scope is the fake store before any
// hook runs.
func TestRepositoryHooksReachNewHandler(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 10)
	got := httpapi.RepositoriesOf(app.handlers)

	if _, ok := got.Renderer.(sentinelRenderer); !ok {
		t.Errorf("NewHandler got Renderer %T, want the hook's sentinelRenderer", got.Renderer)
	}
	if got.Scope != app.store {
		t.Errorf("NewHandler got Scope %v, want the fake store", got.Scope)
	}
}
