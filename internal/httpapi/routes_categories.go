package httpapi

import "net/http"

// registerCategories mounts the EÜR line keys list. Owned by issue #76
// (I10a). See docs/api.md, "Categories".
func registerCategories(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"GET /api/v1/categories",
	)
}
