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

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/gen2brain/webp"
	"github.com/knothhe/picup/internal/imageproc"
	"github.com/knothhe/picup/internal/service"
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

func TestCompletedClipboardPreviewSurvivesImageToolsNavigation(t *testing.T) {
	// Successful clipboard uploads release their encoded source. Their decoded
	// thumbnail is the only local preview that can survive a page round trip.
	bitmap := ui.NewBitmap(image.NewNRGBA(image.Rect(0, 0, 48, 32)))
	upload := &job{id: 1, name: "clipboard.png", preview: bitmap, previewDone: true, result: &service.Result{URL: "https://images.example/clipboard.png"}}
	toolBitmap := ui.NewBitmap(image.NewNRGBA(image.Rect(0, 0, 32, 48)))
	tool := &imageToolJob{source: &job{id: 1, name: "local.png", preview: toolBitmap, previewDone: true}}
	a := &application{win: &mygo.Window{}, jobs: []*job{upload}, imageTools: imageToolsState{jobs: []*imageToolJob{tool}}, uploadPreviews: uploadPreviewState{jobs: []*job{upload}}}
	a.loadUploadPreviews([]*job{tool.source})
	if upload.preview != bitmap || !upload.previewDone || upload.previewLoading {
		t.Fatal("opening image tools discarded the completed clipboard thumbnail")
	}
	a.loadUploadPreviews(a.jobs)
	if upload.preview != bitmap || !upload.previewDone || upload.previewLoading || a.uploadPreviewCancel != nil {
		t.Fatal("returning to uploads tried to decode a released clipboard source")
	}
	if tool.source.preview != toolBitmap || !tool.source.previewDone {
		t.Fatal("returning to uploads discarded the live local thumbnail")
	}
}

func TestPreviewNavigationReleasesRemovedJobsAndCanceledSourceSnapshots(t *testing.T) {
	removed := &job{id: 1, preview: ui.NewBitmap(image.NewNRGBA(image.Rect(0, 0, 8, 8))), previewDone: true}
	tool := &imageToolJob{source: &job{id: 2, previewDone: true}}
	a := &application{win: &mygo.Window{}, imageTools: imageToolsState{jobs: []*imageToolJob{tool}}, uploadPreviews: uploadPreviewState{jobs: []*job{removed}, pending: map[*job]uploadPreviewSource{removed: {job: removed, data: []byte("released encoded source")}}}}
	a.loadUploadPreviews([]*job{tool.source})
	if removed.preview != nil || removed.previewDone || removed.previewLoading || len(a.uploadPreviews.pending) != 0 {
		t.Fatal("navigation retained a removed row or its encoded source")
	}
}

func TestPendingClipboardPreviewKeepsItsOnlySourceAcrossToolNavigation(t *testing.T) {
	data := uploadPreviewFixture(t, "png", 180, 120)
	upload := &job{id: 1, name: "clipboard.png", previewLoading: true, result: &service.Result{URL: "https://images.example/clipboard.png"}}
	tool := &imageToolJob{source: &job{id: 2, previewDone: true}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &application{win: &mygo.Window{}, jobs: []*job{upload}, imageTools: imageToolsState{jobs: []*imageToolJob{tool}}, uploadPreviewCancel: cancel, uploadPreviews: uploadPreviewState{jobs: []*job{upload}, ctx: ctx, pending: map[*job]uploadPreviewSource{upload: {job: upload, data: data}}}}
	a.loadUploadPreviews([]*job{tool.source})
	source, exists := a.uploadPreviews.pending[upload]
	if !exists || !bytes.Equal(source.data, data) || ctx.Err() == nil {
		t.Fatal("navigation discarded the only encoded source of a pending clipboard preview")
	}
	resumed := a.uploadPreviews.source(upload)
	preview := readUploadPreview(resumed.path, resumed.data)
	if preview.err != nil || preview.image == nil || preview.width != 180 || preview.height != 120 {
		t.Fatal("returning to uploads could not resume its pending clipboard preview", preview.err)
	}
	// Once a decoded thumbnail exists, the encoded snapshot is expendable even
	// if the currently visible local rows have not changed.
	upload.preview, upload.previewDone = ui.NewBitmap(preview.image), true
	a.loadUploadPreviews([]*job{tool.source})
	if len(a.uploadPreviews.pending) != 0 {
		t.Fatal("a completed thumbnail retained its full encoded clipboard source")
	}
}

func TestPreviewNavigationReleasesOffPageRemovedClipboardSources(t *testing.T) {
	upload := &job{id: 1, name: "clipboard.png"}
	tool := &imageToolJob{source: &job{id: 2, previewDone: true}}
	a := &application{win: &mygo.Window{}, imageTools: imageToolsState{jobs: []*imageToolJob{tool}}, uploadPreviews: uploadPreviewState{jobs: []*job{tool.source}, pending: map[*job]uploadPreviewSource{upload: {job: upload, data: []byte("removed clipboard source")}}}}
	a.loadUploadPreviews([]*job{tool.source})
	if len(a.uploadPreviews.pending) != 0 {
		t.Fatal("unchanged local rows retained a removed off-page clipboard source")
	}
}
