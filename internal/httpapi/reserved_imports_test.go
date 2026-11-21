package httpapi_test

// Issue #81 deletes this file when it implements routes_imports.go.
func init() {
	reservedByFile["routes_imports.go"] = []string{
		"POST /api/v1/imports",
		"GET /api/v1/imports/{id}",
		"POST /api/v1/imports/{id}/commit",
	}
}
