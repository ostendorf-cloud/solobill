package httpapi_test

// Issue #75 deletes this file when it implements routes_status.go.
func init() {
	reservedByFile["routes_status.go"] = []string{
		"GET /api/v1/status",
	}
}
