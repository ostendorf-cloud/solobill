package logo_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/logo"
)

func encodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func encodeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func chunk(typ string, data []byte) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, uint32(len(data)))
	b.WriteString(typ)
	b.Write(data)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(append([]byte(typ), data...)))

	return b.Bytes()
}

// pngHeader is a PNG of a few dozen bytes whose IHDR declares w by h
// pixels, followed by idat as its only image data.
func pngHeader(w, h uint32, idat []byte) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 2 // 8 bit RGB
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	b.Write(chunk("IHDR", ihdr))
	b.Write(chunk("IDAT", idat))
	b.Write(chunk("IEND", nil))

	return b.Bytes()
}

func TestCheck(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   func(t *testing.T) []byte
		wantErr error
	}{
		{name: "a small png", input: func(t *testing.T) []byte { return encodePNG(t, 120, 40) }},
		{name: "a small jpeg", input: func(t *testing.T) []byte { return encodeJPEG(t, 120, 40) }},
		{name: "a png at the pixel budget", input: func(t *testing.T) []byte { return encodePNG(t, 4096, 1024) }},
		{name: "a gif", input: func(*testing.T) []byte { return []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00") }, wantErr: logo.ErrFormat},
		{name: "an svg", input: func(*testing.T) []byte { return []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`) }, wantErr: logo.ErrFormat},
		{name: "empty", input: func(*testing.T) []byte { return nil }, wantErr: logo.ErrFormat},
		{
			name:    "a small file declaring a huge image",
			input:   func(*testing.T) []byte { return pngHeader(100000, 100000, []byte{0x78, 0x9c, 0x03, 0x00}) },
			wantErr: logo.ErrDimensions,
		},
		{name: "a side over the cap", input: func(*testing.T) []byte { return pngHeader(4097, 1, nil) }, wantErr: logo.ErrDimensions},
		{name: "over the pixel budget", input: func(*testing.T) []byte { return pngHeader(4096, 1025, nil) }, wantErr: logo.ErrDimensions},
		{name: "zero width", input: func(*testing.T) []byte { return pngHeader(0, 10, nil) }, wantErr: logo.ErrCorrupt},
		{
			name:    "a corrupt png with a valid header",
			input:   func(*testing.T) []byte { return pngHeader(16, 16, []byte("not zlib data at all")) },
			wantErr: logo.ErrCorrupt,
		},
		{
			name: "a truncated png",
			input: func(t *testing.T) []byte {
				b := encodePNG(t, 120, 40)

				return b[:len(b)/2]
			},
			wantErr: logo.ErrCorrupt,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := logo.Check(tt.input(t))
			if tt.wantErr == nil && err != nil {
				t.Fatalf("Check() = %v, want nil", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("Check() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
