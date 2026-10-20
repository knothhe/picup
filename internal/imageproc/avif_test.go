package imageproc

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"testing"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/webp"
	"github.com/knothhe/picup/internal/config"
)

func TestAVIFOutputQualityTransparencyAndOriginalFormat(t *testing.T) {
	// AVIF uses its embedded codec even when no external converter is available.
	t.Setenv("PATH", t.TempDir())
	input := fixture(t)
	c := config.Default()
	c.Format, c.MaxWidth = "avif", 60
	var high Image
	var lowBytes int
	for _, quality := range []int{25, 90} {
		c.Quality = quality
		out, err := Process(context.Background(), input, "transparent.png", c)
		if err != nil {
			t.Fatal(err)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(out.Data))
		if err != nil || format != "avif" || cfg.Width != 60 || cfg.Height != 40 || out.Width != 60 || out.Height != 40 || out.Extension != "avif" || out.ContentType != "image/avif" {
			t.Fatalf("invalid AVIF conversion: image=%+v format=%q config=%+v err=%v", out, format, cfg, err)
		}
		decoded, _, err := image.Decode(bytes.NewReader(out.Data))
		if err != nil {
			t.Fatal("AVIF is not registered with the shared image decoder", err)
		}
		_, _, _, alpha := decoded.At(15, 10).RGBA()
		if alpha == 0 || alpha == 65535 {
			t.Fatal("AVIF lost partial transparency", alpha)
		}
		t.Logf("PNG %d bytes -> AVIF quality %d, 60x40, %d bytes", len(input), quality, len(out.Data))
		if quality == 25 {
			lowBytes = len(out.Data)
		} else {
			if len(out.Data) <= lowBytes {
				t.Fatal("AVIF quality did not affect encoded output size", lowBytes, len(out.Data))
			}
			high = out
		}
	}
	c.Format, c.Compress, c.MaxWidth = "original", false, 0
	original, err := Process(context.Background(), high.Data, "photo.avif", c)
	if err != nil || !bytes.Equal(original.Data, high.Data) || original.Extension != "avif" {
		t.Fatal("original AVIF bytes changed with compression disabled", err)
	}
	c.Compress, c.Quality, c.MaxWidth = true, 25, 30
	resized, err := Process(context.Background(), high.Data, "photo.avif", c)
	if err != nil || resized.Width != 30 || resized.Height != 20 || resized.Extension != "avif" {
		t.Fatal("original format AVIF did not recompress and resize", resized.Width, resized.Height, resized.Extension, err)
	}
	c.Format = "png"
	converted, err := Process(context.Background(), high.Data, "photo.avif", c)
	if err != nil || converted.Extension != "png" {
		t.Fatal("AVIF source could not convert to PNG", err)
	}
	if _, _, err := image.Decode(bytes.NewReader(converted.Data)); err != nil {
		t.Fatal(err)
	}
}

func TestAnimatedAVIFAndWebPKeepAllFramesForEveryOutput(t *testing.T) {
	first := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	second := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			first.SetNRGBA(x, y, color.NRGBA{R: 220, G: uint8(x * 10), A: 255})
			second.SetNRGBA(x, y, color.NRGBA{B: 220, G: uint8(y * 10), A: 255})
		}
	}
	var avifData, webpData bytes.Buffer
	if err := avif.EncodeAll(&avifData, &avif.AVIF{Image: []image.Image{first, second}, Delay: []float64{.1, .2}}, avif.Options{Quality: 80, QualityAlpha: 100, Speed: 10, ChromaSubsampling: image.YCbCrSubsampleRatio420}); err != nil {
		t.Fatal(err)
	}
	if err := webp.EncodeAll(&webpData, &webp.WEBP{Image: []image.Image{first, second}, Delay: []int{100, 200}}, webp.Options{Quality: 80, Method: webp.DefaultMethod}); err != nil {
		t.Fatal(err)
	}
	animation, err := avif.DecodeAll(bytes.NewReader(avifData.Bytes()))
	if err != nil || len(animation.Image) != 2 {
		t.Fatal("AVIF test fixture did not contain both frames", err)
	}
	for _, source := range []struct {
		format string
		data   []byte
	}{{"avif", avifData.Bytes()}, {"webp", webpData.Bytes()}} {
		for _, target := range []string{"original", "jpeg", "png", "webp", "avif"} {
			t.Run(source.format+"/"+target, func(t *testing.T) {
				c := config.Default()
				c.Format, c.MaxWidth = target, 2
				out, err := Process(context.Background(), source.data, "animation."+source.format, c)
				if err != nil || !bytes.Equal(out.Data, source.data) || out.Extension != source.format || out.ContentType != "image/"+source.format || out.Width != 16 || out.Height != 16 {
					t.Fatal("animation was flattened or its source format was mislabeled", out.Extension, out.Width, out.Height, err)
				}
			})
		}
	}
}

