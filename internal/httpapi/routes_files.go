package httpapi

import "net/http"

// registerFiles mounts receipt and file upload and download. Owned by
// issue #75 (I9). See docs/api.md, "Files".
func registerFiles(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"POST /api/v1/files",
		"GET /api/v1/files/{id}",
		"GET /api/v1/files/{id}/content",
	)
}
