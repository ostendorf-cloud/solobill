package pdf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)

	return hex.EncodeToString(sum[:])
}

func TestCheckLogo(t *testing.T) {
	t.Parallel()
	png := readLogo(t)
	gif := []byte("GIF89a\x01\x00\x01\x00")
	large := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, maxLogoBytes)...)
	corrupt := png[:len(png)/2]
	cases := []struct {
		name    string
		want    string
		logo    []byte
		wantErr error
	}{
		{name: "no logo", want: "", logo: nil},
		{name: "matching png", want: sha256Hex(png), logo: png},
		{name: "named but not given", want: sha256Hex(png), logo: nil, wantErr: ErrLogoMismatch},
		{name: "given but not named", want: "", logo: png, wantErr: ErrLogoMismatch},
		{name: "other bytes", want: sha256Hex(png), logo: append(bytes.Clone(png), 0), wantErr: ErrLogoMismatch},
		{name: "neither png nor jpeg", want: sha256Hex(gif), logo: gif, wantErr: ErrLogoFormat},
		{name: "too large", want: sha256Hex(large), logo: large, wantErr: ErrLogoFormat},
		{name: "a png that does not decode", want: sha256Hex(corrupt), logo: corrupt, wantErr: ErrLogoFormat},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := checkLogo(c.want, c.logo)
			if c.wantErr == nil && err != nil {
				t.Fatalf("checkLogo = %v, want nil", err)
			}
			if c.wantErr != nil && !errors.Is(err, c.wantErr) {
				t.Fatalf("checkLogo = %v, want %v", err, c.wantErr)
			}
		})
	}
}

// TestLogoTemplateVersion: only a template with a place for the logo
// accepts a payload naming one. document-v1 refuses it in the pre
// check, so it can never be issued, and at render.
func TestLogoTemplateVersion(t *testing.T) {
	t.Parallel()
	v1 := loadFixture(t, "offer-de-v1")
	v1.Seller.LogoSHA256 = logoSHA256(t)
	if err := CheckPrintable(v1); !errors.Is(err, ErrUnsupported) {
		t.Errorf("CheckPrintable(v1 with logo) = %v, want ErrUnsupported", err)
	}
	if _, err := BuildSourceWithLogo(v1, false, readLogo(t)); !errors.Is(err, ErrUnsupported) {
		t.Errorf("BuildSourceWithLogo(v1 with logo) = %v, want ErrUnsupported", err)
	}

	v2 := loadFixture(t, "invoice-logo-de")
	if err := CheckPrintable(v2); err != nil {
		t.Errorf("CheckPrintable(v2 with logo) = %v", err)
	}
	src, err := BuildSourceWithLogo(v2, false, readLogo(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(src.Logo, readLogo(t)) {
		t.Error("the source does not carry the logo")
	}
	if _, err := BuildSource(v2, false); !errors.Is(err, ErrLogoMismatch) {
		t.Errorf("BuildSource(v2 with logo, no bytes) = %v, want ErrLogoMismatch", err)
	}
}

// TestRenderLogo renders the same invoice with and without the logo.
// The logo is an image in the PDF, and the text a reader sees is the
// same: the logo sits outside the flow and moves nothing.
func TestRenderLogo(t *testing.T) {
	bin := typstOrSkip(t)
	pdftotext := toolOrSkip(t, "pdftotext")
	r := New(Options{Binary: bin, TempDir: t.TempDir()})

	with := loadFixture(t, "invoice-logo-de")
	without := with
	without.Seller.LogoSHA256 = ""

	withPDF, err := r.RenderPayloadWithLogo(context.Background(), with, false, readLogo(t))
	if err != nil {
		t.Fatal(err)
	}
	withoutPDF, err := r.RenderPayload(context.Background(), without, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(withPDF, []byte("/Subtype /Image")) {
		t.Error("the PDF with a logo holds no image")
	}
	if bytes.Contains(withoutPDF, []byte("/Subtype /Image")) {
		t.Error("the PDF without a logo holds an image")
	}

	dir := t.TempDir()
	text := func(name string, pdf []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, pdf, 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.CommandContext(context.Background(), pdftotext, "-layout", "-enc", "UTF-8", path, "-").Output()
		if err != nil {
			t.Fatal(err)
		}

		return string(out)
	}
	if a, b := text("with.pdf", withPDF), text("without.pdf", withoutPDF); a != b {
		t.Errorf("the logo changes the text layout:\nwith:\n%s\nwithout:\n%s", a, b)
	}

	if _, err := r.RenderPayloadWithLogo(context.Background(), with, false, append(readLogo(t), 0)); !errors.Is(err, ErrLogoMismatch) {
		t.Errorf("render with other logo bytes = %v, want ErrLogoMismatch", err)
	}
}

// TestCheckPayload covers the issuing check: a printable payload passes,
// one that cannot be printed comes back as field details, a payload
// naming a logo is checked without its bytes, and a renderer without
// Typst refuses with an error, never with a silent pass.
func TestCheckPayload(t *testing.T) {
	bin := typstOrSkip(t)
	r := New(Options{Binary: bin, TempDir: t.TempDir()})
	ctx := context.Background()

	if details, err := r.CheckPayload(ctx, loadFixture(t, "offer-de")); err != nil || details != nil {
		t.Errorf("CheckPayload(offer-de) = %v, %v, want nil, nil", details, err)
	}
	if details, err := r.CheckPayload(ctx, loadFixture(t, "invoice-logo-de")); err != nil || details != nil {
		t.Errorf("CheckPayload(invoice-logo-de) = %v, %v, want nil, nil", details, err)
	}

	long := loadFixture(t, "offer-de")
	long.Customer.Name = strings.Repeat("W", 120)
	details, err := r.CheckPayload(ctx, long)
	if err != nil {
		t.Fatalf("CheckPayload(long name) error = %v, want field details", err)
	}
	if details["customer.name"] == "" {
		t.Errorf("CheckPayload(long name) = %v, want a detail for customer.name", details)
	}

	noTypst := New(Options{Binary: filepath.Join(t.TempDir(), "no-typst")})
	if _, err := noTypst.CheckPayload(ctx, loadFixture(t, "offer-de")); !errors.Is(err, ErrUnavailable) {
		t.Errorf("CheckPayload without typst = %v, want ErrUnavailable", err)
	}
}
