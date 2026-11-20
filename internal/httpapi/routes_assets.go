package httpapi

import "net/http"

// registerAssets mounts the asset register. Owned by issue #78 (I11a).
// See docs/api.md, "Assets".
func registerAssets(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"GET /api/v1/assets",
		"POST /api/v1/assets",
		"GET /api/v1/assets/{id}",
		"PUT /api/v1/assets/{id}",
		"POST /api/v1/assets/{id}/disposal",
		"GET /api/v1/assets/{id}/schedule",
	)
}
