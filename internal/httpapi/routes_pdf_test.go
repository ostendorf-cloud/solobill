package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/csrf"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/objectstore"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// newPDFFixture is a Kleinunternehmer tenant in an app whose renderer is
// the fake of fake_pdf_test.go.
func newPDFFixture(t *testing.T, slug string) (docFixture, *fakePDFRenderer) {
	t.Helper()

	r := withPDFRenderer(t)

	return addDocTenant(t, buildTestApp(t, 1000), slug), r
}

func (f docFixture) pdf(t *testing.T, s profileSession, id string) *httptest.ResponseRecorder {
	t.Helper()

	return f.do(t, s, http.MethodGet, documentsPath+"/"+id+"/pdf", nil)
}

// issuedID creates and issues a draft and returns its id and number.
func (f docFixture) issuedID(t *testing.T) (string, string) {
	t.Helper()

	issued := f.mustIssue(t, f.createID(t))

	return issued.str(t, "id"), issued.str(t, "number")
}

// storedPDFs are the files rows of purpose document_pdf.
func storedPDFs(t *testing.T, app testApp) []storage.File {
	t.Helper()

	fx, _, _ := filesOf(t, app)
	var out []storage.File
	for _, f := range fx.files.all() {
		if f.Purpose == storage.FilePurposeDocumentPDF {
			out = append(out, f)
		}
	}

	return out
}

func wantPDF(t *testing.T, rec *httptest.ResponseRecorder, filename string) {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	h := rec.Result().Header
	if got := h.Get("Content-Type"); got != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", got)
	}
	disposition, params, err := mime.ParseMediaType(h.Get("Content-Disposition"))
	if err != nil || disposition != "attachment" || params["filename"] != filename {
		t.Errorf("Content-Disposition = %q, want an attachment named %q", h.Get("Content-Disposition"), filename)
	}
	if h.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(h.Get("Content-Security-Policy"), "sandbox") {
		t.Error("the PDF misses nosniff or the sandbox policy")
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF-")) {
		t.Errorf("body = %q, want a PDF", rec.Body.Bytes())
	}
}

