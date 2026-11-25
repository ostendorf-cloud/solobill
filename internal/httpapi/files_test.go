package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/csrf"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/objectstore"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// samplePDF is a minimal body the sniffer accepts as PDF.
var samplePDF = []byte("%PDF-1.7\n1 0 obj << >> endobj\n%%EOF\n")

// samplePNG is a real 24 by 8 pixel PNG: the logo upload decodes it.
var samplePNG = encodeTestPNG(24, 8)

// encodeTestPNG encodes a w by h PNG with one red pixel.
func encodeTestPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}

	return buf.Bytes()
}

func sha256Of(b []byte) string {
	sum := sha256.Sum256(b)

	return hex.EncodeToString(sum[:])
}

// formPart is one part of a multipart test body. contentType is what the
// client claims, which the server must ignore.
type formPart struct {
	name, filename, contentType string
	data                        []byte
}

func multipartBody(t *testing.T, parts ...formPart) (*bytes.Buffer, string) {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		h := textproto.MIMEHeader{}
		disposition := `form-data; name="` + p.name + `"`
		if p.filename != "" {
			disposition += `; filename="` + p.filename + `"`
		}
		h.Set("Content-Disposition", disposition)
		if p.contentType != "" {
			h.Set("Content-Type", p.contentType)
		}
		w, err := mw.CreatePart(h)
		if err != nil {
			t.Fatalf("create part: %v", err)
		}
		if _, err := w.Write(p.data); err != nil {
			t.Fatalf("write part: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	return &buf, mw.FormDataContentType()
}

func doRequest(t *testing.T, h http.Handler, method, path string, body io.Reader, contentType string, cookie *http.Cookie, token string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if token != "" {
		req.Header.Set(csrf.HeaderName, token)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

// fileCaller is one logged in caller.
type fileCaller struct {
	tenant storage.Tenant
	cookie *http.Cookie
	token  string
}

func ownerSession(t *testing.T, app testApp) fileCaller {
	t.Helper()

	tenant, secret := seedActiveAccount(t, app.store)
	cookie, token := loginAndCSRF(t, app, tenant.Slug, ownerEmail, ownerPassword, secret)

	return fileCaller{tenant: tenant, cookie: cookie, token: token}
}

func memberSession(t *testing.T, app testApp, slug string) fileCaller {
	t.Helper()

	tenant := app.store.addTenant(storage.Tenant{Slug: slug, Name: slug})
	email := "member@" + slug + ".test"
	_, secret, _ := seedMember(t, app.store, tenant, email, memberPassword)
	cookie, token := loginAndCSRF(t, app, tenant.Slug, email, memberPassword, secret)

	return fileCaller{tenant: tenant, cookie: cookie, token: token}
}

func upload(t *testing.T, app testApp, s fileCaller, purpose string, part formPart) *httptest.ResponseRecorder {
	t.Helper()

	parts := []formPart{part}
	if purpose != "" {
		parts = append([]formPart{{name: "purpose", data: []byte(purpose)}}, parts...)
	}
	body, contentType := multipartBody(t, parts...)

	return doRequest(t, app.handler, http.MethodPost, "/api/v1/files", body, contentType, s.cookie, s.token)
}

type uploadResult struct {
	File struct {
		ID               uuid.UUID `json:"id"`
		Purpose          string    `json:"purpose"`
		SHA256           string    `json:"sha256"`
		SizeBytes        int64     `json:"size_bytes"`
		ContentType      string    `json:"content_type"`
		OriginalFilename string    `json:"original_filename"`
		ObjectVersionID  *string   `json:"object_version_id"`
		RetainUntil      *string   `json:"retain_until"`
	} `json:"file"`
	Duplicates []json.RawMessage `json:"duplicates"`
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}

	return v
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	body := decode[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, rec)

	return body.Error.Code
}

func TestUploadStoresUnderAGeneratedKeyWithTheServerHash(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	fx, objects, _ := filesOf(t, app)
	s := ownerSession(t, app)

	// The client lies about the type and sends a path as the file name.
	rec := upload(t, app, s, "receipt", formPart{
		name: "file", filename: "../../etc/rechnung.png", contentType: "image/png", data: samplePDF,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body = %s", rec.Code, rec.Body.String())
	}

	got := decode[uploadResult](t, rec)
	if got.File.SHA256 != sha256Of(samplePDF) || got.File.SizeBytes != int64(len(samplePDF)) {
		t.Errorf("sha256, size = %s, %d, want the server computed values", got.File.SHA256, got.File.SizeBytes)
	}
	if got.File.ContentType != "application/pdf" {
		t.Errorf("content_type = %q, want the sniffed application/pdf", got.File.ContentType)
	}
	if got.File.Purpose != "receipt" {
		t.Errorf("purpose = %q, want receipt", got.File.Purpose)
	}
	if strings.Contains(got.File.OriginalFilename, "/") {
		t.Errorf("original_filename = %q, want the base name only", got.File.OriginalFilename)
	}
	if got.File.ObjectVersionID == nil || got.File.RetainUntil == nil {
		t.Errorf("version, retain = %v, %v, want both from the store", got.File.ObjectVersionID, got.File.RetainUntil)
	}
	if len(got.Duplicates) != 0 {
		t.Errorf("duplicates = %d, want none", len(got.Duplicates))
	}

	key := objectstore.Key(s.tenant.ID, got.File.ID)
	stored, ok := objects.get(key)
	if !ok || !bytes.Equal(stored, samplePDF) {
		t.Fatalf("object under %q = %q, %v, want the uploaded bytes", key, stored, ok)
	}

	rows := fx.files.all()
	if len(rows) != 1 || rows[0].ObjectKey != key || rows[0].TenantID != s.tenant.ID {
		t.Fatalf("rows = %+v, want one row keyed %q", rows, key)
	}
	if rows[0].UploadedBy == nil {
		t.Error("uploaded_by = nil, want the caller")
	}
}

func TestUploadRefusesATypeOutsideTheAllowlist(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
	}{
		{"windows executable named pdf", append([]byte("MZ"), bytes.Repeat([]byte{0x90}, 32)...)},
		{"html", []byte("<html><script>alert(1)</script></html>")},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)},
		{"gif", []byte("GIF89a......")},
		{"pdf magic not at the start", append([]byte(" "), samplePDF...)},
		{"zip", []byte("PK\x03\x04rest")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := buildTestApp(t, 1000)
			fx, objects, _ := filesOf(t, app)
			s := ownerSession(t, app)

			rec := upload(t, app, s, "receipt", formPart{name: "file", filename: "rechnung.pdf", contentType: "application/pdf", data: tt.data})
			if rec.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("status = %d, want 415, body = %s", rec.Code, rec.Body.String())
			}
			if code := errorCode(t, rec); code != "unsupported_media_type" {
				t.Errorf("code = %q, want unsupported_media_type", code)
			}
			if objects.count() != 0 || len(fx.files.all()) != 0 {
				t.Error("a refused upload stored something")
			}
		})
	}
}

