package httpapi_test

import (
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
)

// repositoryHook sets one feature's fake repositories on the
// Repositories every buildTestApp call hands to NewHandler.
type repositoryHook func(t *testing.T, repos *httpapi.Repositories)

// repositoryHooks is every feature's hook. A feature adds its fakes in its
// own fake_<resource>_test.go, whose init appends one hook here, and
// never edits buildTestApp, fake_store_test.go or this file: see
// AGENTS.md's conflict zones. A test reads its fake back through
// testApp.repos.
var repositoryHooks []repositoryHook
