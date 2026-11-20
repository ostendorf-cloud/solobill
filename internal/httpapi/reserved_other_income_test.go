package httpapi_test

// Issue #76 deletes this file when it implements routes_other_income.go.
func init() {
	reservedByFile["routes_other_income.go"] = []string{
		"GET /api/v1/other-income",
		"POST /api/v1/other-income",
		"GET /api/v1/other-income/{id}",
		"PUT /api/v1/other-income/{id}",
		"DELETE /api/v1/other-income/{id}",
		"POST /api/v1/other-income/{id}/book",
		"POST /api/v1/other-income/{id}/storno",
	}
}
