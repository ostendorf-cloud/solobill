package httpapi

import (
	"net/http"

	"github.com/ostendorf-cloud/govue/api"

	"github.com/ostendorf-cloud/solobill/internal/category"
)

// registerCategories mounts the EÜR line keys list. Owned by issue #76
// (I10a). See docs/api.md, "Categories".
func registerCategories(mux *http.ServeMux, route routeFunc, a *app) {
	mux.Handle("GET /api/v1/categories", route(api.Handle(a.handleListCategories)))
}

// categoryJSON is one entry of GET /api/v1/categories.
type categoryJSON struct {
	Key     string `json:"key"`
	Kind    string `json:"kind"`
	LabelDE string `json:"label_de"`
	LabelEN string `json:"label_en"`
}

// handleListCategories answers GET /api/v1/categories for any role: every
// key of category.All(), in form order.
func (a *app) handleListCategories(w http.ResponseWriter, r *http.Request) error {
	if _, err := a.requireSession(r); err != nil {
		return err
	}

	all := category.All()
	out := make([]categoryJSON, len(all))
	for i, c := range all {
		out[i] = categoryJSON{Key: string(c.Key), Kind: string(c.Kind), LabelDE: c.LabelDE, LabelEN: c.LabelEN}
	}

	return api.Write(w, http.StatusOK, map[string]any{"categories": out})
}
