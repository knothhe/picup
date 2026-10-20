package gui

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/gen2brain/webp"
	"github.com/knothhe/picup/internal/imageproc"
	"golang.org/x/image/tiff"
)

func uploadPreviewFixture(t *testing.T, format string, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 140, A: 255})
		}
	}
	var out bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&out, img)
	case "jpeg":
		err = jpeg.Encode(&out, img, nil)
	case "gif":
		err = gif.Encode(&out, img, nil)
	case "webp":
		err = webp.Encode(&out, img)
	case "tiff":
		err = tiff.Encode(&out, img, nil)
	default:
		t.Fatalf("unexpected fixture format %q", format)
	}
	if err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestReadUploadPreviewFormatsAndSize(t *testing.T) {
	for _, format := range []string{"png", "jpeg", "gif", "webp", "tiff"} {
		t.Run(format, func(t *testing.T) {
			data := uploadPreviewFixture(t, format, 240, 120)
			original := bytes.Clone(data)
			preview := readUploadPreview("", data)
			if preview.err != nil || preview.image == nil {
				t.Fatalf("preview failed: %+v", preview)
			}
			if preview.bytes != len(data) || preview.width != 240 || preview.height != 120 {
				t.Fatalf("original metadata was lost: %+v", preview)
			}
			if got := preview.image.Bounds().Size(); got != image.Pt(96, 48) {
				t.Fatalf("thumbnail dimensions = %v, want 96×48", got)
			}
			if !bytes.Equal(data, original) {
				t.Fatal("preview decoding changed clipboard bytes")
			}
		})
	}
	preview := readUploadPreview("", uploadPreviewFixture(t, "png", 12, 6))
	if preview.err != nil || preview.image.Bounds().Size() != image.Pt(12, 6) {
		t.Fatal("small previews should keep their source dimensions", preview.err)
	}
}

func TestReadUploadPreviewDoesNotChangeFile(t *testing.T) {
	data := uploadPreviewFixture(t, "png", 100, 200)
	path := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	preview := readUploadPreview(path, nil)
	if preview.err != nil || preview.image.Bounds().Size() != image.Pt(48, 96) {
		t.Fatal(preview.err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
		t.Fatal("loading a preview modified the source image")
	}
}

func TestReadUploadPreviewRejectsInvalidAndOversizedSources(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty.png":  {},
		"bad.png":    []byte("invalid image"),
		"photo.heic": []byte("\x00\x00\x00\x18ftypheic"),
		"photo.avif": []byte("\x00\x00\x00\x18ftypavif"),
	} {
		t.Run(name, func(t *testing.T) {
			preview := readUploadPreview(name, data)
			if preview.err == nil || preview.image != nil || preview.bytes != len(data) {
				t.Fatalf("unsupported input must use the fallback: %+v", preview)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "large.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(uploadPreviewMaxBytes + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	preview := readUploadPreview(path, nil)
	if preview.err == nil || preview.image != nil || preview.bytes != uploadPreviewMaxBytes+1 {
		t.Fatalf("large file was not rejected before reading: %+v", preview)
	}
	preview = readUploadPreview(t.TempDir(), nil)
	if preview.err == nil || preview.image != nil {
		t.Fatal("directory was accepted as an image")
	}
	preview = readUploadPreview(filepath.Join(t.TempDir(), "missing.png"), nil)
	if preview.err == nil || preview.image != nil {
		t.Fatal("missing file was accepted")
	}
	broken := uploadPreviewFixture(t, "png", 2, 2)
	broken = broken[:len(broken)-12]
	if _, _, err := image.DecodeConfig(bytes.NewReader(broken)); err != nil {
		t.Fatal("fixture must still have a valid image header", err)
	}
	preview = readUploadPreview("truncated.png", broken)
	if preview.err == nil || preview.image != nil || preview.width != 2 || preview.height != 2 {
		t.Fatalf("truncated pixels were accepted: %+v", preview)
	}

	// A valid header claims more pixels than we allow, while the compressed
	// image data still describes a small source. Reject it before allocation.
	data := uploadPreviewFixture(t, "png", 2, 2)
	binary.BigEndian.PutUint32(data[16:20], imageproc.MaxPixels)
	binary.BigEndian.PutUint32(data[20:24], 2)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	preview = readUploadPreview("large-dimensions.png", data)
	if preview.err == nil || preview.image != nil || preview.width != imageproc.MaxPixels {
		t.Fatalf("oversized dimensions were not rejected: %+v", preview)
	}
}

func TestReadUploadPreviewHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	preview := readUploadPreviewContext(ctx, "does-not-exist.png", nil)
	if !errors.Is(preview.err, context.Canceled) || preview.image != nil {
		t.Fatalf("canceled preview did work: %+v", preview)
	}
	reader := uploadPreviewReader{ctx: ctx, reader: bytes.NewReader([]byte("image"))}
	if n, err := reader.Read(make([]byte, 5)); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled reader returned %d, %v", n, err)
	}
}

func TestUploadPreviewsDoNotStartWithoutWindow(t *testing.T) {
	j := &job{path: "does-not-exist.png"}
	a := &application{jobs: []*job{j}}
	a.loadUploadPreviews(a.jobs)
	if j.previewLoading || j.previewDone || a.uploadPreviewCancel != nil {
		t.Fatal("headless rendering started preview IO")
	}
}

func TestPendingUploadPreviewSurvivesClipboardUpload(t *testing.T) {
	data := uploadPreviewFixture(t, "png", 180, 120)
	j := &job{data: data}
	var state uploadPreviewState
	state.source(j)
	// Upload success releases its copy before a queue change starts another
	// preview batch. The pending decode must still have its original source.
	j.data = nil
	resumed := state.source(j)
	preview := readUploadPreview(resumed.path, resumed.data)
	if preview.err != nil || preview.image == nil || preview.width != 180 || preview.height != 120 {
		t.Fatalf("clipboard source was lost when restarting a pending preview: %+v", preview)
	}
	if !bytes.Equal(resumed.data, data) {
		t.Fatal("pending clipboard source changed")
	}
}
