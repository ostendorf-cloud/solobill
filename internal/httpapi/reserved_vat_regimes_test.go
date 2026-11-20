package httpapi_test

// Issue #68 deletes this file when it implements routes_vat_regimes.go.
func init() {
	reservedByFile["routes_vat_regimes.go"] = []string{
		"GET /api/v1/vat-regimes",
		"POST /api/v1/vat-regimes",
	}
}
