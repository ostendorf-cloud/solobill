package httpapi

// SniffContentType exposes sniffContentType to files_test.go.
func SniffContentType(data []byte) string { return sniffContentType(data) }

// CleanFilename exposes cleanFilename to files_test.go.
func CleanFilename(name string) string { return cleanFilename(name) }

// AttachmentDisposition exposes attachmentDisposition to files_test.go.
func AttachmentDisposition(stored, contentType string) string {
	return attachmentDisposition(stored, contentType)
}

// VerifiedChunkSize exposes verifiedChunkSize to files_test.go.
const VerifiedChunkSize = verifiedChunkSize
