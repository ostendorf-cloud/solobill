package httpapi_test

// Issue #75 deletes this file when it implements routes_files.go.
func init() {
	reservedByFile["routes_files.go"] = []string{
		"POST /api/v1/files",
		"GET /api/v1/files/{id}",
		"GET /api/v1/files/{id}/content",
	}
}