func TestUploadRefusesOversize(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	fx, objects, _ := filesOf(t, app)
	s := ownerSession(t, app)

	// One byte over the 10 MiB file limit, inside the body limit.
	big := append(bytes.Clone(samplePDF), bytes.Repeat([]byte{'a'}, 10<<20+1-len(samplePDF))...)
	rec := upload(t, app, s, "receipt", formPart{name: "file", filename: "big.pdf", data: big})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("file part over the limit: status = %d, want 413, body = %s", rec.Code, rec.Body.String())
	}

	// Exactly the limit is accepted.
	exact := big[:10<<20]
	rec = upload(t, app, s, "receipt", formPart{name: "file", filename: "exact.pdf", data: exact})
	if rec.Code != http.StatusCreated {
		t.Fatalf("file part at the limit: status = %d, want 201, body = %s", rec.Code, rec.Body.String())
	}

	// The whole body over its limit, through a multipart preamble the file
	// limit does not count.
	parts, contentType := multipartBody(t,
		formPart{name: "purpose", data: []byte("receipt")},
		formPart{name: "file", filename: "a.pdf", data: samplePDF},
	)
	preamble := bytes.Repeat([]byte("preamble line\r\n"), (10<<20+64<<10)/15+1)
	body := io.MultiReader(bytes.NewReader(preamble), parts)
	rec = doRequest(t, app.handler, http.MethodPost, "/api/v1/files", body, contentType, s.cookie, s.token)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("body over the limit: status = %d, want 413, body = %s", rec.Code, rec.Body.String())
	}

	if objects.count() != 1 || len(fx.files.all()) != 1 {
		t.Errorf("stored %d objects and %d rows, want only the one at the limit", objects.count(), len(fx.files.all()))
	}
}

func TestUploadRefusesMalformedRequests(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	_, objects, _ := filesOf(t, app)
	s := ownerSession(t, app)

	file := formPart{name: "file", filename: "a.pdf", data: samplePDF}
	tests := []struct {
		name       string
		parts      []formPart
		wantStatus int
	}{
		{"no purpose", []formPart{file}, http.StatusBadRequest},
		{"logo is not an upload purpose here", []formPart{{name: "purpose", data: []byte("logo")}, file}, http.StatusBadRequest},
		{"document_pdf is not an upload purpose", []formPart{{name: "purpose", data: []byte("document_pdf")}, file}, http.StatusBadRequest},
		{"no file", []formPart{{name: "purpose", data: []byte("receipt")}}, http.StatusBadRequest},
		{"empty file", []formPart{{name: "purpose", data: []byte("receipt")}, {name: "file", filename: "a.pdf"}}, http.StatusBadRequest},
		{"two files", []formPart{{name: "purpose", data: []byte("receipt")}, file, file}, http.StatusBadRequest},
		{"unknown part", []formPart{{name: "purpose", data: []byte("receipt")}, file, {name: "x", data: []byte("y")}}, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, contentType := multipartBody(t, tt.parts...)
			rec := doRequest(t, app.handler, http.MethodPost, "/api/v1/files", body, contentType, s.cookie, s.token)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d, body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}

	t.Run("not multipart", func(t *testing.T) {
		rec := doRequest(t, app.handler, http.MethodPost, "/api/v1/files", bytes.NewReader(samplePDF), "application/pdf", s.cookie, s.token)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("status = %d, want 415", rec.Code)
		}
	})

	t.Run("without csrf token", func(t *testing.T) {
		body, contentType := multipartBody(t, formPart{name: "purpose", data: []byte("receipt")}, file)
		rec := doRequest(t, app.handler, http.MethodPost, "/api/v1/files", body, contentType, s.cookie, "")
		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", rec.Code)
		}
	})

	if objects.count() != 0 {
		t.Errorf("stored %d objects, want none", objects.count())
	}
}

func TestUploadWarnsAboutADuplicate(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	s := ownerSession(t, app)

	first := upload(t, app, s, "receipt", formPart{name: "file", filename: "a.pdf", data: samplePDF})
	second := upload(t, app, s, "import_original", formPart{name: "file", filename: "b.pdf", data: samplePDF})
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("status = %d, %d, want 201 twice", first.Code, second.Code)
	}

	got := decode[uploadResult](t, second)
	if len(got.Duplicates) != 1 {
		t.Fatalf("duplicates = %d, want 1", len(got.Duplicates))
	}
	if got.File.ID == decode[uploadResult](t, first).File.ID {
		t.Error("the duplicate got the first file's id, want a new file")
	}
}

