package httpapi

import (
	"net/http"

	"github.com/ostendorf-cloud/govue/api"

	"github.com/ostendorf-cloud/solobill/internal/auth"
)

// tenantPathValue is the path wildcard of the routes that run before a
// session exists and so take their tenant from the path: the password
// login and the passkey login. govue's handlers refuse unknown body
// fields, so the tenant slug cannot ride in the body any more.
const tenantPathValue = "tenant"

// authMethodsResponse is GET /api/v1/auth/methods: which login methods
// beyond the local login are configured, for the login view to show only
// those. It carries no setting of its own, only what a request to the
// login route would reveal anyway.
type authMethodsResponse struct {
	OIDC bool `json:"oidc"`
}

// handleAuthMethods is GET /api/v1/auth/methods. Who: anyone, the login
// view calls it before a session exists.
func (a *app) handleAuthMethods(w http.ResponseWriter, _ *http.Request) error {
	return api.Write(w, http.StatusOK, authMethodsResponse{OIDC: a.oidcHandlers != nil})
}

// registerAuth mounts the login, logout, password change, "who am I",
// passkey and invite routes, plus the OIDC routes when OIDC is configured.
//
// The password login, the logout and the password change are govue's
// account handlers, the passkey routes govue's passkey handlers, both
// built by internal/auth.NewLogin. The routes that verify a password or a
// passkey run the stricter per client limit (checked), and the password
// login also the limits per login (perLogin). The handlers live in
// auth_handlers.go and in govue.
func registerAuth(mux *http.ServeMux, route routeFunc, a *app) {
	byTenant := func(h http.Handler) http.Handler {
		return auth.TenantFromPath(a.store, tenantPathValue, h)
	}
	bySession := func(h http.Handler) http.Handler {
		return auth.TenantFromSession(a.sessions, h)
	}

	accounts := a.login.Accounts

	mux.Handle("POST /api/v1/tenants/{tenant}/auth/login", route(a.checked(a.perLogin(byTenant(accounts.Login())))))
	mux.Handle("POST /api/v1/auth/logout", route(accounts.Logout()))
	mux.Handle("POST /api/v1/auth/password", route(a.checked(bySession(accounts.ChangePassword()))))
	mux.Handle("GET /api/v1/me", route(api.Handle(a.handleMe)))

	if passkeys := a.login.Passkeys; passkeys != nil {
		mux.Handle("POST /api/v1/tenants/{tenant}/auth/passkeys/login/begin", route(a.checked(byTenant(passkeys.LoginBegin()))))
		mux.Handle("POST /api/v1/tenants/{tenant}/auth/passkeys/login/finish", route(a.checked(byTenant(passkeys.LoginFinish()))))
		mux.Handle("GET /api/v1/auth/passkeys", route(bySession(passkeys.List())))
		mux.Handle("POST /api/v1/auth/passkeys/register/begin", route(a.checked(bySession(passkeys.RegisterBegin()))))
		mux.Handle("POST /api/v1/auth/passkeys/register/finish", route(a.checked(bySession(passkeys.RegisterFinish()))))
		mux.Handle("POST /api/v1/auth/passkeys/delete", route(a.checked(bySession(passkeys.Delete()))))
	}

	mux.Handle("POST /api/v1/invites", route(api.Handle(a.handleCreateInvite)))
	mux.Handle("POST /api/v1/invites/totp-secret", route(a.checked(api.Handle(a.handleInviteTOTPSecret))))
	mux.Handle("POST /api/v1/invites/accept", route(a.checked(api.Handle(a.handleAcceptInvite))))
	mux.Handle("POST /api/v1/invites/confirm", route(a.checked(api.Handle(a.handleInviteConfirm))))

	mux.Handle("GET /api/v1/auth/methods", route(api.Handle(a.handleAuthMethods)))

	// The old OIDC paths, see legacyOIDCPrefix. Registered with no method
	// and as both the bare prefix and its subtree, so no method and no
	// path below it falls through to the SPA catch-all.
	mux.Handle(legacyOIDCPrefix, route(api.NotFoundHandler()))
	mux.Handle(legacyOIDCPrefix+"/", route(api.NotFoundHandler()))

	if a.oidcHandlers != nil {
		mux.Handle("GET "+oidcLoginPath, route(a.oidcHandlers.Login()))
		mux.Handle("GET "+oidcCallbackPath, route(a.oidcHandlers.Callback()))
		mux.Handle("POST /api/v1/invites/oidc/start", route(api.Handle(a.handleInviteOIDCStart)))
	}
}
