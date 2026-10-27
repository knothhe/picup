package imagetools

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/knothhe/picup/internal/imageproc"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 4), G: uint8(y * 8), B: 99, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestExportConvertsResizesAndPreservesOriginal(t *testing.T) {
	dir := t.TempDir()
	data := fixture(t)
	source := filepath.Join(dir, "original.png")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	options := DefaultOptions()
	options.Format, options.MaxWidth = "jpeg", 16
	result, err := Export(context.Background(), Input{Path: source}, dir, options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Path == source || result.Name != "original-processed.jpg" || result.Extension != "jpg" || result.ContentType != "image/jpeg" {
		t.Fatalf("unexpected export destination or format: %+v", result)
	}
	original, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(original, data) {
		t.Fatal("original was changed", err)
	}
	exported, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, format, err := imageproc.DecodeConfig(bytes.NewReader(exported))
	if err != nil || format != "jpeg" || decoded.Width != 16 || decoded.Height != 8 {
		t.Fatal("exported image does not match requested conversion", decoded, format, err)
	}
	if result.OriginalBytes != len(data) || result.Bytes != len(exported) || result.Width != 16 || result.Height != 8 {
		t.Fatalf("export metrics do not match the file: %+v", result)
	}
}

func TestConcurrentExportsNeverOverwriteExistingFiles(t *testing.T) {
	dir := t.TempDir()
	data := fixture(t)
	reserved := filepath.Join(dir, "photo-processed.png")
	if err := os.WriteFile(reserved, []byte("existing file"), 0600); err != nil {
		t.Fatal(err)
	}
	const count = 8
	results := make([]Result, count)
	errorsByIndex := make([]error, count)
	var workers sync.WaitGroup
	for i := range count {
		workers.Go(func() {
			results[i], errorsByIndex[i] = Export(context.Background(), Input{Name: "photo.png", Data: data}, dir, DefaultOptions())
		})
	}
	workers.Wait()
	seen := make(map[string]bool)
	for i, result := range results {
		if errorsByIndex[i] != nil {
			t.Fatal(errorsByIndex[i])
		}
		if result.Path == reserved || seen[result.Path] {
			t.Fatal("export reused an existing destination", result.Path)
		}
		seen[result.Path] = true
		got, err := os.ReadFile(result.Path)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal("export was partially written or damaged", err)
		}
	}
	untouched, err := os.ReadFile(reserved)
	if err != nil || string(untouched) != "existing file" {
		t.Fatal("existing output was overwritten", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != count+1 {
		t.Fatal("temporary export files leaked", len(entries), err)
	}
}

func TestBatchRetainsSuccessWhenAnotherInputFails(t *testing.T) {
	dir := t.TempDir()
	data := fixture(t)
	inputs := []Input{{Name: "first.png", Data: data}, {Name: "broken.png", Data: []byte("broken image")}, {Name: "third.png", Data: data}}
	var started, finished []int
	outcomes := Batch(context.Background(), inputs, dir, DefaultOptions(), func(event Event) {
		if event.Started {
			started = append(started, event.Index)
		}
		if event.Finished {
			finished = append(finished, event.Index)
		}
	})
	if len(outcomes) != 3 || len(started) != 3 || len(finished) != 3 || outcomes[0].Err != nil || outcomes[1].Err == nil || outcomes[2].Err != nil {
		t.Fatal("batch lost per-file outcomes", outcomes, started, finished)
	}
	for _, index := range []int{0, 2} {
		if _, err := os.Stat(outcomes[index].Result.Path); err != nil {
			t.Fatal("successful export was lost", err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatal("failed input left an export behind", len(entries), err)
	}
}

func TestBatchCancellationLeavesFinishedExportsAndSkipsRemainingInputs(t *testing.T) {
	dir := t.TempDir()
	data := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inputs := []Input{{Name: "first.png", Data: data}, {Name: "second.png", Data: data}}
	started, finished := 0, 0
	outcomes := Batch(ctx, inputs, dir, DefaultOptions(), func(event Event) {
		if event.Started {
			started++
		}
		if event.Finished {
			finished++
			cancel()
		}
	})
	if started != 1 || finished != 2 || outcomes[0].Err != nil || !errors.Is(outcomes[1].Err, context.Canceled) {
		t.Fatal("cancellation did not preserve batch state", outcomes, started, finished)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("canceled input was exported", len(entries), err)
	}
}

func TestExportRejectsCanceledOrInvalidOptionsWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	data := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Export(ctx, Input{Name: "photo.png", Data: data}, dir, DefaultOptions()); !errors.Is(err, context.Canceled) {
		t.Fatal("pre-canceled export was processed", err)
	}
	options := DefaultOptions()
	options.Quality = 0
	if _, err := Export(context.Background(), Input{Name: "photo.png", Data: data}, dir, options); err == nil {
		t.Fatal("invalid options were accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("rejected export created files", len(entries), err)
	}
}

func TestDataInputNameCannotEscapeOutputFolder(t *testing.T) {
	dir := t.TempDir()
	result, err := Export(context.Background(), Input{Name: "../../photo.png", Data: fixture(t)}, dir, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(result.Path) != dir || strings.Contains(result.Name, "..") {
		t.Fatal("input name escaped the selected directory", result)
	}
}

func TestAnimatedInputRetainsItsActualFormatAndFrames(t *testing.T) {
	palette := color.Palette{color.White, color.Black}
	frame := image.NewPaletted(image.Rect(0, 0, 8, 4), palette)
	var buffer bytes.Buffer
	if err := gif.EncodeAll(&buffer, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{10, 10}}); err != nil {
		t.Fatal(err)
	}
	options := DefaultOptions()
	options.Format, options.MaxWidth = "webp", 2
	result, err := Export(context.Background(), Input{Name: "animation.gif", Data: buffer.Bytes()}, t.TempDir(), options)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil || len(animation.Image) != 2 || result.Extension != "gif" || result.Width != 8 || result.Height != 4 {
		t.Fatal("animation was flattened or labeled with the requested format", result, err)
	}
}

func TestExportDoesNotFollowDestinationSymlinks(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(t.TempDir(), "private.png")
	if err := os.WriteFile(source, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, filepath.Join(dir, "photo-processed.png")); err != nil {
		t.Fatal(err)
	}
	result, err := Export(context.Background(), Input{Name: "photo.png", Data: fixture(t)}, dir, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(source)
	if err != nil || string(data) != "unchanged" || result.Name == "photo-processed.png" {
		t.Fatal("export followed a destination symlink", result, err)
	}
}