func TestDownloadServesTheVerifiedBytesAsAnAttachment(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	s := ownerSession(t, app)

	created := decode[uploadResult](t, upload(t, app, s, "receipt", formPart{name: "file", filename: "Rechnung März.pdf", data: samplePDF}))

	rec := doRequest(t, app.handler, http.MethodGet, "/api/v1/files/"+created.File.ID.String()+"/content", nil, "", s.cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), samplePDF) {
		t.Errorf("body = %q, want the uploaded bytes", rec.Body.Bytes())
	}

	h := rec.Result().Header
	if got := h.Get("Content-Type"); got != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", got)
	}
	if got := h.Get("Content-Disposition"); got != "attachment; filename=\"Rechnung M_rz.pdf\"; filename*=utf-8''Rechnung%20M%C3%A4rz.pdf" {
		t.Errorf("Content-Disposition = %q, want an attachment with the encoded name", got)
	}
	if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := h.Get("Content-Security-Policy"); !strings.HasPrefix(got, "sandbox") {
		t.Errorf("Content-Security-Policy = %q, want sandbox", got)
	}

	meta := doRequest(t, app.handler, http.MethodGet, "/api/v1/files/"+created.File.ID.String(), nil, "", s.cookie, "")
	if meta.Code != http.StatusOK {
		t.Fatalf("metadata status = %d, want 200", meta.Code)
	}
}

func TestDownloadOfAnotherTenantIsRefused(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	owner := ownerSession(t, app)
	other := memberSession(t, app, "other")

	created := decode[uploadResult](t, upload(t, app, owner, "receipt", formPart{name: "file", filename: "a.pdf", data: samplePDF}))
	id := created.File.ID.String()

	// fakeFiles returns the row whatever the tenant scope, so this is the
	// handler's own tenant check answering, on top of row level security.
	for _, path := range []string{"/api/v1/files/" + id, "/api/v1/files/" + id + "/content"} {
		rec := doRequest(t, app.handler, http.MethodGet, path, nil, "", other.cookie, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s as another tenant: status = %d, want 404, body = %s", path, rec.Code, rec.Body.String())
		}
		if bytes.Contains(rec.Body.Bytes(), samplePDF) {
			t.Errorf("GET %s as another tenant leaked the bytes", path)
		}
	}

	for _, path := range []string{"/api/v1/files/" + id, "/api/v1/files/" + id + "/content"} {
		rec := doRequest(t, app.handler, http.MethodGet, path, nil, "", nil, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s anonymously: status = %d, want 401", path, rec.Code)
		}
	}
}

func TestDownloadDetectsATamperedObject(t *testing.T) {
	t.Parallel()

	t.Run("small object, same size: clean 500, no bytes", func(t *testing.T) {
		t.Parallel()

		app := buildTestApp(t, 1000)
		_, objects, _ := filesOf(t, app)
		s := ownerSession(t, app)
		created := decode[uploadResult](t, upload(t, app, s, "receipt", formPart{name: "file", filename: "a.pdf", data: samplePDF}))

		key := objectstore.Key(s.tenant.ID, created.File.ID)
		tampered := bytes.Clone(samplePDF)
		tampered[len(tampered)-2] ^= 0xFF
		objects.tamper(key, tampered)

		rec := doRequest(t, app.handler, http.MethodGet, "/api/v1/files/"+created.File.ID.String()+"/content", nil, "", s.cookie, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
		if bytes.Contains(rec.Body.Bytes(), tampered[:8]) {
			t.Error("the response carries object bytes")
		}
	})

	t.Run("different size: clean 500", func(t *testing.T) {
		t.Parallel()

		app := buildTestApp(t, 1000)
		_, objects, _ := filesOf(t, app)
		s := ownerSession(t, app)
		created := decode[uploadResult](t, upload(t, app, s, "receipt", formPart{name: "file", filename: "a.pdf", data: samplePDF}))
		objects.tamper(objectstore.Key(s.tenant.ID, created.File.ID), append(bytes.Clone(samplePDF), 'x'))

		rec := doRequest(t, app.handler, http.MethodGet, "/api/v1/files/"+created.File.ID.String()+"/content", nil, "", s.cookie, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
	})

	t.Run("missing object: 500", func(t *testing.T) {
		t.Parallel()

		app := buildTestApp(t, 1000)
		fx, _, _ := filesOf(t, app)
		s := ownerSession(t, app)
		id := uuid.New()
		fx.files.add(storage.File{
			ID: id, TenantID: s.tenant.ID, Purpose: storage.FilePurposeReceipt, ObjectKey: objectstore.Key(s.tenant.ID, id),
			SHA256: sha256Of(samplePDF), SizeBytes: int64(len(samplePDF)), ContentType: "application/pdf",
		})

		rec := doRequest(t, app.handler, http.MethodGet, "/api/v1/files/"+id.String()+"/content", nil, "", s.cookie, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
	})

	t.Run("large object, last byte changed: truncated, never complete", func(t *testing.T) {
		t.Parallel()

		app := buildTestApp(t, 1000)
		_, objects, _ := filesOf(t, app)
		s := ownerSession(t, app)

		large := append(bytes.Clone(samplePDF), bytes.Repeat([]byte{'z'}, 3*httpapi.VerifiedChunkSize)...)
		created := decode[uploadResult](t, upload(t, app, s, "receipt", formPart{name: "file", filename: "a.pdf", data: large}))
		tampered := bytes.Clone(large)
		tampered[len(tampered)-1] = 'y'
		objects.tamper(objectstore.Key(s.tenant.ID, created.File.ID), tampered)

		srv := httptest.NewServer(app.handler)
		t.Cleanup(srv.Close)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/api/v1/files/"+created.File.ID.String()+"/content", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(s.cookie)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		got, readErr := io.ReadAll(resp.Body)
		if readErr == nil && len(got) == len(large) {
			t.Fatal("the client read the whole tampered object without an error")
		}
		if len(got) >= len(large) {
			t.Errorf("read %d bytes, want fewer than %d", len(got), len(large))
		}
		if !errors.Is(readErr, io.ErrUnexpectedEOF) && readErr == nil {
			t.Errorf("read error = %v, want a truncated body", readErr)
		}

		// The same object unmodified streams in full.
		objects.tamper(objectstore.Key(s.tenant.ID, created.File.ID), large)
		rec := doRequest(t, app.handler, http.MethodGet, "/api/v1/files/"+created.File.ID.String()+"/content", nil, "", s.cookie, "")
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), large) {
			t.Errorf("untampered large object: status = %d, %d bytes, want 200 and all %d", rec.Code, rec.Body.Len(), len(large))
		}
	})

	// The store reports the recorded size but streams one byte more. With
	// a size that is a multiple of the chunk size every recorded byte
	// could otherwise go out before the mismatch is seen.
	for _, size := range []int{httpapi.VerifiedChunkSize, 2 * httpapi.VerifiedChunkSize, 1 << 20} {
		t.Run(fmt.Sprintf("store streams one byte more than %d: never complete", size), func(t *testing.T) {
			t.Parallel()

			app := buildTestApp(t, 1000)
			_, objects, _ := filesOf(t, app)
			s := ownerSession(t, app)

			data := append(bytes.Clone(samplePDF), bytes.Repeat([]byte{'z'}, size-len(samplePDF))...)
			created := decode[uploadResult](t, upload(t, app, s, "receipt", formPart{name: "file", filename: "a.pdf", data: data}))
			objects.tamperKeepingSize(objectstore.Key(s.tenant.ID, created.File.ID), append(bytes.Clone(data), 'x'), int64(size))

			status, got, readErr := fetchThroughServer(t, app, s, "/api/v1/files/"+created.File.ID.String()+"/content")
			if status == http.StatusOK && readErr == nil && len(got) >= size {
				t.Fatalf("the client read %d bytes without an error, want a 500 or a truncated body", len(got))
			}
			if status == http.StatusOK && len(got) >= size {
				t.Errorf("read %d bytes, want fewer than %d", len(got), size)
			}
		})
	}

	t.Run("an overwrite of the key does not replace the recorded version", func(t *testing.T) {
		t.Parallel()

		app := buildTestApp(t, 1000)
		_, objects, _ := filesOf(t, app)
		s := ownerSession(t, app)
		created := decode[uploadResult](t, upload(t, app, s, "receipt", formPart{name: "file", filename: "a.pdf", data: samplePDF}))

		objects.overwrite(objectstore.Key(s.tenant.ID, created.File.ID), []byte("%PDF-1.7 replaced"))

		rec := doRequest(t, app.handler, http.MethodGet, "/api/v1/files/"+created.File.ID.String()+"/content", nil, "", s.cookie, "")
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), samplePDF) {
			t.Errorf("status = %d, body = %q, want 200 and the recorded version", rec.Code, rec.Body.Bytes())
		}
	})

	t.Run("a row whose key is not tenant/id is refused", func(t *testing.T) {
		t.Parallel()

		app := buildTestApp(t, 1000)
		fx, objects, _ := filesOf(t, app)
		s := ownerSession(t, app)

		// The row belongs to the caller's tenant but points at another key,
		// here the object of a different file.
		other := uuid.New()
		otherKey := objectstore.Key(s.tenant.ID, other)
		objects.tamper(otherKey, samplePDF)
		id := uuid.New()
		fx.files.add(storage.File{
			ID: id, TenantID: s.tenant.ID, Purpose: storage.FilePurposeReceipt, ObjectKey: otherKey,
			SHA256: sha256Of(samplePDF), SizeBytes: int64(len(samplePDF)), ContentType: "application/pdf",
		})

		for _, path := range []string{"/api/v1/files/" + id.String(), "/api/v1/files/" + id.String() + "/content"} {
			rec := doRequest(t, app.handler, http.MethodGet, path, nil, "", s.cookie, "")
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("GET %s: status = %d, want 500", path, rec.Code)
			}
			if bytes.Contains(rec.Body.Bytes(), samplePDF) {
				t.Errorf("GET %s served the object of another key", path)
			}
		}
	})
}

