package httpapi_test

// Issue #76 deletes this file when it implements routes_expenses.go.
func init() {
	reservedByFile["routes_expenses.go"] = []string{
		"GET /api/v1/expenses",
		"POST /api/v1/expenses",
		"GET /api/v1/expenses/{id}",
		"PUT /api/v1/expenses/{id}",
		"DELETE /api/v1/expenses/{id}",
		"POST /api/v1/expenses/{id}/book",
		"POST /api/v1/expenses/{id}/storno",
	}
}
