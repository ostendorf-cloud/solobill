package httpapi

import "net/http"

// registerImports reserves the historic import. Owned by issue #81
// (I13). The import format is decided later, so docs/api.md fixes only
// the paths, the batch shape and the status codes.
func registerImports(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"POST /api/v1/imports",
		"GET /api/v1/imports/{id}",
		"POST /api/v1/imports/{id}/commit",
	)
}
