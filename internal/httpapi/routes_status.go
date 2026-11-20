package httpapi

import "net/http"

// registerStatus mounts the operational status the UI shows as a banner,
// today whether the object store's GoBD protection is active. Owned by
// issue #75 (I9). See docs/api.md, "Status".
func registerStatus(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"GET /api/v1/status",
	)
}
