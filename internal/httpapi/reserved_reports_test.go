package httpapi_test

// Issue #80 deletes this file when it implements routes_reports.go.
func init() {
	reservedByFile["routes_reports.go"] = []string{
		"GET /api/v1/reports/euer/{year}",
		"GET /api/v1/reports/euer/{year}/csv",
		"GET /api/v1/euer-assignments",
		"POST /api/v1/euer-assignments",
	}
}