func TestDocumentPDFRendersOnceAndServesTheStoredBytes(t *testing.T) {
	t.Parallel()

	f, renderer := newPDFFixture(t, "pdf-once")
	id, number := f.issuedID(t)
	_, objects, _ := filesOf(t, f.app)

	first := f.pdf(t, f.owner, id)
	wantPDF(t, first, number+".pdf")
	if calls := renderer.renders(); len(calls) != 1 || calls[0].draft || calls[0].logo != nil {
		t.Fatalf("renders = %+v, want one issued render without a logo", calls)
	}
	stored, ok := documentStore(t, f.app).Get(uuid.MustParse(id))
	if !ok || !bytes.Equal(renderer.renders()[0].payload, stored.FrozenPayload) {
		t.Error("the render did not get the frozen payload")
	}

	rows := storedPDFs(t, f.app)
	if len(rows) != 1 {
		t.Fatalf("stored PDFs = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.ID != httpapi.DocumentPDFFileID(uuid.MustParse(id)) || row.TenantID != f.tenant.ID ||
		row.ObjectKey != objectstore.Key(f.tenant.ID, row.ID) || row.SHA256 != sha256Of(first.Body.Bytes()) ||
		row.SizeBytes != int64(first.Body.Len()) || row.ContentType != "application/pdf" || row.OriginalFilename != number+".pdf" {
		t.Errorf("files row = %+v, want the rendered PDF under the document's derived id", row)
	}
	if data, ok := objects.get(row.ObjectKey); !ok || !bytes.Equal(data, first.Body.Bytes()) {
		t.Error("the bucket does not hold the rendered bytes")
	}

	// Every later download returns the stored bytes. The fake would
	// answer "render 2" if it ran again.
	member := f.member(t)
	for range 2 {
		again := f.pdf(t, member, id)
		wantPDF(t, again, number+".pdf")
		if !bytes.Equal(again.Body.Bytes(), first.Body.Bytes()) {
			t.Errorf("later download = %q, want the stored %q", again.Body.Bytes(), first.Body.Bytes())
		}
	}
	if n := len(renderer.renders()); n != 1 {
		t.Errorf("renders = %d, want 1", n)
	}
	if n := objects.count(); n != 1 {
		t.Errorf("puts = %d, want 1", n)
	}
}

// TestDocumentPDFLosingTheRaceServesTheStoredPDF: two first downloads
// render at once and the other one stores first. This one's insert fails
// on the primary key, which aborts its Postgres transaction (the fake
// models that), so the stored row must be read in a new one. The loser's
// Put left one more version of the key; the row names the winner's
// version, so that one is served.
func TestDocumentPDFLosingTheRaceServesTheStoredPDF(t *testing.T) {
	t.Parallel()

	f, renderer := newPDFFixture(t, "pdf-race")
	id, number := f.issuedID(t)
	fx, objects, _ := filesOf(t, f.app)
	fileID := httpapi.DocumentPDFFileID(uuid.MustParse(id))
	key := objectstore.Key(f.tenant.ID, fileID)
	winner := []byte("%PDF-1.7\n% stored by the other download\n%%EOF\n")

	renderer.setDuring(func() {
		info, err := objects.Put(context.Background(), storage.PutObject{Key: key, Body: bytes.NewReader(winner), SizeBytes: int64(len(winner))})
		if err != nil {
			t.Errorf("winner put: %v", err)

			return
		}
		version := info.VersionID
		fx.files.add(storage.File{
			ID: fileID, TenantID: f.tenant.ID, Purpose: storage.FilePurposeDocumentPDF, ObjectKey: key,
			ObjectVersionID: &version, SHA256: sha256Of(winner), SizeBytes: int64(len(winner)),
			ContentType: "application/pdf", OriginalFilename: number + ".pdf",
		})
	})

	rec := f.pdf(t, f.owner, id)
	wantPDF(t, rec, number+".pdf")
	if !bytes.Equal(rec.Body.Bytes(), winner) {
		t.Errorf("body = %q, want the PDF the other download stored", rec.Body.Bytes())
	}
	if n := len(storedPDFs(t, f.app)); n != 1 {
		t.Errorf("stored PDFs = %d, want 1", n)
	}
	if n := objects.versionCount(key); n != 2 {
		t.Errorf("versions of the key = %d, want 2: the winner's and the loser's unreferenced one", n)
	}
}

func TestDocumentPDFOfADraftRendersLiveAndIsNeverStored(t *testing.T) {
	t.Parallel()

	f, renderer := newPDFFixture(t, "pdf-draft")
	id := f.createID(t)
	_, objects, _ := filesOf(t, f.app)

	first := f.pdf(t, f.owner, id)
	wantPDF(t, first, "draft-"+id+".pdf")
	second := f.pdf(t, f.owner, id)
	wantPDF(t, second, "draft-"+id+".pdf")

	calls := renderer.renders()
	if len(calls) != 2 || !calls[0].draft || !calls[1].draft {
		t.Fatalf("renders = %+v, want two draft renders", calls)
	}
	if bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Error("the second download of a draft did not render again")
	}
	if !bytes.Contains(calls[0].payload, []byte(`"Customer Ltd"`)) {
		t.Errorf("draft payload = %s, want the customer", calls[0].payload)
	}
	if n := len(storedPDFs(t, f.app)); n != 0 || objects.count() != 0 {
		t.Errorf("stored PDFs = %d, puts = %d, want nothing stored for a draft", n, objects.count())
	}
}

func TestDocumentPDFIsAFileRoute(t *testing.T) {
	t.Parallel()

	f, renderer := newPDFFixture(t, "pdf-gated")
	storedID, storedNumber := f.issuedID(t)
	wantPDF(t, f.pdf(t, f.owner, storedID), storedNumber+".pdf")
	notYetID, _ := f.issuedID(t)
	draftID := f.createID(t)
	fx, objects, _ := filesOf(t, f.app)
	renders, puts := len(renderer.renders()), objects.count()

	fx.setBucket(false, false, nil)
	for name, id := range map[string]string{"stored": storedID, "first download": notYetID, "draft": draftID} {
		rec := f.pdf(t, f.owner, id)
		wantErrorCode(t, rec, http.StatusServiceUnavailable, "object_store_unavailable")
		if bytes.Contains(rec.Body.Bytes(), []byte("%PDF")) {
			t.Errorf("%s: a gated download served a PDF", name)
		}
	}
	if len(renderer.renders()) != renders || objects.count() != puts || len(storedPDFs(t, f.app)) != 1 {
		t.Error("a gated download rendered or stored something")
	}

	// Issuing never depends on the bucket.
	f.mustIssue(t, f.createID(t))
}

// putLogo uploads data as the tenant's logo as the fixture's owner.
func (f docFixture) putLogo(t *testing.T, data []byte) string {
	t.Helper()

	_, _, logos := filesOf(t, f.app)
	logos.saveProfile()
	body, contentType := multipartBody(t, formPart{name: "file", filename: "logo.png", data: data})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/api/v1/profile/logo", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set(csrf.HeaderName, f.owner.token)
	for _, c := range f.owner.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.app.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT logo status = %d, body = %s", rec.Code, rec.Body.String())
	}
	current := logos.current()
	if current == nil {
		t.Fatal("no logo set")
	}

	return current.String()
}

