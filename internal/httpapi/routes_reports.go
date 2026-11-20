package httpapi

import "net/http"

// registerReports mounts the EÜR report. Owned by issue #80 (I12). See
// docs/api.md, "EÜR report".
func registerReports(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"GET /api/v1/reports/euer/{year}",
		"GET /api/v1/reports/euer/{year}/csv",
	)
}
