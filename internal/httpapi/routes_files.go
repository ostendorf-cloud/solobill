package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/api"
	"github.com/ostendorf-cloud/govue/logging"

	"github.com/ostendorf-cloud/solobill/internal/objectstore"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// Upload limits of POST /api/v1/files, from docs/api.md, "Files". The
// body limit covers the multipart framing and the purpose part on top of
// the file part.
const (
	maxFileBytes       int64 = 10 << 20
	maxFileUploadBytes       = maxFileBytes + 64<<10
	// maxPurposeBytes bounds the purpose part, a short enum value.
	maxPurposeBytes = 64
	// maxFilenameRunes bounds the stored original file name, matching
	// files_original_filename_length in the migration.
	maxFilenameRunes = 255
)

// Content types the allowlist admits, sniffed from the magic bytes.
const (
	contentTypePDF  = "application/pdf"
	contentTypeJPEG = "image/jpeg"
	contentTypePNG  = "image/png"
	contentTypeXML  = "application/xml"
)

// codeObjectStoreUnavailable is the 503 of a file route while the bucket
// is gated. govue's api package has no constant for it.
const codeObjectStoreUnavailable = "object_store_unavailable"

// downloadCSP is the Content-Security-Policy of every stored file served
// back: sandbox (docs/api.md) plus a default-src that loads nothing, so a
// browser that renders the bytes anyway runs no script and fetches nothing.
const downloadCSP = "sandbox; default-src 'none'"

// verifiedChunkSize is the read size of a verified download. The last
// chunk read is held back until the SHA-256 of the whole object matched,
// so a tampered object never reaches the client complete. An object of at
// most this size is refused with a clean 500 instead of a truncated body.
const verifiedChunkSize = 32 << 10

// registerFiles mounts receipt and file upload and download. Owned by
// issue #75 (I9). See docs/api.md, "Files".
//
// Every handler that touches the object store calls requireFileRoutes
// first, and the store itself (objectstore.Guarded) refuses while the
// bucket is gated, so a later file route (the PDF download of #74, the
// import attachment of #81) answers the same 503 by calling it too.
func registerFiles(mux *http.ServeMux, route routeFunc, a *app) {
	mux.Handle("POST /api/v1/files", route(api.Handle(a.handleUploadFile)))
	mux.Handle("GET /api/v1/files/{id}", route(api.Handle(a.handleGetFile)))
	mux.Handle("GET /api/v1/files/{id}/content", route(api.Handle(a.handleFileContent)))
}

// objectStoreStatusReporter is what the store in Repositories.Objects
// reports its last bucket check through. objectstore.Guarded implements
// it. A store without it is treated as gated: the file routes fail closed.
type objectStoreStatusReporter interface {
	Status() storage.ObjectStoreStatus
}

// objectStoreStatus returns the last bucket check, the zero status (never
// checked, gated) when no store or no reporter is wired.
func (a *app) objectStoreStatus() storage.ObjectStoreStatus {
	reporter, ok := a.repos.Objects.(objectStoreStatusReporter)
	if !ok {
		return storage.ObjectStoreStatus{}
	}

	return reporter.Status()
}

// errObjectStoreUnavailable is the one 503 every file route answers while
// the bucket is gated. Exposed: a static message, no cause.
func errObjectStoreUnavailable() error {
	return api.New(http.StatusServiceUnavailable, codeObjectStoreUnavailable,
		"the object store is unavailable or not protected as required, try again later").Exposed()
}

// requireFileRoutes answers 503 object_store_unavailable unless the last
// bucket check allows the file routes, and 501 while the file repository
// is not wired. Call it after the session check and before reading a body.
func (a *app) requireFileRoutes() error {
	if a.repos.Files == nil {
		return api.New(http.StatusNotImplemented, codeNotImplemented, "this endpoint is reserved and not implemented yet").Exposed()
	}
	if a.repos.Objects == nil || !a.objectStoreStatus().FileRoutesAvailable() {
		return errObjectStoreUnavailable()
	}

	return nil
}

// mapObjectStoreError turns a store failure into the API error: the gate
// into 503 object_store_unavailable, everything else into a 500.
func mapObjectStoreError(err error) error {
	if errors.Is(err, storage.ErrObjectStoreUnavailable) {
		return errObjectStoreUnavailable()
	}

	return api.Internal(err)
}