func TestDocumentPDFPrintsTheLogoNamedByItsHash(t *testing.T) {
	t.Parallel()

	t.Run("the logo file of the frozen hash", func(t *testing.T) {
		t.Parallel()

		f, renderer := newPDFFixture(t, "pdf-logo")
		f.putLogo(t, samplePNG)
		id, number := f.issuedID(t)
		// A logo set after issuing changes nothing: the payload names
		// the first one by hash.
		f.putLogo(t, append(bytes.Clone(samplePNG), 1))

		wantPDF(t, f.pdf(t, f.owner, id), number+".pdf")
		calls := renderer.renders()
		if len(calls) != 1 || !bytes.Equal(calls[0].logo, samplePNG) {
			t.Fatalf("renders = %d, logo = %q, want one render with the logo of the frozen hash", len(calls), calls[0].logo)
		}
	})

	t.Run("a changed logo object is a conflict", func(t *testing.T) {
		t.Parallel()

		f, renderer := newPDFFixture(t, "pdf-logo-tampered")
		logoID := f.putLogo(t, samplePNG)
		id, _ := f.issuedID(t)
		_, objects, _ := filesOf(t, f.app)
		objects.tamper(objectstore.Key(f.tenant.ID, uuid.MustParse(logoID)), append(bytes.Clone(samplePNG), 2))

		wantErrorCode(t, f.pdf(t, f.owner, id), http.StatusConflict, "conflict")
		if len(renderer.renders()) != 0 || len(storedPDFs(t, f.app)) != 0 {
			t.Error("a PDF was rendered or stored with a changed logo")
		}
	})

	t.Run("no logo file with the frozen hash is a conflict", func(t *testing.T) {
		t.Parallel()

		f, renderer := newPDFFixture(t, "pdf-logo-missing")
		logoID := f.putLogo(t, samplePNG)
		id, _ := f.issuedID(t)
		fx, _, _ := filesOf(t, f.app)
		row, err := fx.files.FindByID(context.Background(), uuid.MustParse(logoID))
		if err != nil {
			t.Fatal(err)
		}
		row.SHA256 = sha256Of([]byte("another logo"))
		fx.files.add(row)

		wantErrorCode(t, f.pdf(t, f.owner, id), http.StatusConflict, "conflict")
		if len(renderer.renders()) != 0 || len(storedPDFs(t, f.app)) != 0 {
			t.Error("a PDF was rendered or stored without its logo")
		}
	})
}

func TestDocumentPDFRendererFailures(t *testing.T) {
	t.Parallel()

	t.Run("an unavailable renderer is 503", func(t *testing.T) {
		t.Parallel()

		f, renderer := newPDFFixture(t, "pdf-down")
		id, _ := f.issuedID(t)
		renderer.setErr(rendererDown{})

		wantErrorCode(t, f.pdf(t, f.owner, id), http.StatusServiceUnavailable, "renderer_unavailable")
		if len(storedPDFs(t, f.app)) != 0 {
			t.Error("a failed render stored a PDF")
		}
	})

	t.Run("a draft the template cannot print is 400 with the fields", func(t *testing.T) {
		t.Parallel()

		f, renderer := newPDFFixture(t, "pdf-invalid")
		renderer.setErr(cannotPrint{details: map[string]string{"seller.tax_number": "required"}})

		body := wantErrorCode(t, f.pdf(t, f.owner, f.createID(t)), http.StatusBadRequest, "invalid_argument")
		if body.Error.Details["seller.tax_number"] != "required" {
			t.Errorf("details = %v, want seller.tax_number", body.Error.Details)
		}
	})

	t.Run("an issued payload the template refuses is an internal error", func(t *testing.T) {
		t.Parallel()

		f, renderer := newPDFFixture(t, "pdf-issued-invalid")
		id, _ := f.issuedID(t)
		renderer.setErr(cannotPrint{details: map[string]string{"lines": "too long"}})

		wantErrorCode(t, f.pdf(t, f.owner, id), http.StatusInternalServerError, "internal")
	})

	t.Run("a failing render is an internal error and stores nothing", func(t *testing.T) {
		t.Parallel()

		f, renderer := newPDFFixture(t, "pdf-garbage")
		id, _ := f.issuedID(t)
		renderer.setErr(errors.New("fake: compile failed"))

		wantErrorCode(t, f.pdf(t, f.owner, id), http.StatusInternalServerError, "internal")
		if len(storedPDFs(t, f.app)) != 0 {
			t.Error("a failed render stored a PDF")
		}
	})
}

