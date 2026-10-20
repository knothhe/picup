package imageproc

import (
	"bytes"
	"context"
	"github.com/gen2brain/webp"
	"github.com/knothhe/picup/internal/config"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 120, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 120; x++ {
			im.SetNRGBA(x, y, color.NRGBA{uint8(x * 2), uint8(y * 3), 100, uint8(x * 2)})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, im)
	return b.Bytes()
}
func TestCompressionResizeAndTransparency(t *testing.T) {
	b := fixture(t)
	c := config.Default()
	r, e := Process(context.Background(), b, "a.png", c)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Data) > len(b) || r.Extension != "png" {
		t.Fatal("PNG fallback")
	}
	decoded, _ := png.Decode(bytes.NewReader(r.Data))
	_, _, _, a := decoded.At(20, 20).RGBA()
	if a == 65535 {
		t.Fatal("lost alpha")
	}
	c.Format = "webp"
	c.MaxWidth = 60
	r, e = Process(context.Background(), b, "a.png", c)
	if e != nil {
		t.Fatal(e)
	}
	if r.Width != 60 || r.Height != 40 || r.ContentType != "image/webp" {
		t.Fatalf("bad conversion %+v", r)
	}
	im, e := webp.Decode(bytes.NewReader(r.Data))
	if e != nil {
		t.Fatal(e)
	}
	_, _, _, a = im.At(5, 5).RGBA()
	if a == 65535 {
		t.Fatal("WebP lost alpha")
	}
	c.Format = "png"
	c.MaxWidth = 1000
	r, e = Process(context.Background(), b, "a.png", c)
	if e != nil || r.Width != 120 {
		t.Fatal("image was enlarged", e)
	}
}
func TestOriginalAndAnimatedPreserved(t *testing.T) {
	c := config.Default()
	c.Compress = false
	b := fixture(t)
	r, e := Process(context.Background(), b, "a.png", c)
	if e != nil || !bytes.Equal(b, r.Data) {
		t.Fatal("no-compress changed source", e)
	}
	palette := color.Palette{color.Black, color.White}
	frames := []*image.Paletted{image.NewPaletted(image.Rect(0, 0, 10, 10), palette), image.NewPaletted(image.Rect(0, 0, 10, 10), palette)}
	var buf bytes.Buffer
	gif.EncodeAll(&buf, &gif.GIF{Image: frames, Delay: []int{10, 10}})
	c.Compress = true
	c.Format = "webp"
	c.MaxWidth = 2
	r, e = Process(context.Background(), buf.Bytes(), "a.gif", c)
	if e != nil || !bytes.Equal(r.Data, buf.Bytes()) || r.Extension != "gif" {
		t.Fatal("animation changed", e)
	}
	if _, e = Process(context.Background(), []byte("not an image"), "evil.png", c); e == nil {
		t.Fatal("invalid image accepted")
	}
}
func TestJPEGQualityAndContentHash(t *testing.T) {
	im := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			im.Set(x, y, color.RGBA{uint8(x * y), uint8(x), uint8(y), 255})
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, im, &jpeg.Options{Quality: 100})
	c := config.Default()
	c.Quality = 60
	r, e := Process(context.Background(), b.Bytes(), "a.jpg", c)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Data) >= b.Len() || r.Extension != "jpg" || len(r.Hash) != 64 {
		t.Fatal("JPEG compression or hash failed")
	}
	r2, _ := Process(context.Background(), b.Bytes(), "renamed.jpg", c)
	if r.Hash != r2.Hash {
		t.Fatal("key depends on filename")
	}
}
