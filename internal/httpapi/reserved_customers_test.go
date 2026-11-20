package httpapi_test

// Issue #69 deletes this file when it implements routes_customers.go.
func init() {
	reservedByFile["routes_customers.go"] = []string{
		"GET /api/v1/customers",
		"POST /api/v1/customers",
		"GET /api/v1/customers/{id}",
		"PUT /api/v1/customers/{id}",
		"DELETE /api/v1/customers/{id}",
	}
}
