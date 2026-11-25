package httpapi

import (
	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// DocumentPDFName is the download name of d's PDF, for routes_pdf_test.go.
func DocumentPDFName(d storage.Document) string {
	return documentPDFName(d)
}

// DocumentPDFFileID is the files row id of a document's stored PDF, for
// routes_pdf_test.go.
func DocumentPDFFileID(id uuid.UUID) uuid.UUID {
	return documentPDFFileID(id)
}
