package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/api"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// maxLogoUploadBytes is the whole body limit of PUT /api/v1/profile/logo,
// multipart framing included (docs/api.md, "Seller logo").
const maxLogoUploadBytes int64 = 512 << 10

// registerProfileLogo mounts the seller logo. Owned by issue #75 (I9),
// because the logo is stored through the object store that issue
// builds. It uses a.repos.SellerProfileLogos and a.repos.Files, never
// SellerProfileRepository.Put. Both routes are file routes: they answer
// 503 object_store_unavailable while the bucket is gated. See
// docs/api.md, "Seller logo".
//
// SellerProfileLogos stays nil until the seller_profiles table of #68
// exists and the logo column migration can land on top of it; both
// routes answer 501 until then, exactly as their stubs did.
func registerProfileLogo(mux *http.ServeMux, route routeFunc, a *app) {
	mux.Handle("GET /api/v1/profile/logo", route(api.Handle(a.handleGetLogo)))
	mux.Handle("PUT /api/v1/profile/logo", route(api.Handle(a.handlePutLogo)))
}

// logoResponse is the 200 body of PUT /api/v1/profile/logo.
type logoResponse struct {
	FileID    uuid.UUID `json:"file_id"`
	SHA256    string    `json:"sha256"`
	SizeBytes int64     `json:"size_bytes"`
}

// handleGetLogo is GET /api/v1/profile/logo. Who: any.
func (a *app) handleGetLogo(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if a.repos.SellerProfileLogos == nil {
		return a.notImplemented(w, r)
	}
	if err := a.requireFileRoutes(); err != nil {
		return err
	}

	var logoID *uuid.UUID
	err = a.repos.Scope.WithTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		var err error
		logoID, err = a.repos.SellerProfileLogos.Logo(ctx)

		return err
	})
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return api.NotFound("no seller profile yet")
	case err != nil:
		return api.Internal(fmt.Errorf("httpapi: read logo: %w", err))
	case logoID == nil:
		return api.NotFound("no logo set")
	}

	file, err := a.findFile(r.Context(), c, *logoID)
	if err != nil {
		return err
	}
	if file.Purpose != storage.FilePurposeLogo || (file.ContentType != contentTypePNG && file.ContentType != contentTypeJPEG) {
		return api.Internal(fmt.Errorf("httpapi: logo file %s has purpose %q and type %q", file.ID, file.Purpose, file.ContentType))
	}

	return a.serveStoredFile(w, r, file, nil)
}

// handlePutLogo is PUT /api/v1/profile/logo. Who: owner. It stores a new
// logo file and points the profile at it; the earlier logo file stays
// stored, like every file.
func (a *app) handlePutLogo(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if c.role != storage.AccountRoleOwner {
		return api.PermissionDenied("owner role required")
	}
	if a.repos.SellerProfileLogos == nil {
		return a.notImplemented(w, r)
	}
	if err := a.requireFileRoutes(); err != nil {
		return err
	}

	// No profile, no upload: checked before the body is read, so nothing
	// is stored for a request that can only fail.
	err = a.repos.Scope.WithTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		_, err := a.repos.SellerProfileLogos.Logo(ctx)

		return err
	})
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return api.NotFound("no seller profile yet")
	case err != nil:
		return api.Internal(fmt.Errorf("httpapi: read logo: %w", err))
	}

	up, err := readUpload(w, r, maxLogoUploadBytes, maxLogoUploadBytes, false)
	if err != nil {
		return err
	}

	contentType := sniffContentType(up.data)
	if contentType != contentTypePNG && contentType != contentTypeJPEG {
		return errUnsupportedType("the logo must be a PNG or a JPEG")
	}

	file, _, err := a.storeFile(r.Context(), c, storage.FilePurposeLogo, contentType, up,
		func(ctx context.Context, file storage.File) error {
			id := file.ID
			err := a.repos.SellerProfileLogos.SetLogo(ctx, &id)
			if errors.Is(err, storage.ErrNotFound) {
				return api.NotFound("no seller profile yet")
			}

			return err
		})
	if err != nil {
		return err
	}

	return api.Write(w, http.StatusOK, logoResponse{FileID: file.ID, SHA256: file.SHA256, SizeBytes: file.SizeBytes})
}
