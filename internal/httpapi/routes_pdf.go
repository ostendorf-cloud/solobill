package httpapi

import "net/http"

// registerPDF mounts the PDF of a document. Owned by issue #74 (I8).
// For an issued document it is a file route: the first download renders
// once from the frozen payload and stores the PDF as a files row of
// purpose document_pdf; every later download returns those stored bytes.
// It answers 503 object_store_unavailable while the bucket is gated.
// Issuing never depends on the bucket. See docs/api.md, "PDF".
func registerPDF(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"GET /api/v1/documents/{id}/pdf",
	)
}
