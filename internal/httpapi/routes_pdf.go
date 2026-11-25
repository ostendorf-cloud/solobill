package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/api"
	"github.com/ostendorf-cloud/govue/logging"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	logocheck "github.com/ostendorf-cloud/solobill/internal/logo"
	"github.com/ostendorf-cloud/solobill/internal/numbering"
	"github.com/ostendorf-cloud/solobill/internal/objectstore"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// registerPDF mounts the PDF of a document. Owned by issue #74 (I8).
// For an issued document it is a file route: the first download renders
// once from the frozen payload and stores the PDF as a files row of
// purpose document_pdf; every later download returns those stored bytes.
// It answers 503 object_store_unavailable while the bucket is gated.
// Issuing never depends on the bucket. See docs/api.md, "PDF".
func registerPDF(mux *http.ServeMux, route routeFunc, a *app) {
	mux.Handle("GET /api/v1/documents/{id}/pdf", route(api.Handle(a.handleDocumentPDF)))
}

// documentPDFNamespace derives the id of a document's stored PDF from
// the document's id (UUID version 5). The files row needs no link column
// on the immutable document: the id alone finds it, and the primary key
// of files lets only one first download store it.
var documentPDFNamespace = uuid.MustParse("5d0f3c8e-7a41-4b6f-9e2d-3c1a8b7f6e05")

// documentPDFFileID is the files row id of document id's stored PDF.
func documentPDFFileID(id uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(documentPDFNamespace, id[:])
}

// documentPDFName is the download name: numbering.FileName of the stored
// display number for an issued document, "draft-<id>.pdf" for a draft.
func documentPDFName(d storage.Document) string {
	if d.State == storage.DocumentStateDraft {
		return "draft-" + d.ID.String() + ".pdf"
	}
	display := ""
	if d.DisplayNumber != nil {
		display = *d.DisplayNumber
	}

	return numbering.FileName(display)
}

// rendererFieldDetails is a renderer refusal that names the fields it
// cannot print (internal/pdf.InvalidPayloadError).
type rendererFieldDetails interface {
	FieldDetails() map[string]string
}

// rendererUnavailableError is a renderer that cannot run, for example
// without its binary (internal/pdf.ErrUnavailable).
type rendererUnavailableError interface {
	RendererUnavailable() bool
}

// rendererOf returns r unless it is nil or a typed nil (a nil pointer
// stored in the interface), which would panic or render nothing.
func rendererOf(r DocumentRenderer) (DocumentRenderer, bool) {
	if r == nil {
		return nil, false
	}
	if v := reflect.ValueOf(r); v.Kind() == reflect.Pointer && v.IsNil() {
		return nil, false
	}

	return r, true
}

// pdfRendererUnavailable is the 503 of the PDF download while no
// renderer is wired or the wired one cannot run.
func pdfRendererUnavailable() *api.Error {
	return api.New(http.StatusServiceUnavailable, codeRendererUnavailable,
		"the PDF renderer is not available, try again later").Exposed()
}

// errLogoConflict is the 409 of a logo the frozen payload names by
// SHA-256 that the bucket does not hold with exactly that hash.
func errLogoConflict() error {
	return api.Conflict("the logo this document names by SHA-256 is missing or was changed; the PDF cannot be rendered as issued")
}

// pdfSource is what one download needs, read in the tenant scope.
type pdfSource struct {
	doc     storage.Document
	payload []byte
	draft   bool
	// stored is the files row of the stored PDF, nil before the first
	// download of an issued document and for a draft.
	stored *storage.File
}

// handleDocumentPDF answers GET /api/v1/documents/{id}/pdf. Who: any.
func (a *app) handleDocumentPDF(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.documentsCaller(w, r, false)
	if !ok {
		return err
	}
	// A file route for the first and every later download, and for a
	// draft: nothing is read or rendered while the bucket is gated.
	if err := a.requireFileRoutes(); err != nil {
		return err
	}
	id, err := documentID(r)
	if err != nil {
		return err
	}

	src, err := a.loadPDFSource(r.Context(), c, id)
	if err != nil {
		return err
	}
	name := documentPDFName(src.doc)
	if src.stored != nil {
		// A stored PDF needs no renderer: it stays downloadable while none
		// is configured.
		return a.serveStoredFile(w, r, *src.stored, func(h http.Header) {
			h.Set("Content-Disposition", attachmentDisposition(name, contentTypePDF))
		})
	}
	renderer, ok := rendererOf(a.repos.Renderer)
	if !ok {
		return pdfRendererUnavailable()
	}

	logo, err := a.documentLogo(r, c, src.payload)
	if err != nil {
		return err
	}
	pdf, err := renderer.RenderWithLogo(r.Context(), src.payload, src.draft, logo)
	if err != nil {
		return renderError(err, src.draft)
	}
	if !bytes.HasPrefix(pdf, magicPDF) {
		return api.Internal(fmt.Errorf("httpapi: the renderer returned no PDF for document %s", id))
	}

	if src.draft {
		// A draft renders live and is never stored.
		return writePDF(w, name, pdf)
	}

	stored, err := a.storeDocumentPDF(r.Context(), c, src.doc, name, pdf)
	if err != nil {
		return err
	}
	if stored != nil {
		// Another download stored the PDF first: serve what is stored.
		return a.serveStoredFile(w, r, *stored, func(h http.Header) {
			h.Set("Content-Disposition", attachmentDisposition(name, contentTypePDF))
		})
	}

	return writePDF(w, name, pdf)
}

