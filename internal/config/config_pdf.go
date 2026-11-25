package config

import "github.com/ostendorf-cloud/govue/envconf"

// PDFConfig is the document renderer's configuration (issue #74).
//
// No PDF_TYPST_BINARY is a valid configuration: the process starts,
// issuing a document, creating a correction and the PDF download answer
// 503, and one log line at startup says so. Everything else keeps
// working.
type PDFConfig struct {
	// Enabled is true exactly when PDF_TYPST_BINARY is set.
	Enabled bool
	// TypstBinary is the Typst executable: an absolute path or a name
	// looked up on PATH. cmd/solobill checks it and its version at
	// startup and refuses to start when either is wrong.
	TypstBinary string
}

// loadPDF reads the renderer's variables into cfg. envconf trims the
// value, so a value of only whitespace is unset. Nothing here can be
// invalid before the binary is run, so fail is not called.
func loadPDF(l *envconf.Loader, cfg *PDFConfig, _ func(error)) {
	binary := l.String("PDF_TYPST_BINARY", "")
	if binary == "" {
		return
	}
	cfg.Enabled = true
	cfg.TypstBinary = binary
}
