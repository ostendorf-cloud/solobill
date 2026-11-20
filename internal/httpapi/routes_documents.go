package httpapi

import "net/http"

// registerDocuments mounts offers, invoices and corrections. Owned by
// issue #70 (I4). See docs/api.md, "Documents".
func registerDocuments(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"GET /api/v1/documents",
		"POST /api/v1/documents",
		"POST /api/v1/documents/preview",
		"GET /api/v1/documents/{id}",
		"PUT /api/v1/documents/{id}",
		"DELETE /api/v1/documents/{id}",
		"POST /api/v1/documents/{id}/issue",
		"POST /api/v1/documents/{id}/correction",
		"POST /api/v1/offers/{id}/to-invoice",
	)
}
