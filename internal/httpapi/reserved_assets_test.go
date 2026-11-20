package httpapi_test

// Issue #78 deletes this file when it implements routes_assets.go.
func init() {
	reservedByFile["routes_assets.go"] = []string{
		"GET /api/v1/assets",
		"POST /api/v1/assets",
		"GET /api/v1/assets/{id}",
		"PUT /api/v1/assets/{id}",
		"POST /api/v1/assets/{id}/disposal",
		"GET /api/v1/assets/{id}/schedule",
	}
}
