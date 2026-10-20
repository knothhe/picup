package gui

import (
	"context"
	"encoding/binary"
	"image"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/imageproc"
	"github.com/knothhe/picup/internal/service"
)

func malformedAVIFPreviewFixture(huge bool) []byte {
	box := func(kind string, payload []byte) []byte {
		b := make([]byte, 8+len(payload))
		binary.BigEndian.PutUint32(b, uint32(len(b)))
		copy(b[4:8], kind)
		copy(b[8:], payload)
		return b
	}
	data := box("ftyp", append([]byte("avif"), make([]byte, 4)...))
	ispe := make([]byte, 12)
	width := uint32(1)
	var pitm []byte
	if huge {
		width = ^uint32(0)
		pitm = make([]byte, 6)
	}
	binary.BigEndian.PutUint32(ispe[4:8], width)
	binary.BigEndian.PutUint32(ispe[8:12], width)
	meta := append(make([]byte, 4), box("pitm", pitm)...)
	meta = append(meta, box("iprp", box("ipco", box("ispe", ispe)))...)
	return append(data, box("meta", meta)...)
}

func TestAVIFPreviewsRejectDamagedAndOversizedMetadata(t *testing.T) {
	for _, huge := range []bool{false, true} {
		data := malformedAVIFPreviewFixture(huge)
		preview := readUploadPreview("broken.avif", data)
		if preview.err == nil || preview.image != nil {
			t.Fatal("invalid AVIF entered upload preview decoding", huge)
		}
		client := guiCloudClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(data) })
		for _, full := range []bool{false, true} {
			preview := readGalleryImage(context.Background(), client, "broken.avif", full)
			if preview.err == nil || preview.bitmap != nil {
				t.Fatal("invalid AVIF entered gallery preview decoding", huge, full)
			}
		}
	}
}

func TestAVIFSettingAutosavesAndPreviews(t *testing.T) {
	cfg := config.Default()
	a := &application{config: cfg, path: filepath.Join(t.TempDir(), "config.json"), quality: 85, tab: 2, settingsTab: 1}
	tt := ui.NewTester(nativeTestView(a), 1040, 760)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	if err := tt.Click("保留原格式"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("AVIF"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(settingsAutosaveDelay)
	saved, err := config.Load(a.path)
	if err != nil || saved.Format != "avif" {
		t.Fatal("AVIF selection did not autosave", err, saved.Format)
	}
	data := uploadPreviewFixture(t, "png", 240, 120)
	processed, err := imageproc.Process(context.Background(), data, "photo.png", saved)
	if err != nil {
		t.Fatal(err)
	}
	preview := readUploadPreview("photo.avif", processed.Data)
	if preview.err != nil || preview.image == nil || preview.image.Bounds().Size() != image.Pt(96, 48) {
		t.Fatal("new AVIF output cannot be previewed", preview.err)
	}
	client := guiCloudClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/avif")
		_, _ = w.Write(processed.Data)
	})
	for _, full := range []bool{false, true} {
		galleryImage := readGalleryImage(context.Background(), client, "photo.avif", full)
		if galleryImage.err != nil || galleryImage.bitmap == nil || galleryImage.width != 240 || galleryImage.height != 120 {
			t.Fatal("AVIF gallery thumbnail or full preview failed", full, galleryImage.err)
		}
	}
}

func TestUploadSizeSummaryShowsActualSavings(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		SetUILanguage(language)
		for _, test := range []struct {
			bytes int
			label string
		}{{500, tf("节省 %s", "50.0%")}, {1000, tr("体积未减少")}, {1250, tf("体积增加 %s", "25.0%")}} {
			result := &service.Result{OriginalBytes: 1000, Bytes: test.bytes, Width: 200, Height: 100}
			if got := uploadSizeSummary(result); !strings.Contains(got, test.label) {
				t.Fatal("summary hides the actual processing result", got)
			}
		}
	}
}