// fetchThroughServer sends a GET through a real server, so an aborted
// response is seen the way a client sees it.
func fetchThroughServer(t *testing.T, app testApp, s fileCaller, path string) (int, []byte, error) {
	t.Helper()

	srv := httptest.NewServer(app.handler)
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(s.cookie)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	got, readErr := io.ReadAll(resp.Body)

	return resp.StatusCode, got, readErr
}

func TestFileRoutesAreGatedByTheBucketCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		versioning, lock bool
		err              error
	}{
		{name: "no versioning", versioning: false, lock: true},
		{name: "no object lock while requested", versioning: true, lock: false},
		{name: "unreachable", err: errors.New("connection refused")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := buildTestApp(t, 1000)
			fx, objects, logos := filesOf(t, app)
			logos.saveProfile()
			s := ownerSession(t, app)

			created := decode[uploadResult](t, upload(t, app, s, "receipt", formPart{name: "file", filename: "a.pdf", data: samplePDF}))
			fx.setBucket(tt.versioning, tt.lock, tt.err)
			before := objects.count()

			recs := map[string]*httptest.ResponseRecorder{
				"upload":   upload(t, app, s, "receipt", formPart{name: "file", filename: "a.pdf", data: samplePDF}),
				"download": doRequest(t, app.handler, http.MethodGet, "/api/v1/files/"+created.File.ID.String()+"/content", nil, "", s.cookie, ""),
				"logo get": doRequest(t, app.handler, http.MethodGet, "/api/v1/profile/logo", nil, "", s.cookie, ""),
			}
			body, contentType := multipartBody(t, formPart{name: "file", filename: "logo.png", data: samplePNG})
			recs["logo put"] = doRequest(t, app.handler, http.MethodPut, "/api/v1/profile/logo", body, contentType, s.cookie, s.token)

			for name, rec := range recs {
				if rec.Code != http.StatusServiceUnavailable || errorCode(t, rec) != "object_store_unavailable" {
					t.Errorf("%s: status = %d, body = %s, want 503 object_store_unavailable", name, rec.Code, rec.Body.String())
				}
			}
			if objects.count() != before {
				t.Error("a gated upload stored an object")
			}

			// Metadata does not depend on the bucket.
			meta := doRequest(t, app.handler, http.MethodGet, "/api/v1/files/"+created.File.ID.String(), nil, "", s.cookie, "")
			if meta.Code != http.StatusOK {
				t.Errorf("metadata while gated: status = %d, want 200", meta.Code)
			}

			// Every other route keeps working: the session still answers.
			if rec := doRequest(t, app.handler, http.MethodGet, "/api/v1/me", nil, "", s.cookie, ""); rec.Code != http.StatusOK {
				t.Errorf("GET /api/v1/me while gated: status = %d, want 200", rec.Code)
			}
		})
	}
}

