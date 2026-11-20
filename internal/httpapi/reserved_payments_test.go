package httpapi_test

// Issue #71 deletes this file when it implements routes_payments.go.
func init() {
	reservedByFile["routes_payments.go"] = []string{
		"GET /api/v1/documents/{id}/payments",
		"POST /api/v1/documents/{id}/payments",
	}
}
