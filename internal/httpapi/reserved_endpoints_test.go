package httpapi_test

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/auth"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// reservedEndpoints is every endpoint the year end contract (issue #67)
// reserves as a 501 stub. An issue that implements an endpoint removes
// its row here in the same pull request.
var reservedEndpoints = []string{
	"GET /api/v1/profile",
	"PUT /api/v1/profile",
	"GET /api/v1/profile/logo",
	"PUT /api/v1/profile/logo",
	"GET /api/v1/vat-regimes",
	"POST /api/v1/vat-regimes",
	"GET /api/v1/customers",
	"POST /api/v1/customers",
	"GET /api/v1/customers/{id}",
	"PUT /api/v1/customers/{id}",
	"DELETE /api/v1/customers/{id}",
	"GET /api/v1/catalogue-items",
	"POST /api/v1/catalogue-items",
	"GET /api/v1/catalogue-items/{id}",
	"PUT /api/v1/catalogue-items/{id}",
	"DELETE /api/v1/catalogue-items/{id}",
	"GET /api/v1/documents",
	"POST /api/v1/documents",
	"POST /api/v1/documents/preview",
	"GET /api/v1/documents/{id}",
	"PUT /api/v1/documents/{id}",
	"DELETE /api/v1/documents/{id}",
	"POST /api/v1/documents/{id}/issue",
	"POST /api/v1/documents/{id}/correction",
	"POST /api/v1/offers/{id}/to-invoice",
	"GET /api/v1/documents/{id}/payments",
	"POST /api/v1/documents/{id}/payments",
	"GET /api/v1/documents/{id}/pdf",
	"POST /api/v1/files",
	"GET /api/v1/files/{id}",
	"GET /api/v1/files/{id}/content",
	"GET /api/v1/expenses",
	"POST /api/v1/expenses",
	"GET /api/v1/expenses/{id}",
	"PUT /api/v1/expenses/{id}",
	"DELETE /api/v1/expenses/{id}",
	"POST /api/v1/expenses/{id}/book",
	"POST /api/v1/expenses/{id}/storno",
	"GET /api/v1/other-income",
	"POST /api/v1/other-income",
	"GET /api/v1/other-income/{id}",
	"PUT /api/v1/other-income/{id}",
	"DELETE /api/v1/other-income/{id}",
	"POST /api/v1/other-income/{id}/book",
	"POST /api/v1/other-income/{id}/storno",
	"GET /api/v1/categories",
	"GET /api/v1/assets",
	"POST /api/v1/assets",
	"GET /api/v1/assets/{id}",
	"PUT /api/v1/assets/{id}",
	"POST /api/v1/assets/{id}/disposal",
	"GET /api/v1/assets/{id}/schedule",
	"GET /api/v1/reports/euer/{year}",
	"GET /api/v1/reports/euer/{year}/csv",
	"POST /api/v1/imports",
	"GET /api/v1/imports/{id}",
	"POST /api/v1/imports/{id}/commit",
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

// TestReservedEndpointsAnswer501WithASession checks every stub answers
// 501 in the one error shape.
func TestReservedEndpointsAnswer501WithASession(t *testing.T) {
	t.Parallel()

	// A member session: a reserved endpoint answers 501 to any logged in
	// caller, whatever the role its implementation will require.
	app := buildTestApp(t, 1000)
	tenant, _ := seedActiveAccount(t, app.store)
	hash, err := auth.HashPassword(memberPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret() error = %v", err)
	}
	app.store.addAccount(storage.Account{
		TenantID:     tenant.ID,
		Email:        memberEmail,
		PasswordHash: &hash,
		TOTPSecret:   &secret,
		Role:         storage.AccountRoleMember,
		Status:       storage.AccountStatusActive,
	})
	cookie, token := loginAndCSRF(t, app, tenant.Slug, memberEmail, memberPassword, secret)

	for _, pattern := range reservedEndpoints {
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

// TestReservedEndpointsRequireASession checks that a reserved endpoint
// tells an anonymous caller nothing more than "log in first". Only the
// safe methods are checked: an unsafe one is already refused by the
// CSRF protector before it reaches the handler.
func TestReservedEndpointsRequireASession(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)

	for _, pattern := range reservedEndpoints {
		method, path := concrete(pattern)
		if method != http.MethodGet {
			continue
		}
		t.Run(pattern, func(t *testing.T) {
			t.Parallel()

			rec := doJSON(t, app.handler, method, path, nil, nil, "")
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
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

	for _, pattern := range reservedEndpoints {
		heading := "### `" + pattern + "`"
		if !strings.Contains(string(doc), heading) {
			t.Errorf("docs/api.md has no heading %q", heading)
		}
	}
}
