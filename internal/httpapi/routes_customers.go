package httpapi

import "net/http"

// registerCustomers mounts the customers API. Owned by issue #69 (I3).
// See docs/api.md, "Customers".
func registerCustomers(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"GET /api/v1/customers",
		"POST /api/v1/customers",
		"GET /api/v1/customers/{id}",
		"PUT /api/v1/customers/{id}",
		"DELETE /api/v1/customers/{id}",
	)
}