func TestAVIFAnimationBrandParsing(t *testing.T) {
	box := func(major, compatible string) []byte {
		b := make([]byte, 16+len(compatible))
		binary.BigEndian.PutUint32(b, uint32(len(b)))
		copy(b[4:8], "ftyp")
		copy(b[8:12], major)
		copy(b[16:], compatible)
		return b
	}
	for _, b := range [][]byte{box("avis", "avif"), box("avif", "mif1avis")} {
		if !isAnimatedAVIF(b) {
			t.Fatal("sequence brand was missed")
		}
	}
	for _, b := range [][]byte{nil, []byte("avis"), box("avif", "mif1"), append(box("avif", ""), []byte("avis")...)} {
		if isAnimatedAVIF(b) {
			t.Fatal("non-sequence data was misidentified")
		}
	}
	b := box("avif", "avis")
	binary.BigEndian.PutUint32(b, ^uint32(0))
	if isAnimatedAVIF(b) {
		t.Fatal("invalid box length accepted")
	}
}

func TestMalformedAVIFHeaderFailsWithoutPanicking(t *testing.T) {
	box := func(kind string, payload []byte) []byte {
		b := make([]byte, 8+len(payload))
		binary.BigEndian.PutUint32(b, uint32(len(b)))
		copy(b[4:8], kind)
		copy(b[8:], payload)
		return b
	}
	data := box("ftyp", append([]byte("avif"), make([]byte, 4)...))
	ispe := make([]byte, 12)
	binary.BigEndian.PutUint32(ispe[4:8], 1)
	binary.BigEndian.PutUint32(ispe[8:12], 1)
	meta := append(make([]byte, 4), box("pitm", nil)...)
	meta = append(meta, box("iprp", box("ipco", box("ispe", ispe)))...)
	data = append(data, box("meta", meta)...)
	// libavif's Go metadata fast path indexes the empty pitm payload. All
	// application-facing decoding paths must turn that panic into an error.
	if _, _, err := DecodeConfig(bytes.NewReader(data)); err == nil {
		t.Fatal("malformed AVIF configuration was accepted")
	}
	if _, err := Decode(data); err == nil {
		t.Fatal("malformed AVIF pixels were accepted")
	}
	if _, err := Process(context.Background(), data, "broken.avif", config.Default()); err == nil {
		t.Fatal("malformed AVIF upload was accepted")
	}
}

func TestAVIFDimensionsCannotOverflowPixelLimit(t *testing.T) {
	box := func(kind string, payload []byte) []byte {
		b := make([]byte, 8+len(payload))
		binary.BigEndian.PutUint32(b, uint32(len(b)))
		copy(b[4:8], kind)
		copy(b[8:], payload)
		return b
	}
	data := box("ftyp", append([]byte("avif"), make([]byte, 4)...))
	ispe := make([]byte, 12)
	binary.BigEndian.PutUint32(ispe[4:8], ^uint32(0))
	binary.BigEndian.PutUint32(ispe[8:12], ^uint32(0))
	meta := append(make([]byte, 4), box("pitm", make([]byte, 6))...)
	meta = append(meta, box("iprp", box("ipco", box("ispe", ispe)))...)
	data = append(data, box("meta", meta)...)
	c := config.Default()
	c.Compress = false
	if _, err := Process(context.Background(), data, "huge.avif", c); err == nil {
		t.Fatal("AVIF dimensions overflowed the 40-megapixel limit")
	}
	if _, err := Decode(data); err == nil {
		t.Fatal("oversized AVIF entered the pixel decoder")
	}
}
