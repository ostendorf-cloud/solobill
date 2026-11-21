package httpapi

import "net/http"

// registerPayments mounts the payments of a document. Owned by issue
// #71 (I5). See docs/api.md, "Payments".
func registerPayments(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"GET /api/v1/documents/{id}/payments",
		"POST /api/v1/documents/{id}/payments",
	)
}