// loadPDFSource reads the document and, for an issued one, its stored
// PDF, in the caller's tenant. A draft gets the payload it prints now.
func (a *app) loadPDFSource(ctx context.Context, c caller, id uuid.UUID) (pdfSource, error) {
	var src pdfSource
	err := a.documentsInTenant(ctx, c, func(ctx context.Context) error {
		d, err := a.repos.Documents.FindByID(ctx, id)
		if err != nil {
			return err
		}
		// Row level security hides another tenant's document already;
		// a store that ever returned one still answers 404.
		if d.TenantID != c.tenantID {
			logging.FromContext(ctx).ErrorContext(ctx, "document of another tenant returned by the repository",
				"document_id", d.ID, "tenant_id", c.tenantID)

			return api.NotFound("not found")
		}
		src.doc = d

		switch d.State {
		case storage.DocumentStateDraft:
			src.draft = true
			src.payload, err = a.draftPayload(ctx, d)

			return err
		case storage.DocumentStateIssued:
		default:
			return api.Internal(fmt.Errorf("httpapi: document %s has state %q", d.ID, d.State))
		}

		if d.Imported {
			return api.Conflict("an imported document has no rendered PDF: its original PDF is the document").
				WithDetails(map[string]any{"imported": true})
		}
		src.payload = d.FrozenPayload

		file, err := a.repos.Files.FindByID(ctx, documentPDFFileID(d.ID))
		switch {
		case errors.Is(err, storage.ErrNotFound):
			return nil
		case err != nil:
			return fmt.Errorf("find the stored PDF: %w", err)
		}
		if err := checkStoredPDF(file, c.tenantID); err != nil {
			return api.Internal(err)
		}
		src.stored = &file

		return nil
	})

	return src, err
}

// checkStoredPDF refuses a files row under a document PDF's id that is
// not one: another tenant, another purpose or another key.
func checkStoredPDF(file storage.File, tenantID uuid.UUID) error {
	switch {
	case file.TenantID != tenantID:
		return fmt.Errorf("httpapi: stored PDF %s belongs to another tenant", file.ID)
	case file.Purpose != storage.FilePurposeDocumentPDF:
		return fmt.Errorf("httpapi: file %s has purpose %q, want %q", file.ID, file.Purpose, storage.FilePurposeDocumentPDF)
	case file.ObjectKey != objectstore.Key(tenantID, file.ID):
		return fmt.Errorf("httpapi: stored PDF %s has object key %q", file.ID, file.ObjectKey)
	}

	return nil
}

// draftPayload is the marshalled payload draft d prints now.
func (a *app) draftPayload(ctx context.Context, d storage.Document) ([]byte, error) {
	profile, err := a.repos.SellerProfiles.Get(ctx)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, fmt.Errorf("seller profile: %w", err)
	}
	// Without a profile the renderer names every missing seller field.
	p, err := a.issuer(nil).DraftPayload(ctx, d, profile, time.Now())
	if err != nil {
		return nil, err
	}
	raw, err := docpayload.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("httpapi: marshal draft payload: %w", err)
	}

	return raw, nil
}

// documentLogo reads the logo the payload names by SHA-256 from the
// bucket, nil when it names none. The file is found by that hash, never
// by a name or a profile setting, and the bytes read must hash to it:
// anything else is 409 conflict.
func (a *app) documentLogo(r *http.Request, c caller, payload []byte) ([]byte, error) {
	p, err := docpayload.Unmarshal(payload)
	if err != nil {
		return nil, api.Internal(fmt.Errorf("httpapi: payload: %w", err))
	}
	want := p.Seller.LogoSHA256
	if want == "" {
		return nil, nil
	}

	var logo *storage.File
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		files, err := a.repos.Files.FindBySHA256(ctx, want)
		if err != nil {
			return err
		}
		for _, f := range files {
			if f.Purpose == storage.FilePurposeLogo && f.TenantID == c.tenantID && f.SHA256 == want &&
				f.ObjectKey == objectstore.Key(c.tenantID, f.ID) {
				logo = &f

				break
			}
		}

		return nil
	})
	if err != nil {
		return nil, api.Internal(fmt.Errorf("httpapi: find logo %s: %w", want, err))
	}
	if logo == nil {
		logging.FromContext(r.Context()).WarnContext(r.Context(), "logo named by a payload not found",
			"tenant_id", c.tenantID, "logo_sha256", want)

		return nil, errLogoConflict()
	}

	body, _, err := readStoredObject(r.Context(), a.repos.Objects, *logo)
	if errors.Is(err, storage.ErrNotFound) {
		logIntegrity(r, *logo, "object missing from the store")

		return nil, errLogoConflict()
	}
	if err != nil {
		return nil, mapObjectStoreError(err)
	}
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(io.LimitReader(body, logo.SizeBytes+1))
	if err != nil {
		return nil, mapObjectStoreError(fmt.Errorf("httpapi: read logo %s: %w", logo.ID, err))
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		logIntegrity(r, *logo, fmt.Sprintf("logo has SHA-256 %s, the payload names %s", got, want))

		return nil, errLogoConflict()
	}
	// The upload checked it too; a logo stored before that check existed
	// is decoded within the pixel budget here before Typst sees it.
	if err := logocheck.Check(data); err != nil {
		logging.FromContext(r.Context()).WarnContext(r.Context(), "stored logo cannot be printed",
			"file_id", logo.ID, "tenant_id", c.tenantID, "reason", err.Error())

		return nil, api.Conflict("the logo this document names cannot be printed")
	}

	return data, nil
}

