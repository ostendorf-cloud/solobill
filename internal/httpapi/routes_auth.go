package httpapi

import (
	"net/http"

	"github.com/ostendorf-cloud/govue/api"
)

// registerAuth mounts the login, logout, "who am I" and invite routes,
// plus the OIDC routes when OIDC is configured. The handlers live in
// auth_handlers.go.
func registerAuth(mux *http.ServeMux, route routeFunc, a *app) {
	mux.Handle("POST /api/v1/auth/login", route(api.Handle(a.handleLogin)))
	mux.Handle("POST /api/v1/auth/logout", route(api.Handle(a.handleLogout)))
	mux.Handle("GET /api/v1/me", route(api.Handle(a.handleMe)))
	mux.Handle("POST /api/v1/invites", route(api.Handle(a.handleCreateInvite)))
	mux.Handle("POST /api/v1/invites/totp-secret", route(api.Handle(a.handleInviteTOTPSecret)))
	mux.Handle("POST /api/v1/invites/accept", route(api.Handle(a.handleAcceptInvite)))

	if a.oidcHandlers != nil {
		mux.Handle("GET "+oidcLoginPath, route(a.oidcHandlers.Login()))
		mux.Handle("GET "+oidcCallbackPath, route(a.oidcHandlers.Callback()))
		mux.Handle("POST /api/v1/invites/oidc/start", route(api.Handle(a.handleInviteOIDCStart)))
	}
}
