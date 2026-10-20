package service

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gen2brain/webp"
	"github.com/knothhe/picup/internal/config"
)

func TestUploadSendsCompressedBytesAndAccurateResults(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 128, 96))
	for y := range 96 {
		for x := range 128 {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x*2 + x*y%11), G: uint8(y*2 + x*y%17), B: uint8(x + y), A: 255})
		}
	}
	var jpegInput, pngInput, webpInput bytes.Buffer
	if err := jpeg.Encode(&jpegInput, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	if err := (&png.Encoder{CompressionLevel: png.NoCompression}).Encode(&pngInput, img); err != nil {
		t.Fatal(err)
	}
	if err := webp.Encode(&webpInput, img, webp.Options{Quality: 100, Method: webp.DefaultMethod}); err != nil {
		t.Fatal(err)
	}
	for _, source := range []struct {
		name, target, wantFormat, wantExt string
		data                              []byte
	}{
		{"jpeg", "original", "jpeg", "jpg", jpegInput.Bytes()},
		{"png", "original", "png", "png", pngInput.Bytes()},
		{"webp", "original", "webp", "webp", webpInput.Bytes()},
		{"avif", "avif", "avif", "avif", pngInput.Bytes()},
	} {
		t.Run(source.name, func(t *testing.T) {
			type request struct {
				contentType string
				body        []byte
			}
			requests := make(chan request, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				requests <- request{r.Header.Get("Content-Type"), body}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			c := config.Default()
			c.Endpoint, c.Bucket, c.PublicURL = server.URL, "photos", "https://img.example.com"
			c.AccessKey, c.SecretKey, c.Format, c.Quality = "test", "secret", source.target, 60
			result, err := (Service{Config: c}).Upload(context.Background(), source.data, "photo."+source.name)
			if err != nil {
				t.Fatal(err)
			}
			uploaded := <-requests
			if len(uploaded.body) >= len(source.data) || bytes.Equal(uploaded.body, source.data) {
				t.Fatal("upload sent the source instead of compressed bytes", len(source.data), len(uploaded.body))
			}
			cfg, format, err := image.DecodeConfig(bytes.NewReader(uploaded.body))
			if err != nil || format != source.wantFormat || cfg.Width != 128 || cfg.Height != 96 || uploaded.contentType != "image/"+source.wantFormat || !strings.HasSuffix(result.Key, "."+source.wantExt) {
				t.Fatal("uploaded content does not match its format, dimensions, or metadata", cfg, format, uploaded.contentType, result.Key, err)
			}
			if result.Bytes != len(uploaded.body) || result.OriginalBytes != len(source.data) || result.Width != cfg.Width || result.Height != cfg.Height {
				t.Fatal("upload result reported inaccurate compression data", result)
			}
			t.Logf("uploaded %s: %d -> %d bytes, Content-Type %s", source.name, len(source.data), result.Bytes, uploaded.contentType)
		})
	}
}
