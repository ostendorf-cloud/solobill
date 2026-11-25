// Package pdf renders a document's frozen payload into a PDF with Typst
// (issue #74). It implements httpapi.DocumentRenderer and
// docissue.PayloadChecker.
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
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	logocheck "github.com/ostendorf-cloud/solobill/internal/logo"
)

// CurrentTemplateVersion is the template a new document is issued
// with. The issuing code freezes it into FrozenPayloadV1.TemplateVersion.
const CurrentTemplateVersion = "document-v2"

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
	"document-v2": "templates/document-v2.typ",
}

// logoTemplates are the template versions that print the seller logo.
// document-v1 has no place for one: a payload naming it and a logo is
// ErrUnsupported, so a stored PDF never silently lacks its logo.
var logoTemplates = map[string]bool{
	"document-v2": true,
}

// Errors of Render.
var (
	// ErrUnknownTemplate is a template version this build does not ship.
	ErrUnknownTemplate = errors.New("pdf: unknown template version")
	// ErrUnavailable is a renderer that cannot run: no Typst binary. Its
	// RendererUnavailable method lets the API answer 503 without
	// importing this package.
	ErrUnavailable error = unavailableError{}
	// ErrOutputTooLarge is a PDF larger than Options.MaxOutputBytes.
	ErrOutputTooLarge = errors.New("pdf: output too large")
	// ErrCompile is a Typst run that failed. The wrapped message carries
	// Typst's diagnostics.
	ErrCompile = errors.New("pdf: typst compile failed")
	// ErrTooManyPages is a PDF with more pages than its template
	// version's Limits.Pages. CheckPrintable refuses such a payload
	// before Typst runs; this is the check after it.
	ErrTooManyPages = errors.New("pdf: too many pages")
	// ErrLayoutOverflow is a text the template's overflow check found
	// outside its cell. Render returns it inside an *InvalidPayloadError
	// naming the field. The issuing code refuses the issue on it.
	ErrLayoutOverflow = errors.New("pdf: text does not fit its cell")
)

// unavailableError is ErrUnavailable's type.
type unavailableError struct{}

func (unavailableError) Error() string { return "pdf: renderer unavailable" }

// RendererUnavailable marks the error for httpapi.
func (unavailableError) RendererUnavailable() bool { return true }

// overflowMessage is the template's overflow assertion in Typst's
// diagnostics; its group is the field path.
var overflowMessage = regexp.MustCompile(`assertion failed: overflow: ([^\s"]+)`)

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
// number. A payload that names a logo needs RenderWithLogo.
func (r *Renderer) Render(ctx context.Context, payload []byte, draft bool) ([]byte, error) {
	return r.RenderWithLogo(ctx, payload, draft, nil)
}

// RenderWithLogo is Render with the bytes of the logo file the payload
// names by SHA-256, nil when it names none. The bytes are checked
// against that SHA-256 (ErrLogoMismatch) and must be a PNG or a JPEG
// (ErrLogoFormat). It is httpapi.DocumentRenderer's method.
func (r *Renderer) RenderWithLogo(ctx context.Context, payload []byte, draft bool, logo []byte) ([]byte, error) {
	p, err := docpayload.Unmarshal(payload)
	if err != nil {
		return nil, fmt.Errorf("pdf: %w", err)
	}

	return r.RenderPayloadWithLogo(ctx, p, draft, logo)
}

// RenderPayload renders a decoded payload without a logo.
func (r *Renderer) RenderPayload(ctx context.Context, p docpayload.FrozenPayloadV1, draft bool) ([]byte, error) {
	return r.RenderPayloadWithLogo(ctx, p, draft, nil)
}

// RenderPayloadWithLogo renders a decoded payload with its logo bytes.
func (r *Renderer) RenderPayloadWithLogo(ctx context.Context, p docpayload.FrozenPayloadV1, draft bool, logo []byte) ([]byte, error) {
	src, err := BuildSourceWithLogo(p, draft, logo)
	if err != nil {
		return nil, err
	}

	return r.compile(ctx, src)
}

// CheckPayload implements docissue.PayloadChecker: the issuing code
// calls it before it freezes a payload, so an issued document can always
// be rendered later. It runs CheckPrintable and then Typst itself, the
// authority on the layout, and reports what cannot be printed as field
// details. The logo is left out of the Typst run: issuing never reads
// the bucket, and the logo sits outside the flow, so it changes no cell
// and no page break. Any other failure, such as a missing Typst binary,
// is an error and refuses the issue.
func (r *Renderer) CheckPayload(ctx context.Context, p docpayload.FrozenPayloadV1) (map[string]string, error) {
	details, err := checkDetails(CheckPrintable(p))
	if details != nil || err != nil {
		return details, err
	}

	q := p
	q.Seller.LogoSHA256 = ""
	_, err = r.RenderPayload(ctx, q, false)

	return checkDetails(err)
}

