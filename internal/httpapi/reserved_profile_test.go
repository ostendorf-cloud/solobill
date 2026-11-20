package httpapi_test

// Issue #68 deletes this file when it implements routes_profile.go.
func init() {
	reservedByFile["routes_profile.go"] = []string{
		"GET /api/v1/profile",
		"PUT /api/v1/profile",
	}
}