// fileJSON is the file JSON of docs/api.md, "Files".
type fileJSON struct {
	ID               uuid.UUID  `json:"id"`
	Purpose          string     `json:"purpose"`
	SHA256           string     `json:"sha256"`
	SizeBytes        int64      `json:"size_bytes"`
	ContentType      string     `json:"content_type"`
	OriginalFilename string     `json:"original_filename"`
	UploadedAt       time.Time  `json:"uploaded_at"`
	ObjectVersionID  *string    `json:"object_version_id"`
	RetainUntil      *time.Time `json:"retain_until"`
}

func toFileJSON(f storage.File) fileJSON {
	out := fileJSON{
		ID:               f.ID,
		Purpose:          string(f.Purpose),
		SHA256:           f.SHA256,
		SizeBytes:        f.SizeBytes,
		ContentType:      f.ContentType,
		OriginalFilename: f.OriginalFilename,
		UploadedAt:       f.UploadedAt.UTC(),
		ObjectVersionID:  f.ObjectVersionID,
	}
	if f.RetainUntil != nil {
		retain := f.RetainUntil.UTC()
		out.RetainUntil = &retain
	}

	return out
}

// uploadResponse is the 201 body of POST /api/v1/files.
type uploadResponse struct {
	File       fileJSON   `json:"file"`
	Duplicates []fileJSON `json:"duplicates"`
}

// handleUploadFile is POST /api/v1/files. Who: any.
func (a *app) handleUploadFile(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	// The gate comes before the body: while the bucket is gated nothing is
	// read and nothing is stored.
	if err := a.requireFileRoutes(); err != nil {
		return err
	}

	up, err := readUpload(w, r, maxFileUploadBytes, maxFileBytes, true)
	if err != nil {
		return err
	}

	var purpose storage.FilePurpose
	switch storage.FilePurpose(up.purpose) {
	case storage.FilePurposeReceipt, storage.FilePurposeImportOriginal:
		purpose = storage.FilePurpose(up.purpose)
	default:
		return api.Invalid("purpose must be receipt or import_original").
			WithDetails(map[string]any{"purpose": "must be receipt or import_original"})
	}

	contentType := sniffContentType(up.data)
	if contentType == "" {
		return errUnsupportedType("only PDF, JPEG, PNG and XML files are accepted")
	}

	file, duplicates, err := a.storeFile(r.Context(), c, purpose, contentType, up, nil)
	if err != nil {
		return err
	}

	resp := uploadResponse{File: toFileJSON(file), Duplicates: make([]fileJSON, 0, len(duplicates))}
	for _, d := range duplicates {
		resp.Duplicates = append(resp.Duplicates, toFileJSON(d))
	}

	return api.Write(w, http.StatusCreated, resp)
}

// handleGetFile is GET /api/v1/files/{id}. Who: any. It reads metadata
// only, so it does not depend on the bucket.
func (a *app) handleGetFile(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if a.repos.Files == nil {
		return a.notImplemented(w, r)
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return api.NotFound("no such file")
	}

	file, err := a.findFile(r.Context(), c, id)
	if err != nil {
		return err
	}

	return api.Write(w, http.StatusOK, toFileJSON(file))
}

// handleFileContent is GET /api/v1/files/{id}/content. Who: any. The
// bytes are served as an attachment, never rendered by this origin, and
// verified against the stored SHA-256 while they stream.
func (a *app) handleFileContent(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if err := a.requireFileRoutes(); err != nil {
		return err
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return api.NotFound("no such file")
	}

	file, err := a.findFile(r.Context(), c, id)
	if err != nil {
		return err
	}

	return a.serveStoredFile(w, r, file, func(h http.Header) {
		h.Set("Content-Disposition", attachmentDisposition(file.OriginalFilename))
	})
}

