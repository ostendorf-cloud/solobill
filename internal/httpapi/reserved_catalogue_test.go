package httpapi_test

// Issue #69 deletes this file when it implements routes_catalogue.go.
func init() {
	reservedByFile["routes_catalogue.go"] = []string{
		"GET /api/v1/catalogue-items",
		"POST /api/v1/catalogue-items",
		"GET /api/v1/catalogue-items/{id}",
		"PUT /api/v1/catalogue-items/{id}",
		"DELETE /api/v1/catalogue-items/{id}",
	}
}
