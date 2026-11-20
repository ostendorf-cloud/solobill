package httpapi

import "net/http"

// registerProfile mounts the seller profile. Owned by issue #68 (I2).
// See docs/api.md, "Seller profile".
//
// The profile's has_logo field is computed here from
// a.repos.SellerProfileLogos (false while it is nil, before issue #75).
// The logo itself is never read or written through these routes: issue
// #75 owns the logo routes in routes_profile_logo.go and the logo column.
func registerProfile(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"GET /api/v1/profile",
		"PUT /api/v1/profile",
	)
}