// findFile reads the file row in the caller's tenant. Row level security
// already hides another tenant's row; the tenant and the object key are
// checked again here, so a store that ever returned a foreign row still
// answers 404 and never serves its object.
func (a *app) findFile(ctx context.Context, c caller, id uuid.UUID) (storage.File, error) {
	var file storage.File
	err := a.repos.Scope.WithTenant(ctx, c.tenantID, func(ctx context.Context) error {
		var err error
		file, err = a.repos.Files.FindByID(ctx, id)

		return err
	})
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return storage.File{}, api.NotFound("no such file")
	case err != nil:
		return storage.File{}, api.Internal(fmt.Errorf("httpapi: find file: %w", err))
	}

	if file.TenantID != c.tenantID {
		logging.FromContext(ctx).ErrorContext(ctx, "file of another tenant returned by the repository",
			"file_id", file.ID, "tenant_id", c.tenantID)

		return storage.File{}, api.NotFound("no such file")
	}
	if file.ObjectKey != objectstore.Key(c.tenantID, file.ID) {
		return storage.File{}, api.Internal(fmt.Errorf("httpapi: file %s has object key %q, want %q",
			file.ID, file.ObjectKey, objectstore.Key(c.tenantID, file.ID)))
	}

	return file, nil
}

// serveStoredFile streams file's object to w with the headers every
// stored file is served with, plus whatever extra adds, and verifies it on
// the way: see writeVerified.
func (a *app) serveStoredFile(w http.ResponseWriter, r *http.Request, file storage.File, extra func(http.Header)) error {
	body, info, err := a.repos.Objects.Get(r.Context(), file.ObjectKey)
	if errors.Is(err, storage.ErrNotFound) {
		// A row without its object is an integrity failure, not a 404: the
		// file existed and is gone from the store.
		logIntegrity(r, file, "object missing from the store")

		return api.Internal(fmt.Errorf("httpapi: object of file %s is missing: %w", file.ID, err))
	}
	if err != nil {
		return mapObjectStoreError(err)
	}
	defer func() { _ = body.Close() }()

	if info.SizeBytes != file.SizeBytes {
		logIntegrity(r, file, fmt.Sprintf("store reports %d bytes, the files row %d", info.SizeBytes, file.SizeBytes))

		return api.Internal(fmt.Errorf("httpapi: object of file %s: %w", file.ID, objectstore.ErrIntegrity))
	}

	return writeVerified(w, r, file, body, func(h http.Header) {
		h.Set("Content-Type", file.ContentType)
		h.Set("Content-Length", strconv.FormatInt(file.SizeBytes, 10))
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", downloadCSP)
		h.Set("Cache-Control", "private, no-store")
		if extra != nil {
			extra(h)
		}
	})
}

// writeVerified copies body to w while it computes the SHA-256, and holds
// the last chunk back until the whole object is read and matches file's
// SHA-256 and size. On a mismatch it logs an integrity error and either
// returns a 500 (nothing written yet) or aborts the response, so the
// client sees a truncated download and never wrong bytes as complete.
// Headers are set only right before the first byte is written.
func writeVerified(w http.ResponseWriter, r *http.Request, file storage.File, body io.Reader, setHeaders func(http.Header)) error {
	hash := sha256.New()
	src := io.TeeReader(io.LimitReader(body, file.SizeBytes+1), hash)

	held := make([]byte, verifiedChunkSize)
	next := make([]byte, verifiedChunkSize)
	heldN := 0
	var total int64
	started := false

	start := func() {
		setHeaders(w.Header())
		w.WriteHeader(http.StatusOK)
		started = true
	}
	fail := func(err error) error {
		if !started {
			return api.Internal(err)
		}
		// The status line is out: the only honest signal left is to cut
		// the connection short of Content-Length.
		panic(http.ErrAbortHandler)
	}

	for {
		n, err := io.ReadFull(src, next)
		total += int64(n)
		if n > 0 {
			if heldN > 0 {
				if !started {
					start()
				}
				if _, werr := w.Write(held[:heldN]); werr != nil {
					// The client went away; there is nobody left to answer.
					return nil
				}
			}
			held, next = next, held
			heldN = n
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			return fail(fmt.Errorf("httpapi: read object of file %s: %w", file.ID, err))
		}
	}

	if err := objectstore.Compare(hash.Sum(nil), total, file.SHA256, file.SizeBytes); err != nil {
		logIntegrity(r, file, err.Error())

		return fail(fmt.Errorf("httpapi: object of file %s: %w", file.ID, err))
	}

	if !started {
		start()
	}
	_, _ = w.Write(held[:heldN])

	return nil
}

