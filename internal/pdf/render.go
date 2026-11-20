// Package pdf renders a document's frozen payload into a PDF with Typst
// (issue #74). It implements httpapi.DocumentRenderer.
//
// The renderer formats and never computes: every figure is printed as
// the payload holds it. Rendering is reproducible: the same payload and
// template version give the same PDF bytes, because the template, the
// fonts and the creation timestamp all come from the payload or this
// package, never from the machine.
//
// Every render runs typst compile in its own temporary directory, which
// is also Typst's project root. The directory holds only the template,
// the data file and the bundled fonts. System fonts and Typst's own
// embedded fonts are ignored, packages resolve to empty directories
// inside the root, and the payload reaches Typst only as JSON data,
// never as markup.
package pdf

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
)

// CurrentTemplateVersion is the template a new document is issued
// with. The issuing code freezes it into FrozenPayloadV1.TemplateVersion.
const CurrentTemplateVersion = "document-v1"

// PDFStandard is the standard Typst enforces: PDF/A-3b, archivable, and
// the base of a later ZUGFeRD file which needs PDF/A-3 attachments.
const PDFStandard = "a-3b"

// Defaults of Options.
const (
	DefaultBinary         = "typst"
	DefaultTimeout        = 30 * time.Second
	DefaultMaxOutputBytes = 10 << 20
)

// maxStderrBytes bounds what is kept of Typst's diagnostics.
const maxStderrBytes = 8 << 10

//go:embed templates/*.typ fonts/*.ttf fonts/LICENSE
var assets embed.FS

// templates maps every template version ever shipped to its file. A
// version is never removed or changed: a stored payload names it
// forever.
var templates = map[string]string{
	"document-v1": "templates/document-v1.typ",
}

// Errors of Render.
var (
	// ErrUnknownTemplate is a template version this build does not ship.
	ErrUnknownTemplate = errors.New("pdf: unknown template version")
	// ErrUnavailable is a renderer that cannot run: no Typst binary.
	ErrUnavailable = errors.New("pdf: renderer unavailable")
	// ErrOutputTooLarge is a PDF larger than Options.MaxOutputBytes.
	ErrOutputTooLarge = errors.New("pdf: output too large")
	// ErrCompile is a Typst run that failed. The wrapped message carries
	// Typst's diagnostics.
	ErrCompile = errors.New("pdf: typst compile failed")
)

// Options configure a Renderer. A zero field takes its default.
type Options struct {
	// Binary is the Typst executable, a path or a name on PATH.
	Binary string
	// Timeout bounds one render, on top of the caller's context.
	Timeout time.Duration
	// MaxOutputBytes bounds the size of one PDF.
	MaxOutputBytes int64
	// TempDir is where the per render directory is created. Empty is
	// os.TempDir.
	TempDir string
}

// Renderer renders payloads through typst compile.
type Renderer struct {
	opts Options
}

// New returns a Renderer with the zero fields of opts set to defaults.
func New(opts Options) *Renderer {
	if opts.Binary == "" {
		opts.Binary = DefaultBinary
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.MaxOutputBytes <= 0 {
		opts.MaxOutputBytes = DefaultMaxOutputBytes
	}

	return &Renderer{opts: opts}
}

// Render decodes a stored docpayload.FrozenPayloadV1 and renders it with
// the template version it names. draft prints the watermark and no
// number. It is httpapi.DocumentRenderer's method.
func (r *Renderer) Render(ctx context.Context, payload []byte, draft bool) ([]byte, error) {
	p, err := docpayload.Unmarshal(payload)
	if err != nil {
		return nil, fmt.Errorf("pdf: %w", err)
	}

	return r.RenderPayload(ctx, p, draft)
}

// RenderPayload renders a decoded payload.
func (r *Renderer) RenderPayload(ctx context.Context, p docpayload.FrozenPayloadV1, draft bool) ([]byte, error) {
	src, err := BuildSource(p, draft)
	if err != nil {
		return nil, err
	}

	return r.compile(ctx, src)
}

// Source is everything one typst compile reads besides the fonts.
type Source struct {
	// Template is the Typst file of the payload's template version.
	Template []byte
	// Data is data.json, the formatted payload.
	Data []byte
	// CreationTime is the PDF creation timestamp: the issue date at
	// midnight UTC, so the bytes are the same on every render.
	CreationTime time.Time
}

// BuildSource resolves the template and formats the payload into the
// data file, without running Typst. The golden tests compare its Data.
func BuildSource(p docpayload.FrozenPayloadV1, draft bool) (Source, error) {
	in, err := BuildInput(p, draft)
	if err != nil {
		return Source{}, err
	}
	data, err := MarshalInput(in)
	if err != nil {
		return Source{}, err
	}
	tmpl, err := assets.ReadFile(templates[in.TemplateVersion])
	if err != nil {
		return Source{}, fmt.Errorf("pdf: read template %s: %w", in.TemplateVersion, err)
	}
	created := time.Unix(0, 0).UTC()
	if t, err := time.Parse(docpayload.DateLayout, p.IssueDate); err == nil {
		created = t
	}

	return Source{Template: tmpl, Data: data, CreationTime: created}, nil
}

// templateVersion returns the version to render: the payload's, or the
// current one for a draft that names none.
func templateVersion(p docpayload.FrozenPayloadV1, draft bool) (string, error) {
	v := p.TemplateVersion
	if v == "" && draft {
		v = CurrentTemplateVersion
	}
	if _, ok := templates[v]; !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownTemplate, v)
	}

	return v, nil
}