func TestStatusReportsTheLastBucketCheck(t *testing.T) {
	t.Parallel()

	app := buildTestApp(t, 1000)
	fx, _, _ := filesOf(t, app)
	s := ownerSession(t, app)

	type status struct {
		ObjectStore struct {
			Reachable            bool    `json:"reachable"`
			Versioning           bool    `json:"versioning"`
			ObjectLock           bool    `json:"object_lock"`
			RequireVersioning    bool    `json:"require_versioning"`
			FileRoutesAvailable  bool    `json:"file_routes_available"`
			GoBDProtectionActive bool    `json:"gobd_protection_active"`
			CheckedAt            *string `json:"checked_at"`
		} `json:"object_store"`
	}

	rec := doRequest(t, app.handler, http.MethodGet, "/api/v1/status", nil, "", s.cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	got := decode[status](t, rec).ObjectStore
	if !got.Reachable || !got.Versioning || !got.ObjectLock || !got.RequireVersioning ||
		!got.FileRoutesAvailable || !got.GoBDProtectionActive || got.CheckedAt == nil {
		t.Errorf("object_store = %+v, want everything true and a check time", got)
	}

	fx.setBucket(true, false, nil)
	got = decode[status](t, doRequest(t, app.handler, http.MethodGet, "/api/v1/status", nil, "", s.cookie, "")).ObjectStore
	if got.ObjectLock || got.GoBDProtectionActive || got.FileRoutesAvailable {
		t.Errorf("object_store without lock = %+v, want lock, protection and file routes false", got)
	}

	if rec := doRequest(t, app.handler, http.MethodGet, "/api/v1/status", nil, "", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous status = %d, want 401", rec.Code)
	}
}

func TestLogo(t *testing.T) {
	t.Parallel()

	putLogo := func(t *testing.T, app testApp, s fileCaller, part formPart) *httptest.ResponseRecorder {
		t.Helper()

		body, contentType := multipartBody(t, part)

		return doRequest(t, app.handler, http.MethodPut, "/api/v1/profile/logo", body, contentType, s.cookie, s.token)
	}

	t.Run("owner sets a png and reads it back", func(t *testing.T) {
		t.Parallel()

		app := buildTestApp(t, 1000)
		_, _, logos := filesOf(t, app)
		logos.saveProfile()
		s := ownerSession(t, app)

		rec := putLogo(t, app, s, formPart{name: "file", filename: "logo.pdf", contentType: "application/pdf", data: samplePNG})
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT status = %d, want 200, body = %s", rec.Code, rec.Body.String())
		}
		got := decode[struct {
			FileID    uuid.UUID `json:"file_id"`
			SHA256    string    `json:"sha256"`
			SizeBytes int64     `json:"size_bytes"`
		}](t, rec)
		if got.SHA256 != sha256Of(samplePNG) || got.SizeBytes != int64(len(samplePNG)) {
			t.Errorf("PUT body = %+v", got)
		}
		if current := logos.current(); current == nil || *current != got.FileID {
			t.Errorf("logo = %v, want %v", current, got.FileID)
		}

		rec = doRequest(t, app.handler, http.MethodGet, "/api/v1/profile/logo", nil, "", s.cookie, "")
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), samplePNG) {
			t.Fatalf("GET status = %d, want 200 and the png", rec.Code)
		}
		if ct := rec.Result().Header.Get("Content-Type"); ct != "image/png" {
			t.Errorf("Content-Type = %q, want image/png", ct)
		}
		if rec.Result().Header.Get("X-Content-Type-Options") != "nosniff" ||
			!strings.HasPrefix(rec.Result().Header.Get("Content-Security-Policy"), "sandbox") {
			t.Error("the logo misses nosniff or the sandbox policy")
		}
	})

	t.Run("a member cannot set it", func(t *testing.T) {
		t.Parallel()

		app := buildTestApp(t, 1000)
		_, objects, logos := filesOf(t, app)
		logos.saveProfile()
		s := memberSession(t, app, "members")

		if rec := putLogo(t, app, s, formPart{name: "file", filename: "logo.png", data: samplePNG}); rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", rec.Code)
		}
		if objects.count() != 0 {
			t.Error("a refused logo was stored")
		}
	})

	t.Run("only png or jpeg", func(t *testing.T) {
		t.Parallel()

		app := buildTestApp(t, 1000)
		_, objects, logos := filesOf(t, app)
		logos.saveProfile()
		s := ownerSession(t, app)

		for _, data := range [][]byte{samplePDF, []byte("<?xml version=\"1.0\"?><svg/>"), []byte("GIF89a")} {
			if rec := putLogo(t, app, s, formPart{name: "file", filename: "logo.png", contentType: "image/png", data: data}); rec.Code != http.StatusUnsupportedMediaType {
				t.Errorf("status = %d, want 415 for %q", rec.Code, data[:5])
			}
		}
		if objects.count() != 0 {
			t.Error("a refused logo was stored")
		}
	})

	t.Run("over 512 KiB", func(t *testing.T) {
		t.Parallel()

		app := buildTestApp(t, 1000)
		_, objects, logos := filesOf(t, app)
		logos.saveProfile()
		s := ownerSession(t, app)

		big := append(bytes.Clone(samplePNG), bytes.Repeat([]byte{0}, 512<<10)...)
		if rec := putLogo(t, app, s, formPart{name: "file", filename: "logo.png", data: big}); rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("status = %d, want 413", rec.Code)
		}
		if objects.count() != 0 {
			t.Error("an oversize logo was stored")
		}
	})

	t.Run("no profile yet", func(t *testing.T) {
		t.Parallel()

		app := buildTestApp(t, 1000)
		_, objects, _ := filesOf(t, app)
		s := ownerSession(t, app)

		if rec := putLogo(t, app, s, formPart{name: "file", filename: "logo.png", data: samplePNG}); rec.Code != http.StatusNotFound {
			t.Errorf("PUT status = %d, want 404", rec.Code)
		}
		if rec := doRequest(t, app.handler, http.MethodGet, "/api/v1/profile/logo", nil, "", s.cookie, ""); rec.Code != http.StatusNotFound {
			t.Errorf("GET status = %d, want 404", rec.Code)
		}
		if objects.count() != 0 {
			t.Error("a logo without a profile was stored")
		}
	})

	t.Run("no logo set", func(t *testing.T) {
		t.Parallel()

		app := buildTestApp(t, 1000)
		_, _, logos := filesOf(t, app)
		logos.saveProfile()
		s := ownerSession(t, app)

		if rec := doRequest(t, app.handler, http.MethodGet, "/api/v1/profile/logo", nil, "", s.cookie, ""); rec.Code != http.StatusNotFound {
			t.Errorf("GET status = %d, want 404", rec.Code)
		}
	})
}