// logIntegrity logs a stored object that does not match its files row, at
// ERROR, with the file id so an operator can restore the version.
func logIntegrity(r *http.Request, file storage.File, reason string) {
	logging.FromContext(r.Context()).ErrorContext(r.Context(), "file integrity check failed",
		"file_id", file.ID, "tenant_id", file.TenantID, "reason", reason)
}

// attachmentDisposition is the Content-Disposition of a download: always
// attachment, with the stored file name as an RFC 2231 encoded parameter
// that mime.FormatMediaType quotes or escapes. A name it cannot encode is
// dropped, never spliced in by hand.
func attachmentDisposition(filename string) string {
	if filename != "" {
		if v := mime.FormatMediaType("attachment", map[string]string{"filename": filename}); v != "" {
			return v
		}
	}

	return "attachment"
}

// upload is one parsed multipart upload, held in memory: nothing is
// written to disk, and the file part is bounded by the caller's limit.
type upload struct {
	data     []byte
	sha256   string
	filename string
	purpose  string
}

// readUpload reads a multipart/form-data body with one part "file" and,
// when withPurpose, one part "purpose". The whole body is limited to
// maxBody by http.MaxBytesReader and the file part to maxFile, counted
// while streaming. The SHA-256 is computed on the same pass. The client's
// part Content-Type is never read.
func readUpload(w http.ResponseWriter, r *http.Request, maxBody, maxFile int64, withPurpose bool) (upload, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return upload{}, errUnsupportedType("the request must be multipart/form-data")
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	reader, err := r.MultipartReader()
	if err != nil {
		return upload{}, api.Invalid("malformed multipart body")
	}

	var (
		up          upload
		seenFile    bool
		seenPurpose bool
	)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return upload{}, uploadReadError(err)
		}

		// The part name is never echoed back: it is client input of any
		// length.
		switch name := part.FormName(); {
		case name == "file" && !seenFile:
			seenFile = true
			hash := sha256.New()
			var buf bytes.Buffer
			n, err := io.Copy(io.MultiWriter(&buf, hash), io.LimitReader(part, maxFile+1))
			if err != nil {
				return upload{}, uploadReadError(err)
			}
			if n > maxFile {
				return upload{}, errTooLarge(fmt.Sprintf("the file is larger than %d bytes", maxFile))
			}
			up.data = buf.Bytes()
			up.sha256 = hex.EncodeToString(hash.Sum(nil))
			up.filename = cleanFilename(part.FileName())
		case name == "purpose" && withPurpose && !seenPurpose:
			seenPurpose = true
			value, err := io.ReadAll(io.LimitReader(part, maxPurposeBytes+1))
			if err != nil {
				return upload{}, uploadReadError(err)
			}
			if len(value) > maxPurposeBytes {
				return upload{}, api.Invalid("purpose is too long").WithDetails(map[string]any{"purpose": "too long"})
			}
			up.purpose = string(value)
		default:
			return upload{}, api.Invalid("unexpected or repeated multipart part")
		}
		_ = part.Close()
	}

	if !seenFile || len(up.data) == 0 {
		return upload{}, api.Invalid("the file part is missing or empty").WithDetails(map[string]any{"file": "required"})
	}
	if withPurpose && !seenPurpose {
		return upload{}, api.Invalid("the purpose part is missing").WithDetails(map[string]any{"purpose": "required"})
	}

	return up, nil
}

// uploadReadError maps a read failure of the body: over the body limit is
// 413, anything else a malformed request.
func uploadReadError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return errTooLarge("the request body is too large")
	}

	return api.Invalid("malformed multipart body")
}

func errTooLarge(message string) error {
	return api.New(http.StatusRequestEntityTooLarge, api.CodeRequestTooLarge, message)
}

func errUnsupportedType(message string) error {
	return api.New(http.StatusUnsupportedMediaType, api.CodeUnsupportedMediaType, message)
}

