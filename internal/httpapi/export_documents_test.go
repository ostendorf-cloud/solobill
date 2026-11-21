package httpapi

// PayloadCheckerWired reports whether r counts as a wired payload
// checker, for documents_test.go.
func PayloadCheckerWired(r DocumentRenderer) bool {
	_, ok := payloadCheckerOf(r)

	return ok
}

// DocumentsError maps an error of the documents routes to the API
// error, for documents_test.go.
func DocumentsError(err error) error {
	return documentsError(err)
}
