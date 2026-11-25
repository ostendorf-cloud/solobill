// Package logo checks a seller logo before it is stored and again before
// it is printed: a PNG or a JPEG that decodes completely and stays within
// a pixel budget. The upload limit bounds the file, not the image: a
// small PNG can declare an image of billions of pixels, which a decoder
// (Go's here, Typst's in internal/pdf) would try to allocate.
package logo

import (
	"bytes"
	"errors"
	"fmt"
	"image"

	// The two formats a logo may have. Registering them here keeps every
	// other format, SVG included, undecodable.
	_ "image/jpeg"
	_ "image/png"
)

// The pixel budget. A logo prints at most 60 mm by 11 mm, about 700 by
// 130 pixels at 300 dpi, so these caps leave a wide margin. A decoded
// image of MaxPixels takes 16 MiB as RGBA.
const (
	// MaxSide is the most pixels of either side.
	MaxSide = 4096
	// MaxPixels is the most pixels of the whole image (2048 by 2048).
	MaxPixels = 2048 * 2048
)

// Errors of Check.
var (
	// ErrFormat is neither a PNG nor a JPEG by its magic bytes.
	ErrFormat = errors.New("logo: neither a PNG nor a JPEG")
	// ErrDimensions is an image larger than MaxSide or MaxPixels, or of
	// zero size.
	ErrDimensions = errors.New("logo: image dimensions outside the allowed range")
	// ErrCorrupt is a PNG or a JPEG that does not decode.
	ErrCorrupt = errors.New("logo: the image does not decode")
)

var (
	magicPNG  = []byte("\x89PNG\r\n\x1a\n")
	magicJPEG = []byte{0xFF, 0xD8, 0xFF}
)

// Check reports whether data is a logo that can be printed. It reads the
// declared dimensions first and decodes the whole image only within the
// budget.
func Check(data []byte) error {
	if !bytes.HasPrefix(data, magicPNG) && !bytes.HasPrefix(data, magicJPEG) {
		return ErrFormat
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxSide || cfg.Height > MaxSide ||
		cfg.Width*cfg.Height > MaxPixels {
		return fmt.Errorf("%w: %d by %d pixels, at most %d per side and %d in total",
			ErrDimensions, cfg.Width, cfg.Height, MaxSide, MaxPixels)
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("%w: %w", ErrCorrupt, err)
	}

	return nil
}
