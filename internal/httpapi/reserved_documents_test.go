package httpapi_test

// Issue #70 deletes this file when it implements routes_documents.go.
func init() {
	reservedByFile["routes_documents.go"] = []string{
		"GET /api/v1/documents",
		"POST /api/v1/documents",
		"POST /api/v1/documents/preview",
		"GET /api/v1/documents/{id}",
		"PUT /api/v1/documents/{id}",
		"DELETE /api/v1/documents/{id}",
		"POST /api/v1/documents/{id}/issue",
		"POST /api/v1/documents/{id}/correction",
		"POST /api/v1/offers/{id}/to-invoice",
	}
}
