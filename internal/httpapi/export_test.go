package httpapi

import "maps"

// ReservedPatterns returns every pattern h still answers with a 501
// stub, mapped to the routes_<resource>.go file that registered it, for
// reserved_endpoints_test.go.
func ReservedPatterns(h Handlers) map[string]string {
	return maps.Clone(h.reserved)
}

// RepositoriesOf returns the Repositories h was built with, for
// testhooks_contract_test.go.
func RepositoriesOf(h Handlers) Repositories {
	return h.repos
}