func (r *Renderer) compile(ctx context.Context, src Source) ([]byte, error) {
	bin, err := exec.LookPath(r.opts.Binary)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	dir, err := os.MkdirTemp(r.opts.TempDir, "solobill-pdf-")
	if err != nil {
		return nil, fmt.Errorf("pdf: temp dir: %w", err)
	}
	defer func() {
		// Best effort: a leftover directory holds no secret, only the
		// document being rendered, and the next render uses a new one.
		_ = os.RemoveAll(dir)
	}()

	if err := writeProject(dir, src); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, r.opts.Timeout)
	defer cancel()

	out := filepath.Join(dir, "out.pdf")
	// #nosec G204: the binary is configuration, every argument is fixed
	// or a path inside dir.
	cmd := exec.CommandContext(ctx, bin, compileArgs(dir, src.CreationTime)...)
	cmd.Dir = dir
	// A minimal environment: no TYPST_* variable from the host can add a
	// font path, a package path or a root.
	cmd.Env = []string{"HOME=" + dir, "XDG_CACHE_HOME=" + filepath.Join(dir, "cache"), "TZ=UTC"}
	stderr := &limitedBuffer{max: maxStderrBytes}
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr
	cmd.WaitDelay = time.Second

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w: %w", ErrCompile, ctx.Err())
		}

		return nil, fmt.Errorf("%w: %w: %s", ErrCompile, err, bytes.TrimSpace(stderr.Bytes()))
	}

	info, err := os.Stat(out)
	if err != nil {
		return nil, fmt.Errorf("%w: no output: %w", ErrCompile, err)
	}
	if info.Size() > r.opts.MaxOutputBytes {
		return nil, fmt.Errorf("%w: %d bytes, limit %d", ErrOutputTooLarge, info.Size(), r.opts.MaxOutputBytes)
	}
	pdf, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("pdf: read output: %w", err)
	}

	return pdf, nil
}

// compileArgs is the typst command line. Paths are relative to dir,
// which is both the working directory and the project root.
func compileArgs(dir string, created time.Time) []string {
	return []string{
		"compile",
		"--root", dir,
		"--font-path", filepath.Join(dir, "fonts"),
		"--ignore-system-fonts",
		"--ignore-embedded-fonts",
		"--package-path", filepath.Join(dir, "packages"),
		"--package-cache-path", filepath.Join(dir, "package-cache"),
		"--pdf-standard", PDFStandard,
		"--creation-timestamp", strconv.FormatInt(created.Unix(), 10),
		"--diagnostic-format", "short",
		"--format", "pdf",
		filepath.Join(dir, "main.typ"),
		filepath.Join(dir, "out.pdf"),
	}
}

// writeProject writes the template, the data and the bundled fonts into
// dir, and the empty package directories.
func writeProject(dir string, src Source) error {
	for _, sub := range []string{"fonts", "packages", "package-cache", "cache"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o700); err != nil {
			return fmt.Errorf("pdf: create %s: %w", sub, err)
		}
	}
	files := map[string][]byte{
		"main.typ":  src.Template,
		"data.json": src.Data,
	}
	fonts, err := fs.Glob(assets, "fonts/*.ttf")
	if err != nil {
		return fmt.Errorf("pdf: list fonts: %w", err)
	}
	for _, f := range fonts {
		b, err := assets.ReadFile(f)
		if err != nil {
			return fmt.Errorf("pdf: read font %s: %w", f, err)
		}
		files[f] = b
	}
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			return fmt.Errorf("pdf: write %s: %w", name, err)
		}
	}

	return nil
}

// FontLicense returns the licence of the bundled Go fonts, which a
// binary distribution has to reproduce.
func FontLicense() []byte {
	b, err := assets.ReadFile("fonts/LICENSE")
	if err != nil {
		// The file is embedded at build time; a missing one cannot build.
		return nil
	}

	return b
}

// limitedBuffer keeps the first max bytes written and drops the rest.
type limitedBuffer struct {
	buf bytes.Buffer
	max int
}

// Write keeps what fits and reports every byte as written, so the
// process never blocks on a full pipe.
func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
		} else {
			b.buf.Write(p)
		}
	}

	return len(p), nil
}

// Bytes returns what was kept.
func (b *limitedBuffer) Bytes() []byte { return b.buf.Bytes() }
