package httpapi

import (
	"net/http"
	"time"

	"github.com/ostendorf-cloud/govue/api"
)

// registerStatus mounts the operational status the UI shows as a banner,
// today whether the object store's GoBD protection is active. Owned by
// issue #75 (I9). See docs/api.md, "Status".
func registerStatus(mux *http.ServeMux, route routeFunc, a *app) {
	mux.Handle("GET /api/v1/status", route(api.Handle(a.handleStatus)))
}

// objectStoreStatusJSON is the object_store block of GET /api/v1/status.
type objectStoreStatusJSON struct {
	Reachable            bool       `json:"reachable"`
	Versioning           bool       `json:"versioning"`
	ObjectLock           bool       `json:"object_lock"`
	RequireVersioning    bool       `json:"require_versioning"`
	FileRoutesAvailable  bool       `json:"file_routes_available"`
	GoBDProtectionActive bool       `json:"gobd_protection_active"`
	CheckedAt            *time.Time `json:"checked_at"`
}

type statusResponse struct {
	ObjectStore objectStoreStatusJSON `json:"object_store"`
}

// handleStatus is GET /api/v1/status. Who: any. It serves the last
// periodic bucket check and never checks per request. /readyz does not
// depend on it.
func (a *app) handleStatus(w http.ResponseWriter, r *http.Request) error {
	if _, err := a.requireSession(r); err != nil {
		return err
	}

	s := a.objectStoreStatus()
	out := objectStoreStatusJSON{
		Reachable:            s.Reachable,
		Versioning:           s.Versioning,
		ObjectLock:           s.ObjectLock,
		RequireVersioning:    s.RequireVersioning,
		FileRoutesAvailable:  a.repos.Objects != nil && s.FileRoutesAvailable(),
		GoBDProtectionActive: s.GoBDProtectionActive(),
	}
	if !s.CheckedAt.IsZero() {
		checked := s.CheckedAt.UTC()
		out.CheckedAt = &checked
	}

	w.Header().Set("Cache-Control", "no-store")

	return api.Write(w, http.StatusOK, statusResponse{ObjectStore: out})
}