// nestXML is a closed document of depth elements nested in each other.
func nestXML(depth int) string {
	return "<?xml version=\"1.0\"?>" + strings.Repeat("<a>", depth) + strings.Repeat("</a>", depth)
}

func TestSniffContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want string
	}{
		{"pdf", "%PDF-1.4", "application/pdf"},
		{"png", "\x89PNG\r\n\x1a\nrest", "image/png"},
		{"jpeg", "\xFF\xD8\xFF\xE0rest", "image/jpeg"},
		{"xml", "<?xml version=\"1.0\"?><Invoice/>", "application/xml"},
		{"xml after bom and newline", "\xEF\xBB\xBF\n<?xml version=\"1.0\"?>\n<Invoice/>", "application/xml"},
		{"xml declaration without a root", "<?xml version=\"1.0\"?>", ""},
		{"xml with comment, pi and namespaced root", "<?xml version=\"1.0\"?>\n<!-- c -->\n<?app x?>\n<rsm:CrossIndustryInvoice xmlns:rsm=\"urn:x\"/>", "application/xml"},
		{"a long comment before the root", "<?xml version=\"1.0\"?><!--" + strings.Repeat("x", 5000) + "--><Invoice/>", "application/xml"},
		// Without the SVG namespace a browser runs no script in it.
		{"svgx root without a namespace is accepted", "<?xml version=\"1.0\"?><svgx/>", "application/xml"},
		{"doctype with an internal subset", "<?xml version=\"1.0\"?>\n<!DOCTYPE x [<!ENTITY e \"v\">]>\n<rsm:CrossIndustryInvoice xmlns:rsm=\"urn:x\"/>", ""},
		{"doctype whose entity value holds ]>", "<?xml version=\"1.0\"?><!DOCTYPE r [<!ENTITY e \"]><Invoice/>\">]><svg xmlns=\"x\"/>", ""},
		{"svg root", "<?xml version=\"1.0\"?><svg xmlns=\"x\" onload=\"alert(1)\"/>", ""},
		{"svg root upper case after a comment", "<?xml version=\"1.0\"?><!-- x --><SVG/>", ""},
		{"prefixed svg root", "<?xml version=\"1.0\"?><s:svg xmlns:s=\"x\"/>", ""},
		{"xhtml root after doctype", "<?xml version=\"1.0\"?><!DOCTYPE html><html><script>alert(1)</script></html>", ""},
		{"svg namespace on another root", "<?xml version=\"1.0\"?><a xmlns=\"http://www.w3.org/2000/svg\"/>", ""},
		{"xhtml namespace on another root", "<?xml version=\"1.0\"?><a><b xmlns=\"http://www.w3.org/1999/XHTML\"/></a>", ""},
		{"xhtml namespace after 4 KiB", "<?xml version=\"1.0\"?><rsm:CrossIndustryInvoice xmlns:rsm=\"urn:x\"><!--" + strings.Repeat("x", 5000) +
			"--><h:script xmlns:h=\"http://www.w3.org/1999/xhtml\">alert(1)</h:script></rsm:CrossIndustryInvoice>", ""},
		{"xhtml namespace through a character reference", "<?xml version=\"1.0\"?><r><h:script xmlns:h=\"http://www.w3.org/1999/xht&#109;l\">alert(1)</h:script></r>", ""},
		{"svg namespace through a character reference", "<?xml version=\"1.0\"?><r xmlns=\"http://www.w3.org/2000/&#115;vg\"/>", ""},
		{"xhtml namespace through an entity", "<?xml version=\"1.0\"?><!DOCTYPE r [<!ENTITY ns \"http://www.w3.org/1999/xht&#109;l\">]><r xmlns=\"&ns;\"/>", ""},
		{"unused xhtml namespace declaration", "<?xml version=\"1.0\"?><r xmlns:h=\"http://www.w3.org/1999/xhtml\"/>", ""},
		{"undefined entity", "<?xml version=\"1.0\"?><r>&ns;</r>", ""},
		{"xhtml attribute namespace", "<?xml version=\"1.0\"?><r xmlns:h=\"http://www.w3.org/1999/xhtml\" h:onload=\"x\"/>", ""},
		{"xml-stylesheet pi", "<?xml version=\"1.0\"?><?xml-stylesheet type=\"text/xsl\" href=\"x.xsl\"?><Invoice/>", ""},
		{"two roots", "<?xml version=\"1.0\"?><Invoice/><Invoice/>", ""},
		{"depth 256 accepted", nestXML(256), "application/xml"},
		{"depth 257 refused", nestXML(257), ""},
		{"repeated attribute", "<?xml version=\"1.0\"?><r a=\"1\" a=\"2\"/>", ""},
		{"same attribute through two prefixes of one namespace", "<?xml version=\"1.0\"?><r xmlns:p=\"urn:x\" xmlns:q=\"urn:x\" p:a=\"1\" q:a=\"2\"/>", ""},
		{"upper case xml-stylesheet pi", "<?xml version=\"1.0\"?><?XML-STYLESHEET type=\"text/xsl\" href=\"x.xsl\"?><Invoice/>", ""},
		{"attribute value of 64 KiB accepted", "<?xml version=\"1.0\"?><r a=\"" + strings.Repeat("c", 64<<10) + "\"/>", "application/xml"},
		{"attribute value over 64 KiB refused", "<?xml version=\"1.0\"?><r a=\"" + strings.Repeat("c", 64<<10+1) + "\"/>", ""},
		{"not well formed", "<?xml version=\"1.0\"?><Invoice><a></Invoice>", ""},
		{"latin-1 encoding", "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><Invoice/>", ""},
		{"unterminated comment", "<?xml version=\"1.0\"?><!-- never closed", ""},
		{"xml-stylesheet is not a declaration", "<?xml-stylesheet href=\"x\"?>", ""},
		{"xml without declaration", "<Invoice/>", ""},
		{"html", "<!doctype html>", ""},
		{"truncated png", "\x89PNG", ""},
		{"empty", "", ""},
		{"text", "hello", ""},
	}
	for _, tt := range tests {
		if got := httpapi.SniffContentType([]byte(tt.data)); got != tt.want {
			t.Errorf("%s: SniffContentType() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// unversionedStore can read only the current object: it does not
// implement storage.VersionedObjectReader. It counts Get calls.
type unversionedStore struct{ gets int }

func (s *unversionedStore) Put(context.Context, storage.PutObject) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, nil
}

func (s *unversionedStore) Get(context.Context, string) (io.ReadCloser, storage.ObjectInfo, error) {
	s.gets++

	return io.NopCloser(bytes.NewReader(samplePDF)), storage.ObjectInfo{SizeBytes: int64(len(samplePDF))}, nil
}

func (s *unversionedStore) Stat(context.Context, string) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, nil
}

func (s *unversionedStore) BucketVersioning(context.Context) (bool, error) { return false, nil }

func TestReadStoredObjectNeverFallsBackToTheCurrentObject(t *testing.T) {
	t.Parallel()

	store := &unversionedStore{}
	version := "v1"
	file := storage.File{ID: uuid.New(), ObjectKey: "t/f", ObjectVersionID: &version}

	if _, _, err := httpapi.ReadStoredObject(context.Background(), store, file); err == nil {
		t.Fatal("ReadStoredObject() with a version on a store without versions error = nil, want an error")
	}
	if store.gets != 0 {
		t.Errorf("Get called %d times, want 0: no fall back to the current object", store.gets)
	}

	// Without a recorded version the current object is the right one.
	file.ObjectVersionID = nil
	body, _, err := httpapi.ReadStoredObject(context.Background(), store, file)
	if err != nil {
		t.Fatalf("ReadStoredObject() without a version error = %v", err)
	}
	_ = body.Close()
	if store.gets != 1 {
		t.Errorf("Get called %d times, want 1", store.gets)
	}
}

// TestSniffAcceptsEInvoices proves the roots XML uploads exist for pass:
// UBL Invoice and CreditNote, CII CrossIndustryInvoice, and a default
// namespace Invoice.
func TestSniffAcceptsEInvoices(t *testing.T) {
	t.Parallel()

	samples := map[string]string{
		"ubl invoice": `<?xml version="1.0" encoding="UTF-8"?>
<Invoice xmlns="urn:oasis:names:specification:ubl:schema:xsd:Invoice-2"
         xmlns:cac="urn:oasis:names:specification:ubl:schema:xsd:CommonAggregateComponents-2"
         xmlns:cbc="urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2">
  <cbc:CustomizationID>urn:cen.eu:en16931:2017#compliant#urn:xeinkauf.de:kosit:xrechnung_3.0</cbc:CustomizationID>
  <cbc:ID>RE-20260009</cbc:ID>
  <cbc:IssueDate>2026-11-20</cbc:IssueDate>
  <cac:LegalMonetaryTotal><cbc:PayableAmount currencyID="EUR">119.00</cbc:PayableAmount></cac:LegalMonetaryTotal>
</Invoice>`,
		"ubl credit note": `<?xml version="1.0" encoding="UTF-8"?>
<CreditNote xmlns="urn:oasis:names:specification:ubl:schema:xsd:CreditNote-2"
            xmlns:cbc="urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2">
  <cbc:ID>RE-20260009-1</cbc:ID>
</CreditNote>`,
		"cii invoice": `<?xml version="1.0" encoding="UTF-8"?>
<!-- ZUGFeRD / Factur-X -->
<rsm:CrossIndustryInvoice xmlns:rsm="urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100"
    xmlns:ram="urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100"
    xmlns:udt="urn:un:unece:uncefact:data:standard:UnqualifiedDataType:100">
  <rsm:ExchangedDocument>
    <ram:ID>RE-20260009</ram:ID>
    <ram:IssueDateTime><udt:DateTimeString format="102">20261120</udt:DateTimeString></ram:IssueDateTime>
    <ram:IncludedNote><ram:Content>Gem&#228;&#223; &amp; 19 UStG</ram:Content></ram:IncludedNote>
  </rsm:ExchangedDocument>
</rsm:CrossIndustryInvoice>`,
		"default namespace invoice with bom": "\xEF\xBB\xBF<?xml version=\"1.0\"?>\n<Invoice xmlns=\"urn:example:invoice\"><Total>119.00</Total></Invoice>\n",
	}
	for name, data := range samples {
		if got := httpapi.SniffContentType([]byte(data)); got != "application/xml" {
			t.Errorf("%s: SniffContentType() = %q, want application/xml", name, got)
		}
	}
}

// TestSniffXMLIsBounded proves hostile XML is refused quickly: entity
// expansion never happens, deep nesting stops at the depth cap, and a
// 10 MiB body is parsed in linear time.
func TestSniffXMLIsBounded(t *testing.T) {
	// Not parallel: the time bounds would otherwise measure the other
	// tests too.
	// The bounds are far above the measured times on purpose. A shared CI
	// runner with the race detector is about 2.5 times slower than a laptop
	// (12.5 s against 5 s for the slowest case), and a 12 s bound failed
	// there. A quadratic parse of 10 MiB would take hours, so these bounds
	// still catch the regression this test exists for.
	large := 10 * time.Second
	if raceEnabled {
		large = 60 * time.Second
	}

	var laughs strings.Builder
	laughs.WriteString(`<?xml version="1.0"?><!DOCTYPE lolz [<!ENTITY lol "lol">`)
	for i := 1; i <= 9; i++ {
		fmt.Fprintf(&laughs, `<!ENTITY lol%d "`, i)
		for range 10 {
			if i == 1 {
				laughs.WriteString("&lol;")
			} else {
				fmt.Fprintf(&laughs, "&lol%d;", i-1)
			}
		}
		laughs.WriteString(`">`)
	}
	laughs.WriteString(`]><lolz>&lol9;</lolz>`)

	nested := "<?xml version=\"1.0\"?>" + strings.Repeat("<a>", 10<<20/3)
	var attrs strings.Builder
	attrs.WriteString("<?xml version=\"1.0\"?><a ")
	for i := 0; attrs.Len() < 10<<20; i++ {
		fmt.Fprintf(&attrs, "b%d=\"c\" ", i)
	}
	attrs.WriteString("/>")
	manyElements := "<?xml version=\"1.0\"?><r>" + strings.Repeat("<a x=\"1\"/>", 10<<20/11) + "</r>"

	tests := []struct {
		name  string
		data  string
		want  string
		bound time.Duration
	}{
		// About 0.3 s without the race detector and 5 s with it were
		// measured on a laptop, 12.5 s with it on a CI runner.
		{"billion laughs", laughs.String(), "", 100 * time.Millisecond},
		{"10 MiB of nesting", nested, "", large},
		{"10 MiB of distinct attributes on one element", attrs.String(), "", large},
		{"10 MiB of one repeated attribute", "<?xml version=\"1.0\"?><a " + strings.Repeat("b=\"c\" ", 10<<20/6) + "/>", "", large},
		{"10 MiB attribute value", "<?xml version=\"1.0\"?><a b=\"" + strings.Repeat("c", 10<<20) + "\"/>", "", large},
		{"10 MiB of small elements", manyElements, "application/xml", large},
		{"10 MiB base64 text node", "<?xml version=\"1.0\"?><Invoice><Attachment>" + strings.Repeat("QUJD", 10<<20/4) + "</Attachment></Invoice>", "application/xml", large},
	}
	for _, tt := range tests {
		start := time.Now()
		got := httpapi.SniffContentType([]byte(tt.data))
		elapsed := time.Since(start)
		if got != tt.want {
			t.Errorf("%s: SniffContentType() = %q, want %q", tt.name, got, tt.want)
		}
		if elapsed > tt.bound {
			t.Errorf("%s: took %v, want at most %v", tt.name, elapsed, tt.bound)
		}
		t.Logf("%s: %d bytes in %v", tt.name, len(tt.data), elapsed)
	}
}

// Unicode format characters, built from their code points so the source
// carries none of them literally.
var (
	rlo  = string(rune(0x202E)) // right to left override
	lri  = string(rune(0x2066)) // left to right isolate
	pdi  = string(rune(0x2069)) // pop directional isolate
	zwsp = string(rune(0x200B)) // zero width space
)

func TestCleanFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want string
	}{
		{"rechnung.pdf", "rechnung.pdf"},
		{"  a\x00b\r\nc.pdf ", "abc.pdf"},
		{"bad\xffutf8.pdf", "bad" + string(utf8.RuneError) + "utf8.pdf"},
		{strings.Repeat("ä", 300), strings.Repeat("ä", 255)},
		{"a" + rlo + "fdp.exe", "afdp.exe"},
		{lri + "inv" + pdi + "oice" + zwsp + ".pdf", "invoice.pdf"},
	}
	for _, tt := range tests {
		if got := httpapi.CleanFilename(tt.in); got != tt.want {
			t.Errorf("CleanFilename(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestAttachmentDisposition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, stored, contentType, want string
	}{
		{"plain", "rechnung.pdf", "application/pdf", `attachment; filename=rechnung.pdf`},
		{"extension from the sniffed type", "evil.svg", "application/pdf", `attachment; filename=evil.pdf`},
		{"jpeg", "photo.jpeg", "image/jpeg", `attachment; filename=photo.jpg`},
		{"right to left override", "a" + rlo + "fdp.exe", "application/pdf", `attachment; filename=afdp.pdf`},
		{"cr lf", "a\r\nSet-Cookie: x.pdf", "application/pdf", `attachment; filename="aSet-Cookie: x.pdf"`},
		{"quotes", `say "hi".pdf`, "application/pdf", `attachment; filename="say \"hi\".pdf"`},
		{"path", `..\..\etc/passwd`, "application/xml", `attachment; filename=.._.._etc_passwd.xml`},
		{"empty", "", "image/png", `attachment; filename=file.png`},
		{
			"non ascii gets an ascii fallback", "Quittung Bäcker.pdf", "application/pdf",
			`attachment; filename="Quittung B_cker.pdf"; filename*=utf-8''Quittung%20B%C3%A4cker.pdf`,
		},
		{
			"non ascii with quotes and cr lf", "Ä\"b\r\n.pdf", "application/pdf",
			`attachment; filename="_\"b.pdf"; filename*=utf-8''%C3%84%22b.pdf`,
		},
		{"only an extension", ".pdf", "application/pdf", `attachment; filename=file.pdf`},
	}
	for _, tt := range tests {
		if got := httpapi.AttachmentDisposition(tt.stored, tt.contentType); got != tt.want {
			t.Errorf("%s: AttachmentDisposition(%q) = %q, want %q", tt.name, tt.stored, got, tt.want)
		}
	}

	long := httpapi.AttachmentDisposition(strings.Repeat("a", 300)+".exe", "application/pdf")
	if !strings.HasSuffix(long, ".pdf") || len(long) > len("attachment; filename=")+200+len(".pdf") {
		t.Errorf("long name: %q, want at most 200 base characters and .pdf", long)
	}
	for _, header := range []string{long, httpapi.AttachmentDisposition("a\r\nb.pdf", "application/pdf")} {
		if strings.ContainsAny(header, "\r\n") {
			t.Errorf("header %q carries a line break", header)
		}
	}
}