// renderError maps a renderer failure. A draft the template cannot
// print is 400 with the fields. An issued payload passed the same check
// when it was issued, so a refusal there is an internal error.
func renderError(err error, draft bool) error {
	var fields rendererFieldDetails
	var unavailable rendererUnavailableError
	switch {
	case errors.As(err, &unavailable) && unavailable.RendererUnavailable():
		return pdfRendererUnavailable()
	case draft && errors.As(err, &fields):
		return profileInvalid("the draft cannot be printed yet", fields.FieldDetails())
	default:
		return api.Internal(fmt.Errorf("httpapi: render document: %w", err))
	}
}

// storeDocumentPDF puts the rendered PDF into the bucket and writes its
// files row under documentPDFFileID. When another download wrote the
// row first, it returns that row and the caller serves it; otherwise it
// returns nil.
func (a *app) storeDocumentPDF(ctx context.Context, c caller, d storage.Document, name string, pdf []byte) (*storage.File, error) {
	id := documentPDFFileID(d.ID)
	key := objectstore.Key(c.tenantID, id)
	sum := sha256.Sum256(pdf)
	hash := hex.EncodeToString(sum[:])
	now := time.Now().UTC()
	retain := objectstore.RetainUntil(now)

	info, err := a.repos.Objects.Put(ctx, storage.PutObject{
		Key:         key,
		Body:        bytes.NewReader(pdf),
		SizeBytes:   int64(len(pdf)),
		ContentType: contentTypePDF,
		SHA256:      hash,
		RetainUntil: &retain,
	})
	if err != nil {
		return nil, mapObjectStoreError(fmt.Errorf("httpapi: store the PDF of document %s: %w", d.ID, err))
	}

	uploader := c.accountID
	file := storage.File{
		ID:               id,
		TenantID:         c.tenantID,
		Purpose:          storage.FilePurposeDocumentPDF,
		ObjectKey:        key,
		SHA256:           hash,
		SizeBytes:        int64(len(pdf)),
		ContentType:      contentTypePDF,
		OriginalFilename: name,
		UploadedBy:       &uploader,
		UploadedAt:       now,
		RetainUntil:      info.RetainUntil,
	}
	if info.VersionID != "" {
		version := info.VersionID
		file.ObjectVersionID = &version
	}

	err = a.inTenant(ctx, c.tenantID, func(ctx context.Context) error {
		_, err := a.repos.Files.Create(ctx, file)

		return err
	})
	if err == nil {
		return nil, nil
	}
	if !errors.Is(err, storage.ErrConflict) {
		return nil, api.Internal(fmt.Errorf("httpapi: store the PDF of document %s: %w", d.ID, err))
	}

	// Another first download stored the PDF while this one rendered. The
	// unique violation aborted the insert's transaction, so the stored
	// row is read in a new one. This download's Put left one more version
	// of the key behind. It is harmless: the row names the winner's
	// version, a versioned read never falls back to the current object,
	// and without versioning (local development only) the current object
	// holds the same bytes, because rendering is reproducible. Putting
	// after the insert instead would leave a row without its object
	// whenever the Put fails, and a files row is never deleted.
	var first storage.File
	err = a.inTenant(ctx, c.tenantID, func(ctx context.Context) error {
		var err error
		first, err = a.repos.Files.FindByID(ctx, id)

		return err
	})
	if err != nil {
		return nil, api.Internal(fmt.Errorf("httpapi: read the PDF of document %s stored first: %w", d.ID, err))
	}
	if err := checkStoredPDF(first, c.tenantID); err != nil {
		return nil, api.Internal(err)
	}

	return &first, nil
}

// writePDF writes freshly rendered bytes with the headers every stored
// file is served with.
func writePDF(w http.ResponseWriter, name string, pdf []byte) error {
	h := w.Header()
	h.Set("Content-Type", contentTypePDF)
	h.Set("Content-Length", strconv.Itoa(len(pdf)))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", downloadCSP)
	h.Set("Cache-Control", "private, no-store")
	h.Set("Content-Disposition", attachmentDisposition(name, contentTypePDF))
	w.WriteHeader(http.StatusOK)
	// The client went away when this fails; nobody is left to answer.
	_, _ = w.Write(pdf)

	return nil
}
