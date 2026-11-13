package httpapi

import (
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/ostendorf-cloud/govue/api"
)

// apiPathPrefix is every path this application answers as JSON rather than
// as part of the SPA. newSPAHandler checks it directly, rather than
// registering its own "/api/" pattern on the mux, because http.ServeMux
// would then treat an unmatched route under /api/ and an unmatched route
// anywhere else as two different routes, and this application wants
// exactly one catch-all, answering each shape differently.
const apiPathPrefix = "/api/"

// newSPAHandler serves the embedded Vue build: a real static asset when
// the request path names one, and index.html for everything else so the
// SPA's own client-side router can take over, matching how a single page
// application is conventionally served behind a catch-all route. An
// unmatched path under apiPathPrefix is answered with the same JSON error
// envelope every other endpoint in this package uses, never the SPA's
// HTML, so a client calling a mistyped or removed API path gets something
// it can actually parse.
func newSPAHandler(distFS fs.FS) (http.Handler, error) {
	index, err := fs.ReadFile(distFS, "index.html")
	if err != nil {
		return nil, fmt.Errorf("httpapi: read embedded index.html: %w", err)
	}

	fileServer := http.FileServer(http.FS(distFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, apiPathPrefix) {
			api.WriteError(w, r, api.NotFound("unknown endpoint"))

			return
		}

		if r.URL.Path != "/" {
			if _, err := fs.Stat(distFS, strings.TrimPrefix(r.URL.Path, "/")); err == nil {
				fileServer.ServeHTTP(w, r)

				return
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(index)
	}), nil
}
