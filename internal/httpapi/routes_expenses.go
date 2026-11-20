package httpapi

import "net/http"

// registerExpenses mounts the expenses API. Owned by issue #76 (I10a).
// See docs/api.md, "Expenses".
func registerExpenses(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"GET /api/v1/expenses",
		"POST /api/v1/expenses",
		"GET /api/v1/expenses/{id}",
		"PUT /api/v1/expenses/{id}",
		"DELETE /api/v1/expenses/{id}",
		"POST /api/v1/expenses/{id}/book",
		"POST /api/v1/expenses/{id}/storno",
	)
}
