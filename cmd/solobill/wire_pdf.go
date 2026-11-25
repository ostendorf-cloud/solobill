package main

import (
	"context"
	"fmt"

	"github.com/ostendorf-cloud/solobill/internal/config"
	"github.com/ostendorf-cloud/solobill/internal/docissue"
	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/pdf"
)

// The Typst renderer is the documents API's renderer and the issuing
// code's payload check.
var (
	_ httpapi.DocumentRenderer = (*pdf.Renderer)(nil)
	_ docissue.PayloadChecker  = (*pdf.Renderer)(nil)
)

// init wires issue #74: the PDF renderer.
func init() {
	wirers = append(wirers, wirePDF)
}

// wirePDF sets Repositories.Renderer when PDF_TYPST_BINARY is set. Without
// it the process starts with issuing off and says so in one line; issuing
// fails closed, because only the renderer can confirm that a payload
// prints. A configured binary that is missing or of another Typst version
// than internal/pdf is pinned to stops the start: another Typst release
// may lay out a stored payload differently.
func wirePDF(ctx context.Context, env wireEnv, repos *httpapi.Repositories) error {
	renderer, err := buildRenderer(ctx, env.cfg.PDF)
	if err != nil {
		return fmt.Errorf("pdf: %w", err)
	}
	if renderer == nil {
		env.logger.Warn("issuing documents is off: PDF_TYPST_BINARY is not set, so no PDF renderer is wired; " +
			"issue, correction and the PDF download answer 503")

		return nil
	}

	repos.Renderer = renderer
	env.logger.Info("pdf renderer wired", "typst_binary", env.cfg.PDF.TypstBinary, "typst_version", pdf.TypstVersion)

	return nil
}

// buildRenderer returns the renderer of cfg after checking the Typst
// binary's version, or nil when no binary is configured.
func buildRenderer(ctx context.Context, cfg config.PDFConfig) (*pdf.Renderer, error) {
	if !cfg.Enabled {
		return nil, nil //nolint:nilnil // no renderer configured is a state the API reports, not an error
	}

	renderer := pdf.New(pdf.Options{Binary: cfg.TypstBinary})
	if err := renderer.CheckVersion(ctx); err != nil {
		return nil, fmt.Errorf("check PDF_TYPST_BINARY %q: %w", cfg.TypstBinary, err)
	}

	return renderer, nil
}
