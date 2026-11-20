package httpapi_test

// Issue #74 deletes this file when it implements routes_pdf.go.
func init() {
	reservedByFile["routes_pdf.go"] = []string{
		"GET /api/v1/documents/{id}/pdf",
	}
}
