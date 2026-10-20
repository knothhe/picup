package imageproc

import (
	"bytes"
	"fmt"
	"image"
	"io"

	"github.com/disintegration/imaging"
	"github.com/gen2brain/avif"
)

// A malformed image must fail its upload or preview instead of terminating the
// application when a third-party codec panics while parsing a damaged header.
func recoverImageError(err *error) {
	if recovered := recover(); recovered != nil {
		*err = fmt.Errorf("invalid image data: %v", recovered)
	}
}

func DecodeConfig(r io.Reader) (cfg image.Config, format string, err error) {
	defer recoverImageError(&err)
	return image.DecodeConfig(r)
}

// ValidDimensions checks the pixel limit without multiplying untrusted header
// dimensions, which may overflow even a 64-bit integer for malformed AVIF data.
func ValidDimensions(width, height int) bool {
	return width > 0 && height > 0 && width <= MaxPixels/height
}

// Decode applies the source's orientation, including AVIF irot/imir properties.
func Decode(data []byte) (img image.Image, err error) {
	defer recoverImageError(&err)
	if len(data) == 0 || len(data) > MaxBytes {
		return nil, fmt.Errorf("image must be between 1 byte and 100 MiB")
	}
	cfg, format, err := DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if !ValidDimensions(cfg.Width, cfg.Height) {
		return nil, fmt.Errorf("image exceeds 40 megapixels")
	}
	if format == "avif" {
		return avif.Decode(bytes.NewReader(data), avif.Options{AutoRotate: true})
	}
	return imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
}
