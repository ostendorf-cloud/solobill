package httpapi

import "net/http"

// registerProfileLogo mounts the seller logo. Owned by issue #75 (I9),
// because the logo is stored through the object store that issue
// builds. It uses a.repos.SellerProfileLogos and a.repos.Files, never
// SellerProfileRepository.Put. Both routes are file routes: they answer
// 503 object_store_unavailable while the bucket is gated. See
// docs/api.md, "Seller logo".
func registerProfileLogo(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"GET /api/v1/profile/logo",
		"PUT /api/v1/profile/logo",
	)
}
