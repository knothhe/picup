package imageproc

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/knothhe/picup/internal/config"
)

func orientedJPEGFixture(t *testing.T, width, height, orientation, quality int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x*y + x*17), G: uint8(y*31 + x*7), B: uint8(x*y*13 + y), A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatal(err)
	}
	// Minimal little-endian EXIF containing the primary image's orientation.
	exif := make([]byte, 32)
	copy(exif, []byte("Exif\x00\x00II"))
	binary.LittleEndian.PutUint16(exif[8:10], 42)
	binary.LittleEndian.PutUint32(exif[10:14], 8)
	binary.LittleEndian.PutUint16(exif[14:16], 1)
	binary.LittleEndian.PutUint16(exif[16:18], 0x0112)
	binary.LittleEndian.PutUint16(exif[18:20], 3)
	binary.LittleEndian.PutUint32(exif[20:24], 1)
	binary.LittleEndian.PutUint16(exif[24:26], uint16(orientation))
	data := append([]byte{}, encoded.Bytes()[:2]...)
	data = append(data, 0xff, 0xe1, 0, byte(len(exif)+2))
	data = append(data, exif...)
	return append(data, encoded.Bytes()[2:]...)
}

func assertVisualDimensions(t *testing.T, out Image, width, height int) {
	t.Helper()
	decoded, err := Decode(out.Data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Size() != image.Pt(width, height) || out.Width != width || out.Height != height {
		t.Fatalf("visual dimensions %v and reported dimensions %dx%d must both equal %dx%d", decoded.Bounds().Size(), out.Width, out.Height, width, height)
	}
}

func TestRotatedJPEGResizesByVisualWidthWithoutCompression(t *testing.T) {
	for _, orientation := range []int{5, 6, 7, 8} {
		t.Run(fmt.Sprint(orientation), func(t *testing.T) {
			data := orientedJPEGFixture(t, 100, 200, orientation, 80)
			cfg := config.Default()
			cfg.Compress, cfg.MaxWidth = false, 150
			out, err := Process(context.Background(), data, "rotated.jpg", cfg)
			if err != nil {
				t.Fatal(err)
			}
			assertVisualDimensions(t, out, 150, 75)
		})
	}
}

func TestRotatedJPEGPassthroughPreservesBytesAndReportsVisualDimensions(t *testing.T) {
	for _, test := range []struct {
		width, height, maxWidth int
	}{{100, 200, 0}, {100, 200, 200}, {100, 200, 300}, {200, 100, 150}} {
		t.Run(fmt.Sprintf("%dx%d/max=%d", test.width, test.height, test.maxWidth), func(t *testing.T) {
			data := orientedJPEGFixture(t, test.width, test.height, 6, 80)
			cfg := config.Default()
			cfg.Compress, cfg.MaxWidth = false, test.maxWidth
			out, err := Process(context.Background(), data, "rotated.jpg", cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out.Data, data) {
				t.Fatal("an image already within the visual width limit was re-encoded")
			}
			assertVisualDimensions(t, out, test.height, test.width)
		})
	}
}

func TestRotatedJPEGSizeFallbackKeepsVisualDimensions(t *testing.T) {
	data := orientedJPEGFixture(t, 100, 200, 6, 1)
	cfg := config.Default()
	cfg.Quality = 100
	out, err := Process(context.Background(), data, "rotated.jpg", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Data, data) {
		t.Fatal("a larger same-format encoding replaced the original")
	}
	assertVisualDimensions(t, out, 200, 100)
}
