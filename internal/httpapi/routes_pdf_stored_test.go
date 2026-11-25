package httpapi_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/objectstore"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// storedPDFBytes is the PDF a test stores for a document directly.
var storedPDFBytes = []byte("%PDF-1.7\n% stored earlier\n%%EOF\n")

// putIssuedWithPDF stores an issued document and, unless edit is nil, a
// PDF for it under its derived file id. edit changes the files row
// before it is stored; the object is written under the row's key.
func putIssuedWithPDF(t *testing.T, f docFixture, edit func(*storage.File)) string {
	t.Helper()

	number := "RE-20260042"
	doc := documentStore(t, f.app).Put(storage.Document{
		TenantID:      f.tenant.ID,
		Type:          storage.DocumentTypeInvoice,
		State:         storage.DocumentStateIssued,
		CustomerID:    uuid.MustParse(f.customer),
		VATRegime:     storage.VATRegimeKleinunternehmer,
		Language:      storage.DocumentLanguageGerman,
		DisplayNumber: &number,
	})
	if edit == nil {
		return doc.ID.String()
	}

	fx, objects, _ := filesOf(t, f.app)
	id := httpapi.DocumentPDFFileID(doc.ID)
	row := storage.File{
		ID: id, TenantID: f.tenant.ID, Purpose: storage.FilePurposeDocumentPDF,
		ObjectKey: objectstore.Key(f.tenant.ID, id), SHA256: sha256Of(storedPDFBytes),
		SizeBytes: int64(len(storedPDFBytes)), ContentType: "application/pdf", OriginalFilename: number + ".pdf",
	}
	edit(&row)
	info, err := objects.Put(context.Background(), storage.PutObject{
		Key: row.ObjectKey, Body: bytes.NewReader(storedPDFBytes), SizeBytes: int64(len(storedPDFBytes)),
	})
	if err != nil {
		t.Fatal(err)
	}
	version := info.VersionID
	row.ObjectVersionID = &version
	fx.files.add(row)

	return doc.ID.String()
}

// TestDocumentPDFWithoutARenderer: a stored PDF stays downloadable while
// no renderer is configured; one that was never rendered is 503.
func TestDocumentPDFWithoutARenderer(t *testing.T) {
	t.Parallel()

	withoutPDFRenderer(t)
	f := addDocTenant(t, buildTestApp(t, 1000), "pdf-no-renderer")

	stored := putIssuedWithPDF(t, f, func(*storage.File) {})
	rec := f.pdf(t, f.owner, stored)
	wantPDF(t, rec, "RE-20260042.pdf")
	if !bytes.Equal(rec.Body.Bytes(), storedPDFBytes) {
		t.Errorf("body = %q, want the stored PDF", rec.Body.Bytes())
	}

	notStored := putIssuedWithPDF(t, f, nil)
	wantErrorCode(t, f.pdf(t, f.owner, notStored), http.StatusServiceUnavailable, "renderer_unavailable")
	wantErrorCode(t, f.pdf(t, f.owner, f.createID(t)), http.StatusServiceUnavailable, "renderer_unavailable")
}

// TestDocumentPDFNeverServesAForeignStoredRow: a files row under a
// document PDF's id that is not that PDF is never served, and the
// document is not rendered over it.
func TestDocumentPDFNeverServesAForeignStoredRow(t *testing.T) {
	t.Parallel()

	other := uuid.New()
	cases := []struct {
		name string
		edit func(*storage.File)
	}{
		{"another tenant", func(f *storage.File) { f.TenantID, f.ObjectKey = other, objectstore.Key(other, f.ID) }},
		{"another purpose", func(f *storage.File) { f.Purpose = storage.FilePurposeReceipt }},
		{"another object key", func(f *storage.File) { f.ObjectKey = objectstore.Key(f.TenantID, uuid.New()) }},
		{"another hash", func(f *storage.File) { f.SHA256 = sha256Of([]byte("other bytes")) }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, renderer := newPDFFixture(t, "pdf-foreign-row")
			id := putIssuedWithPDF(t, f, tt.edit)

			rec := f.pdf(t, f.owner, id)
			wantErrorCode(t, rec, http.StatusInternalServerError, "internal")
			if bytes.Contains(rec.Body.Bytes(), storedPDFBytes) {
				t.Error("the foreign row's bytes were served")
			}
			if len(renderer.renders()) != 0 {
				t.Error("the document was rendered over a foreign row")
			}
		})
	}
}