// checkDetails turns a refusal of a payload into field details and
// passes every other error through.
func checkDetails(err error) (map[string]string, error) {
	var invalid *InvalidPayloadError
	switch {
	case err == nil:
		return nil, nil
	case errors.As(err, &invalid):
		return invalid.Details, nil
	case errors.Is(err, ErrPayloadTooLarge):
		return map[string]string{"lines": "the document holds too much text to be printed"}, nil
	case errors.Is(err, ErrTooManyPages):
		return map[string]string{"lines": "the document needs more pages than can be printed"}, nil
	default:
		return nil, err
	}
}

// Source is everything one typst compile reads besides the fonts.
type Source struct {
	// Template is the Typst file of the payload's template version.
	Template []byte
	// Data is data.json, the formatted payload.
	Data []byte
	// Logo is the checked logo file, nil without one.
	Logo []byte
	// CreationTime is the PDF creation timestamp: the issue date at
	// midnight UTC, so the bytes are the same on every render.
	CreationTime time.Time
	// MaxPages is the template version's page limit, checked in the PDF.
	MaxPages int
}

// BuildSource resolves the template and formats the payload into the
// data file, without running Typst. The golden tests compare its Data.
// A payload that names a logo needs BuildSourceWithLogo.
func BuildSource(p docpayload.FrozenPayloadV1, draft bool) (Source, error) {
	return BuildSourceWithLogo(p, draft, nil)
}

// BuildSourceWithLogo is BuildSource with the logo bytes, checked
// against the payload's SHA-256 and the allowed formats.
func BuildSourceWithLogo(p docpayload.FrozenPayloadV1, draft bool, logo []byte) (Source, error) {
	in, data, err := buildData(p, draft)
	if err != nil {
		return Source{}, err
	}
	if err := checkLogo(p.Seller.LogoSHA256, logo); err != nil {
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

	return Source{
		Template: tmpl, Data: data, Logo: logo, CreationTime: created,
		MaxPages: limitsByTemplate[in.TemplateVersion].Pages,
	}, nil
}

// maxLogoBytes bounds the logo file. The logo upload allows 512 KiB.
const maxLogoBytes = 1 << 20

// checkLogo checks the logo bytes against the SHA-256 frozen in the
// payload. The bytes are trusted only by that hash, never by a name: no
// hash and no bytes is no logo, anything else must match exactly and be
// a PNG or a JPEG.
func checkLogo(want string, logo []byte) error {
	switch {
	case want == "" && logo == nil:
		return nil
	case want == "":
		return fmt.Errorf("%w: the payload names no logo", ErrLogoMismatch)
	case logo == nil:
		return fmt.Errorf("%w: the payload names logo %s and no logo was given", ErrLogoMismatch, want)
	}
	sum := sha256.Sum256(logo)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("%w: the logo has SHA-256 %s, the payload names %s", ErrLogoMismatch, got, want)
	}
	if len(logo) > maxLogoBytes {
		return fmt.Errorf("%w: %d bytes, at most %d", ErrLogoFormat, len(logo), maxLogoBytes)
	}
	// Again here, not only at upload: a stored logo is decoded within the
	// pixel budget before Typst ever sees it.
	if err := logocheck.Check(logo); err != nil {
		return fmt.Errorf("%w: %w", ErrLogoFormat, err)
	}

	return nil
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

	// #nosec G204: the binary is configuration, every argument is fixed
	// or a path inside dir.
	cmd := exec.CommandContext(ctx, bin, compileArgs(dir, src.CreationTime)...)
	cmd.Dir = dir
	cmd.Env = minimalEnv(dir)
	// The PDF streams to stdout into a capped buffer: past the limit the
	// buffer cancels the render, so Typst never writes an unbounded file.
	out := &cappedBuffer{max: r.opts.MaxOutputBytes, cancel: cancel}
	stderr := &limitedBuffer{max: maxStderrBytes}
	cmd.Stdout = out
	cmd.Stderr = stderr
	cmd.WaitDelay = time.Second

	err = cmd.Run()
	switch {
	case out.exceeded:
		return nil, fmt.Errorf("%w: more than %d bytes", ErrOutputTooLarge, r.opts.MaxOutputBytes)
	case err != nil && ctx.Err() != nil:
		return nil, fmt.Errorf("%w: %w", ErrCompile, ctx.Err())
	case err != nil:
		if m := overflowMessage.FindSubmatch(stderr.Bytes()); m != nil {
			return nil, &InvalidPayloadError{
				Details: map[string]string{string(m[1]): "does not fit its cell in the typeset document"},
				Err:     ErrLayoutOverflow,
			}
		}

		return nil, fmt.Errorf("%w: %w: %s", ErrCompile, err, bytes.TrimSpace(stderr.Bytes()))
	case !bytes.HasPrefix(out.buf.Bytes(), []byte("%PDF-")):
		return nil, fmt.Errorf("%w: output is no PDF", ErrCompile)
	}

	pdf := out.buf.Bytes()
	if src.MaxPages > 0 {
		pages, err := pageCount(pdf)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrCompile, err)
		}
		if pages > src.MaxPages {
			return nil, fmt.Errorf("%w: %d pages, at most %d", ErrTooManyPages, pages, src.MaxPages)
		}
	}

	return pdf, nil
}

