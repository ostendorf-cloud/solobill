package config

import "github.com/ostendorf-cloud/govue/envconf"

// PDFConfig is the document renderer's configuration, for example the
// path of the Typst binary. Issue #74 adds the fields and the loading.
type PDFConfig struct{}

// loadPDF reads the renderer's variables into its second argument and
// reports every problem through fail. It reads nothing until issue #74
// lands.
func loadPDF(_ *envconf.Loader, _ *PDFConfig, _ func(error)) {}