// Magic bytes of the allowlist.
var (
	magicPDF  = []byte("%PDF-")
	magicPNG  = []byte("\x89PNG\r\n\x1a\n")
	magicJPEG = []byte{0xFF, 0xD8, 0xFF}
	magicXML  = []byte("<?xml")
	utf8BOM   = []byte{0xEF, 0xBB, 0xBF}
)

// sniffContentType returns the content type of data by its magic bytes,
// or "" for anything outside the allowlist (PDF, JPEG, PNG, XML). The
// client's file name and content type are never consulted. XML is
// recognised by its declaration, optionally after a UTF-8 byte order mark
// and whitespace; it is stored, never parsed.
func sniffContentType(data []byte) string {
	switch {
	case bytes.HasPrefix(data, magicPDF):
		return contentTypePDF
	case bytes.HasPrefix(data, magicPNG):
		return contentTypePNG
	case bytes.HasPrefix(data, magicJPEG):
		return contentTypeJPEG
	}

	rest := bytes.TrimPrefix(data, utf8BOM)
	rest = bytes.TrimLeft(rest, " \t\r\n")
	if bytes.HasPrefix(rest, magicXML) && len(rest) > len(magicXML) && isXMLSpace(rest[len(magicXML)]) {
		return contentTypeXML
	}

	return ""
}

func isXMLSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

// cleanFilename keeps the client's file name as display data only: valid
// UTF-8, no control characters, at most maxFilenameRunes runes. It is
// never a path or a key; mime/multipart already dropped any directory.
func cleanFilename(name string) string {
	name = strings.ToValidUTF8(name, "�")
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}

		return r
	}, name)
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > maxFilenameRunes {
		name = string([]rune(name)[:maxFilenameRunes])
	}

	return name
}

// storeFile puts the upload into the object store under a generated key
// and writes its files row, together with whatever after adds in the same
// tenant transaction. It returns the row and the earlier files with the
// same SHA-256 (the duplicate warning). An object stored before a failed
// row insert stays an orphan, which the contract accepts: it is never
// referenced (storage.ObjectStore).
func (a *app) storeFile(ctx context.Context, c caller, purpose storage.FilePurpose, contentType string, up upload,
	after func(ctx context.Context, file storage.File) error,
) (storage.File, []storage.File, error) {
	id := uuid.New()
	key := objectstore.Key(c.tenantID, id)
	now := time.Now().UTC()
	retain := objectstore.RetainUntil(now)

	info, err := a.repos.Objects.Put(ctx, storage.PutObject{
		Key:         key,
		Body:        bytes.NewReader(up.data),
		SizeBytes:   int64(len(up.data)),
		ContentType: contentType,
		SHA256:      up.sha256,
		RetainUntil: &retain,
	})
	if err != nil {
		return storage.File{}, nil, mapObjectStoreError(fmt.Errorf("httpapi: store file %s: %w", id, err))
	}

	uploader := c.accountID
	file := storage.File{
		ID:               id,
		TenantID:         c.tenantID,
		Purpose:          purpose,
		ObjectKey:        key,
		SHA256:           up.sha256,
		SizeBytes:        int64(len(up.data)),
		ContentType:      contentType,
		OriginalFilename: up.filename,
		UploadedBy:       &uploader,
		UploadedAt:       now,
		RetainUntil:      info.RetainUntil,
	}
	if info.VersionID != "" {
		version := info.VersionID
		file.ObjectVersionID = &version
	}

	var duplicates []storage.File
	err = a.repos.Scope.WithTenant(ctx, c.tenantID, func(ctx context.Context) error {
		var err error
		if duplicates, err = a.repos.Files.FindBySHA256(ctx, up.sha256); err != nil {
			return fmt.Errorf("find duplicates: %w", err)
		}
		if file, err = a.repos.Files.Create(ctx, file); err != nil {
			return fmt.Errorf("create file row: %w", err)
		}
		if after != nil {
			return after(ctx, file)
		}

		return nil
	})
	if err != nil {
		var apiErr *api.Error
		if errors.As(err, &apiErr) {
			return storage.File{}, nil, apiErr
		}

		return storage.File{}, nil, api.Internal(fmt.Errorf("httpapi: store file %s: %w", id, err))
	}

	return file, duplicates, nil
}
