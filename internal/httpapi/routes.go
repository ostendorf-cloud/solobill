package httpapi

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/api"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// routeFunc is the per-route middleware chain NewHandler builds (CORS,
// rate limiter, CSRF protector). Every registerX function wraps each of
// its handlers in it.
type routeFunc func(http.Handler) http.Handler

// registerFunc is the shape of every resource file's register function.
// NewHandler calls each one exactly once. A later issue changes only the
// file of its own resource, never NewHandler: see AGENTS.md's conflict
// zone rule and docs/api.md for the owner of each file.
type registerFunc func(mux *http.ServeMux, route routeFunc, a *app)

// registrations is every resource file's register function, in the
// order NewHandler calls them. The order does not matter for routing:
// http.ServeMux picks the most specific pattern.
var registrations = []registerFunc{
	registerAuth,
	registerProfile,
	registerProfileLogo,
	registerVATRegimes,
	registerCustomers,
	registerCatalogue,
	registerDocuments,
	registerPayments,
	registerPDF,
	registerFiles,
	registerExpenses,
	registerOtherIncome,
	registerCategories,
	registerAssets,
	registerReports,
	registerImports,
}

// codeNotImplemented is the error code of a reserved endpoint whose
// issue has not merged yet. govue's api package has no constant for it.
const codeNotImplemented = "not_implemented"

// caller is an authenticated request's tenant, account and role.
type caller struct {
	tenantID  uuid.UUID
	accountID uuid.UUID
	role      storage.AccountRole
}

// requireSession reads the caller's session: 401 without one, 503 when
// the session store cannot answer.
func (a *app) requireSession(r *http.Request) (caller, error) {
	s, ok, err := a.sessions.Read(r.Context(), r)
	if err != nil {
		// Exposed for the same reason as handleMe's identical message.
		return caller{}, api.Unavailable("session store is unavailable").Exposed()
	}
	if !ok {
		return caller{}, api.Unauthenticated("log in first")
	}

	tenantID, err := uuid.Parse(s.Data[sessionTenantIDKey])
	if err != nil {
		return caller{}, api.Internal(fmt.Errorf("httpapi: parse session tenant id: %w", err))
	}
	accountID, err := uuid.Parse(s.Subject)
	if err != nil {
		return caller{}, api.Internal(fmt.Errorf("httpapi: parse session subject: %w", err))
	}

	return caller{tenantID: tenantID, accountID: accountID, role: storage.AccountRole(s.Data[sessionRoleKey])}, nil
}

// notImplemented is the handler of every reserved endpoint: 401 without
// a session, 501 with one, in the one error shape. Requiring the session
// first keeps a reserved endpoint from telling an anonymous caller more
// than an implemented one would.
func (a *app) notImplemented(_ http.ResponseWriter, r *http.Request) error {
	if _, err := a.requireSession(r); err != nil {
		return err
	}

	// Exposed: a static message written for the caller, with no cause.
	return api.New(http.StatusNotImplemented, codeNotImplemented, "this endpoint is reserved and not implemented yet").Exposed()
}

// stub registers each pattern with the notImplemented handler and
// records it, so a test can compare the registered stubs with its own
// list in both directions (see Handlers.reserved).
func stub(mux *http.ServeMux, route routeFunc, a *app, patterns ...string) {
	for _, p := range patterns {
		mux.Handle(p, route(api.Handle(a.notImplemented)))
		a.reserved = append(a.reserved, p)
	}
}
