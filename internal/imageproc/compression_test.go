package imageproc

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/gen2brain/webp"
	"github.com/knothhe/picup/internal/config"
)

func compressionFixture() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 384, 256))
	for y := range 256 {
		for x := range 384 {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x/2 + (x*y)%23), G: uint8(y + (x*y)%31), B: uint8((x+y)/3 + (x*y)%17), A: 255})
		}
	}
	return img
}

func TestCompressionChangesRealEncodedJPEGPNGAndWebPBytes(t *testing.T) {
	img := compressionFixture()
	var jpegInput, pngInput, webpInput, optimizedPNG bytes.Buffer
	if err := jpeg.Encode(&jpegInput, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	if err := (&png.Encoder{CompressionLevel: png.NoCompression}).Encode(&pngInput, img); err != nil {
		t.Fatal(err)
	}
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&optimizedPNG, img); err != nil {
		t.Fatal(err)
	}
	if err := webp.Encode(&webpInput, img, webp.Options{Quality: 100, Method: webp.DefaultMethod}); err != nil {
		t.Fatal(err)
	}
	for _, source := range []struct {
		name string
		data []byte
	}{{"jpeg", jpegInput.Bytes()}, {"png", pngInput.Bytes()}, {"webp", webpInput.Bytes()}} {
		t.Run(source.name, func(t *testing.T) {
			c := config.Default()
			c.Quality = 60
			out, err := Process(context.Background(), source.data, "sample."+source.name, c)
			if err != nil || len(out.Data) >= len(source.data) || out.OriginalBytes != len(source.data) {
				t.Fatal("compression did not reduce the encoded image", len(source.data), len(out.Data), err)
			}
			cfg, format, err := image.DecodeConfig(bytes.NewReader(out.Data))
			if err != nil || format != source.name || cfg.Width != 384 || cfg.Height != 256 {
				t.Fatal("compressed bytes have the wrong format or dimensions", cfg, format, err)
			}
			t.Logf("%s original format, quality 60, 384x256: %d -> %d bytes, saved %.2f%%", source.name, len(source.data), len(out.Data), 100*(1-float64(len(out.Data))/float64(len(source.data))))
			c.Compress = false
			preserved, err := Process(context.Background(), source.data, "sample."+source.name, c)
			if err != nil || !bytes.Equal(source.data, preserved.Data) {
				t.Fatal("turning compression off did not preserve the source", err)
			}
		})
	}
	c := config.Default()
	out, err := Process(context.Background(), optimizedPNG.Bytes(), "optimized.png", c)
	if err != nil || !bytes.Equal(out.Data, optimizedPNG.Bytes()) {
		t.Fatal("optimized lossless PNG should keep its original bytes", err)
	}
	c.Quality = 1
	lowQuality, err := Process(context.Background(), optimizedPNG.Bytes(), "optimized.png", c)
	if err != nil || !bytes.Equal(out.Data, lowQuality.Data) {
		t.Fatal("lossy quality unexpectedly altered PNG output", err)
	}
	t.Logf("already optimized PNG: %d -> %d bytes; PNG is lossless and ignores lossy quality", optimizedPNG.Len(), len(out.Data))
}
