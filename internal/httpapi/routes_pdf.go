package httpapi

import "net/http"

// registerPDF mounts the PDF of a document. Owned by issue #74 (I8).
// See docs/api.md, "PDF".
func registerPDF(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"GET /api/v1/documents/{id}/pdf",
	)
}
