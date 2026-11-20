package httpapi

import "net/http"

// registerOtherIncome mounts the other income API. Owned by issue #76
// (I10a). See docs/api.md, "Other income".
func registerOtherIncome(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"GET /api/v1/other-income",
		"POST /api/v1/other-income",
		"GET /api/v1/other-income/{id}",
		"PUT /api/v1/other-income/{id}",
		"DELETE /api/v1/other-income/{id}",
		"POST /api/v1/other-income/{id}/book",
		"POST /api/v1/other-income/{id}/storno",
	)
}