// pagesCount matches the /Count of a page tree node; the root's is the
// largest.
var pagesCount = regexp.MustCompile(`/Type\s*/Pages\b[^>]*?/Count\s+(\d+)|/Count\s+(\d+)[^>]*?/Type\s*/Pages\b`)

// pageCount reads a PDF's page count from its page tree root. Typst
// writes the page tree uncompressed.
func pageCount(pdf []byte) (int, error) {
	count := -1
	for _, m := range pagesCount.FindAllSubmatch(pdf, -1) {
		n, err := strconv.Atoi(string(m[1]) + string(m[2]))
		if err != nil {
			return 0, fmt.Errorf("page count: %w", err)
		}
		count = max(count, n)
	}
	if count < 0 {
		return 0, errors.New("no page tree in the PDF")
	}

	return count, nil
}

// minimalEnv is Typst's whole environment: no TYPST_* variable from the
// host can add a font path, a package path or a root, and no proxy or
// home directory of the host is visible.
func minimalEnv(dir string) []string {
	return []string{"HOME=" + dir, "XDG_CACHE_HOME=" + filepath.Join(dir, "cache"), "TZ=UTC"}
}

// TypstVersion is the Typst release the templates and the PDF hashes in
// the tests are pinned to, the same as mise.toml.
const TypstVersion = "0.14.2"

// ErrTypstVersion is a Typst binary of another version than
// TypstVersion.
var ErrTypstVersion = errors.New("pdf: unexpected typst version")

// CheckVersion runs "typst --version" and compares it with TypstVersion.
// cmd/solobill calls it at startup, because another Typst release may
// lay out a stored payload differently.
func (r *Renderer) CheckVersion(ctx context.Context) error {
	bin, err := exec.LookPath(r.opts.Binary)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	ctx, cancel := context.WithTimeout(ctx, r.opts.Timeout)
	defer cancel()
	// #nosec G204: the binary is configuration, the argument is fixed.
	cmd := exec.CommandContext(ctx, bin, "--version")
	cmd.Env = minimalEnv(os.TempDir())
	out := &limitedBuffer{max: maxStderrBytes}
	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	// The output is "typst 0.14.2 (b33de9de)".
	fields := strings.Fields(string(out.Bytes()))
	if len(fields) < 2 || fields[0] != "typst" || fields[1] != TypstVersion {
		return fmt.Errorf("%w: %q, want %s", ErrTypstVersion, bytes.TrimSpace(out.Bytes()), TypstVersion)
	}

	return nil
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
		"--jobs", "1",
		"--format", "pdf",
		filepath.Join(dir, "main.typ"),
		// "-" writes the PDF to stdout.
		"-",
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
	if src.Logo != nil {
		// The one name the template reads the logo from.
		files["logo"] = src.Logo
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

// cappedBuffer collects the PDF. A write past max marks it exceeded,
// cancels the render and fails, which stops the copy from Typst.
type cappedBuffer struct {
	buf      bytes.Buffer
	max      int64
	cancel   context.CancelFunc
	exceeded bool
}

// Write appends p or, past the limit, cancels the render.
func (b *cappedBuffer) Write(p []byte) (int, error) {
	if int64(b.buf.Len())+int64(len(p)) > b.max {
		b.exceeded = true
		b.cancel()

		return 0, ErrOutputTooLarge
	}

	return b.buf.Write(p)
}
