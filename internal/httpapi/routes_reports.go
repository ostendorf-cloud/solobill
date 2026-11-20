package httpapi

import "net/http"

// registerReports mounts the EÜR report and the year assignments of
// entries near the turn of the year. Owned by issue #80 (I12). See
// docs/api.md, "EÜR report".
func registerReports(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"GET /api/v1/reports/euer/{year}",
		"GET /api/v1/reports/euer/{year}/csv",
		"GET /api/v1/euer-assignments",
		"POST /api/v1/euer-assignments",
	)
}
