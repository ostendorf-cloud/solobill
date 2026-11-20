package httpapi

// SniffContentType exposes sniffContentType to files_test.go.
func SniffContentType(data []byte) string { return sniffContentType(data) }

// CleanFilename exposes cleanFilename to files_test.go.
func CleanFilename(name string) string { return cleanFilename(name) }

// VerifiedChunkSize exposes verifiedChunkSize to files_test.go.
const VerifiedChunkSize = verifiedChunkSize