func TestDocumentPDFOfAnotherTenantIsNotFound(t *testing.T) {
	t.Parallel()

	f, renderer := newPDFFixture(t, "pdf-tenant-a")
	id, number := f.issuedID(t)
	draftID := f.createID(t)
	other := addDocTenant(t, f.app, "pdf-tenant-b")

	for _, docID := range []string{id, draftID} {
		rec := f.pdf(t, other.owner, docID)
		wantErrorCode(t, rec, http.StatusNotFound, "not_found")
		if bytes.Contains(rec.Body.Bytes(), []byte("%PDF")) {
			t.Error("another tenant got a PDF")
		}
	}
	if len(renderer.renders()) != 0 {
		t.Error("another tenant's request rendered the document")
	}

	// The document exists: its own tenant gets it.
	wantPDF(t, f.pdf(t, f.owner, id), number+".pdf")
	// And once stored, the other tenant still gets nothing.
	wantErrorCode(t, f.pdf(t, other.owner, id), http.StatusNotFound, "not_found")
}

func TestDocumentPDFOfAnImportedDocumentIsAConflict(t *testing.T) {
	t.Parallel()

	f, renderer := newPDFFixture(t, "pdf-imported")
	number := "RE 2019/7"
	imported := documentStore(t, f.app).Put(storage.Document{
		TenantID:      f.tenant.ID,
		Type:          storage.DocumentTypeInvoice,
		State:         storage.DocumentStateIssued,
		CustomerID:    uuid.MustParse(f.customer),
		VATRegime:     storage.VATRegimeKleinunternehmer,
		Language:      storage.DocumentLanguageGerman,
		Imported:      true,
		DisplayNumber: &number,
	})

	wantErrorCode(t, f.pdf(t, f.owner, imported.ID.String()), http.StatusConflict, "conflict")
	if len(renderer.renders()) != 0 {
		t.Error("an imported document was rendered")
	}
}

func TestDocumentPDFRequiresASession(t *testing.T) {
	t.Parallel()

	f, _ := newPDFFixture(t, "pdf-anon")
	id, _ := f.issuedID(t)
	rec := f.pdf(t, profileSession{}, id)
	wantErrorCode(t, rec, http.StatusUnauthorized, "unauthenticated")
}

func TestDocumentPDFName(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("6f1c2a4e-0000-4000-8000-000000000001")
	str := func(s string) *string { return &s }
	cases := []struct {
		name  string
		input storage.Document
		want  string
	}{
		{"a draft", storage.Document{ID: id, State: storage.DocumentStateDraft, DisplayNumber: str("RE-1")}, "draft-" + id.String() + ".pdf"},
		{"an issued number", storage.Document{ID: id, State: storage.DocumentStateIssued, DisplayNumber: str("RE-20260001")}, "RE-20260001.pdf"},
		{"a correction", storage.Document{ID: id, State: storage.DocumentStateIssued, DisplayNumber: str("RE-20260001-2")}, "RE-20260001-2.pdf"},
		{"a slash", storage.Document{ID: id, State: storage.DocumentStateIssued, DisplayNumber: str("RE/2026/7")}, "RE_2026_7.pdf"},
		{"a quote and a line break", storage.Document{ID: id, State: storage.DocumentStateIssued, DisplayNumber: str("RE\"7\r\nX")}, "RE_7__X.pdf"},
		{"a space and an umlaut", storage.Document{ID: id, State: storage.DocumentStateIssued, DisplayNumber: str("RE 7ä")}, "RE_7__.pdf"},
		{"no number", storage.Document{ID: id, State: storage.DocumentStateIssued}, "document.pdf"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := httpapi.DocumentPDFName(tt.input); got != tt.want {
				t.Errorf("DocumentPDFName() = %q, want %q", got, tt.want)
			}
		})
	}
}
