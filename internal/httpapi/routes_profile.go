package httpapi

import "net/http"

// registerProfile mounts the seller profile. Owned by issue #68 (I2).
// See docs/api.md, "Seller profile".
func registerProfile(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"GET /api/v1/profile",
		"PUT /api/v1/profile",
	)
}
