package httpapi_test

// Issue #75 deletes this file when it implements routes_profile_logo.go.
func init() {
	reservedByFile["routes_profile_logo.go"] = []string{
		"GET /api/v1/profile/logo",
		"PUT /api/v1/profile/logo",
	}
}
