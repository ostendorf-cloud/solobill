package httpapi_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ostendorf-cloud/govue/health"
	"github.com/ostendorf-cloud/govue/logging"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// reservedByFile maps each routes_<resource>.go to the endpoints it still
// answers with a 501 stub. Every entry is added by the init of its own
// reserved_<resource>_test.go, so no two issues edit the same lines: an
// issue that implements its endpoints deletes its own test file.
var reservedByFile = map[string][]string{}

// reservedEndpoints flattens reservedByFile in a stable order.
func reservedEndpoints() []string {
	var all []string
	for _, patterns := range reservedByFile {
		all = append(all, patterns...)
	}
	sort.Strings(all)

	return all
}

// memberEmail and memberPassword are the member account the reserved
// endpoint tests log in with.
const (
	memberEmail    = "member@acme.test"
	memberPassword = "member-password-1"
)

// concrete turns a pattern into a method and a request path.
func concrete(pattern string) (method, path string) {
	method, path, _ = strings.Cut(pattern, " ")
	path = strings.ReplaceAll(path, "{id}", "6f1c2a4e-0000-4000-8000-000000000001")
	path = strings.ReplaceAll(path, "{year}", "2026")

	return method, path
}

// TestReservedListMatchesTheRouter compares the test's list with the
// stubs the router actually registered, in both directions: a stub
// nobody tests and a listed endpoint nobody registers both fail.
func TestReservedListMatchesTheRouter(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	sessions := mustSessions(t)

	handlers, err := httpapi.NewHandler(httpapi.Deps{
		Logger:         logging.New(io.Discard, slog.LevelError),
		Store:          store,
		Sessions:       sessions,
		CSRFKey:        fixedKey(t, 3),
		Login:          newTestLogin(t, store, sessions),
		Invites:        newTestInvites(t, store),
		HealthRegistry: health.New(),
		RateLimitBurst: 10,
		RateLimitPer:   time.Minute,
		DistFS:         fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	registered := httpapi.ReservedPatterns(handlers)

	for p, file := range registered {
		if !slices.Contains(reservedByFile[file], p) {
			t.Errorf("%s registers stub %q, but its reserved test file does not list it", file, p)
		}
	}
	for file, patterns := range reservedByFile {
		for _, p := range patterns {
			got, ok := registered[p]
			switch {
			case !ok:
				t.Errorf("the reserved list of %s names %q, but the router registers no stub for it", file, p)
			case got != file:
				t.Errorf("the reserved list of %s names %q, but %s registers it", file, p, got)
			}
		}
	}
}

// TestReservedEndpointsAnswer501WithASession checks every stub answers
// 501 in the one error shape.
func TestReservedEndpointsAnswer501WithASession(t *testing.T) {
	t.Parallel()

	// A member session: a reserved endpoint answers 501 to any logged in
	// caller, whatever the role its implementation will require.
	app := buildTestApp(t, 1000)
	tenant, _ := seedActiveAccount(t, app.store)
	hash := hashPassword(t, memberPassword)
	secret := totpSecret(t)
	app.store.addAccount(storage.Account{
		TenantID:     tenant.ID,
		Email:        memberEmail,
		PasswordHash: &hash,
		TOTPSecret:   &secret,
		Role:         storage.AccountRoleMember,
		Status:       storage.AccountStatusActive,
	})
	cookie, token := loginAndCSRF(t, app, tenant.Slug, memberEmail, memberPassword, secret)

	for _, pattern := range reservedEndpoints() {
		t.Run(pattern, func(t *testing.T) {
			t.Parallel()

			method, path := concrete(pattern)
			rec := doJSON(t, app.handler, method, path, nil, []*http.Cookie{cookie}, token)
			if rec.Code != http.StatusNotImplemented {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotImplemented, rec.Body.String())
			}

			var body struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Error.Code != "not_implemented" || body.Error.Message == "" {
				t.Errorf("error = %+v, want code not_implemented and a message", body.Error)
			}
		})
	}
}

// TestReservedEndpointsRequireASession checks that a reserved endpoint,
// whatever its method, tells an anonymous caller nothing more than "log
// in first". A request without a session carries no CSRF token either,
// and govue's CSRF protector lets it through, because there is no
// session to forge: the stub's own session check answers.
func TestReservedEndpointsRequireASession(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)

	for _, pattern := range reservedEndpoints() {
		t.Run(pattern, func(t *testing.T) {
			t.Parallel()

			method, path := concrete(pattern)
			rec := doJSON(t, app.handler, method, path, nil, nil, "")
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
			}
		})
	}
}

// TestReservedEndpointsAreDocumented keeps docs/api.md and the router in
// step: every reserved endpoint has its own heading there.
func TestReservedEndpointsAreDocumented(t *testing.T) {
	t.Parallel()

	doc, err := os.ReadFile("../../docs/api.md")
	if err != nil {
		t.Fatalf("read docs/api.md: %v", err)
	}

	for _, pattern := range reservedEndpoints() {
		heading := "### `" + pattern + "`"
		if !strings.Contains(string(doc), heading) {
			t.Errorf("docs/api.md has no heading %q", heading)
		}
	}
}
