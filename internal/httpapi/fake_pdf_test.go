package httpapi_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/httpapi"
)

// init wires the renderer fake of issue #74 into the app of a test that
// asked for one with withPDFRenderer. Every other app keeps the
// renderers of the documents and contract hooks.
func init() {
	repositoryHooks = append(repositoryHooks, func(t *testing.T, repos *httpapi.Repositories) {
		if v, ok := pdfRenderers.Load(t); ok {
			if r, ok := v.(*fakePDFRenderer); ok {
				repos.Renderer = r
			}
		}
	})
}

// pdfRenderers holds the renderer fake of each test that asked for one.
var pdfRenderers sync.Map

// withPDFRenderer makes the next app of t carry a new fakePDFRenderer,
// which also accepts every payload, so issuing is on.
func withPDFRenderer(t *testing.T) *fakePDFRenderer {
	t.Helper()

	r := &fakePDFRenderer{}
	pdfRenderers.Store(t, r)
	t.Cleanup(func() { pdfRenderers.Delete(t) })

	return r
}

// renderCall is one call of the renderer fake.
type renderCall struct {
	payload []byte
	draft   bool
	logo    []byte
}

// fakePDFRenderer renders a small PDF naming its own call count, so two
// renders of the same payload differ and a test sees whether stored
// bytes or a new render reached the client. err, when set, is returned
// instead.
type fakePDFRenderer struct {
	mu    sync.Mutex
	calls []renderCall
	err   error
	// during, when set, runs inside the render, before it returns: a test
	// lets a concurrent download win the race to store the PDF there.
	during func()
}

func (r *fakePDFRenderer) RenderWithLogo(_ context.Context, payload []byte, draft bool, logo []byte) ([]byte, error) {
	r.mu.Lock()
	during := r.during
	r.mu.Unlock()
	if during != nil {
		during()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, renderCall{payload: payload, draft: draft, logo: logo})
	if r.err != nil {
		return nil, r.err
	}
	sum := sha256.Sum256(payload)

	return fmt.Appendf(nil, "%%PDF-1.7\n%% fake render %d draft=%v logo=%d bytes payload=%s\n%%%%EOF\n",
		len(r.calls), draft, len(logo), hex.EncodeToString(sum[:])), nil
}

func (r *fakePDFRenderer) CheckPayload(context.Context, docpayload.FrozenPayloadV1) (map[string]string, error) {
	return nil, nil
}

func (r *fakePDFRenderer) setErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.err = err
}

func (r *fakePDFRenderer) setDuring(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.during = fn
}

func (r *fakePDFRenderer) renders() []renderCall {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]renderCall(nil), r.calls...)
}

// rendererDown is a renderer failure that marks itself unavailable, as
// internal/pdf.ErrUnavailable does.
type rendererDown struct{}

func (rendererDown) Error() string             { return "fake: typst is missing" }
func (rendererDown) RendererUnavailable() bool { return true }

// cannotPrint is a renderer refusal naming fields, as
// internal/pdf.InvalidPayloadError does.
type cannotPrint struct{ details map[string]string }

func (e cannotPrint) Error() string                   { return "fake: the payload cannot be printed" }
func (e cannotPrint) FieldDetails() map[string]string { return e.details }
