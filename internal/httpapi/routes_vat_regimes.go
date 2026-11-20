package httpapi

import "net/http"

// registerVATRegimes mounts the VAT regime history. Owned by issue #68
// (I2). See docs/api.md, "VAT regime periods".
func registerVATRegimes(mux *http.ServeMux, route routeFunc, a *app) {
	stub(mux, route, a,
		"GET /api/v1/vat-regimes",
		"POST /api/v1/vat-regimes",
	)
}
