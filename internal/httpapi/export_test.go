package httpapi

import "slices"

// ReservedPatterns returns every pattern h still answers with a 501
// stub, for reserved_endpoints_test.go.
func ReservedPatterns(h Handlers) []string {
	return slices.Clone(h.reserved)
}
