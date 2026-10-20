package imageproc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/gen2brain/avif"
	"github.com/gen2brain/webp"
	"github.com/knothhe/picup/internal/config"
	_ "golang.org/x/image/tiff"
)

const MaxBytes = 100 << 20
const MaxPixels = 40_000_000

type Image struct {
	Data                         []byte
	Extension, ContentType, Hash string
	OriginalBytes                int
	Width, Height                int
}

func ReadFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular image file: %s", path)
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if len(b) > MaxBytes {
		return nil, fmt.Errorf("image exceeds 100 MiB")
	}
	return b, err
}

func Process(ctx context.Context, data []byte, name string, c config.Config) (out Image, err error) {
	defer recoverImageError(&err)
	return process(ctx, data, name, c)
}

func process(ctx context.Context, data []byte, name string, c config.Config) (Image, error) {
	out := Image{OriginalBytes: len(data)}
	if len(data) == 0 || len(data) > MaxBytes {
		return out, fmt.Errorf("image must be between 1 byte and 100 MiB")
	}
	if err := c.ValidateOptions(); err != nil {
		return out, err
	}
	converted := false
	cfg, format, err := DecodeConfig(bytes.NewReader(data))
	if err != nil {
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".heic" && ext != ".heif" {
			return out, fmt.Errorf("unsupported or invalid image: %w", err)
		}
		data, err = convert(ctx, data, ext)
		if err != nil {
			return out, err
		}
		converted = true
		cfg, format, err = DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return out, err
		}
	}
	if !ValidDimensions(cfg.Width, cfg.Height) {
		return out, fmt.Errorf("image exceeds 40 megapixels")
	}
	out.Width, out.Height = cfg.Width, cfg.Height
	target := c.Format
	if target == "original" {
		target = format
		if format == "tiff" {
			target = "png"
		}
		if converted {
			target = "webp"
		}
	}
	// Preserve animated formats rather than silently flattening their frames.
	animated := format == "gif" || isAnimatedPNG(data) || isAnimatedWebP(data) || isAnimatedAVIF(data)
	if animated {
		return finish(out, data, format), nil
	}
	resize := c.MaxWidth > 0 && cfg.Width > c.MaxWidth
	if !c.Compress && !resize && target == format {
		return finish(out, data, format), nil
	}
	img, err := Decode(data)
	if err != nil {
		return out, err
	}
	if c.MaxWidth > 0 && img.Bounds().Dx() > c.MaxWidth {
		img = imaging.Resize(img, c.MaxWidth, 0, imaging.Lanczos)
		resize = true
	}
	var buf bytes.Buffer
	switch target {
	case "jpeg":
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: c.Quality})
	case "png":
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		err = enc.Encode(&buf, img)
	case "webp":
		err = webp.Encode(&buf, img, webp.Options{Quality: c.Quality, Method: webp.DefaultMethod})
	case "avif":
		err = avif.Encode(&buf, img, avif.Options{Quality: c.Quality, QualityAlpha: 100, Speed: 8, ChromaSubsampling: image.YCbCrSubsampleRatio420})
	default:
		return out, fmt.Errorf("unsupported output format %q", target)
	}
	if err != nil {
		return out, err
	}
	out.Width, out.Height = img.Bounds().Dx(), img.Bounds().Dy()
	// A requested resize or conversion takes priority over a size-only fallback.
	if !resize && target == format && buf.Len() >= len(data) {
		out.Width, out.Height = cfg.Width, cfg.Height
		return finish(out, data, format), nil
	}
	return finish(out, buf.Bytes(), target), nil
}

func finish(out Image, data []byte, format string) Image {
	out.Data = data
	out.Extension = format
	if format == "jpeg" {
		out.Extension = "jpg"
	}
	out.ContentType = "image/" + format
	out.Hash = fmt.Sprintf("%x", sha256.Sum256(data))
	return out
}
func isAnimatedPNG(b []byte) bool {
	if len(b) < 8 || string(b[1:4]) != "PNG" {
		return false
	}
	for i := 8; i+12 <= len(b); {
		n := int(binary.BigEndian.Uint32(b[i : i+4]))
		if n > len(b)-i-12 {
			return false
		}
		if string(b[i+4:i+8]) == "acTL" {
			return true
		}
		i += n + 12
	}
	return false
}
func isAnimatedWebP(b []byte) bool {
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return false
	}
	for i := 12; i+8 <= len(b); {
		n := int(binary.LittleEndian.Uint32(b[i+4 : i+8]))
		if n > len(b)-i-8 {
			return false
		}
		if string(b[i:i+4]) == "ANIM" {
			return true
		}
		i += 8 + n + (n % 2)
	}
	return false
}

func convert(ctx context.Context, b []byte, ext string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "picup-convert-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	src, dst := filepath.Join(dir, "source"+ext), filepath.Join(dir, "image.png")
	if err = os.WriteFile(src, b, 0600); err != nil {
		return nil, err
	}
	var cmd *exec.Cmd
	if p, e := exec.LookPath("magick"); e == nil {
		cmd = exec.CommandContext(ctx, p, src+"[0]", "-auto-orient", dst)
	} else if runtime.GOOS == "darwin" {
		cmd = exec.CommandContext(ctx, "sips", "-s", "format", "png", src, "--out", dst)
	} else {
		return nil, fmt.Errorf("HEIC/HEIF conversion requires ImageMagick (magick)")
	}
	if err = cmd.Run(); err != nil {
		return nil, fmt.Errorf("HEIC/HEIF conversion failed: %w", err)
	}
	return ReadFile(dst)
}
