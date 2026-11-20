package httpapi

import "net/http"

// registerCatalogue mounts the catalogue items API. Owned by issue #69
// (I3). See docs/api.md, "Catalogue items".
func registerCatalogue(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"GET /api/v1/catalogue-items",
		"POST /api/v1/catalogue-items",
		"GET /api/v1/catalogue-items/{id}",
		"PUT /api/v1/catalogue-items/{id}",
		"DELETE /api/v1/catalogue-items/{id}",
	)
}
