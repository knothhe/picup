package selection

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestDecodeImagesPreservesFilenamesAndFiltersSelection(t *testing.T) {
	dir := t.TempDir()
	names := []string{"图片 空格.PNG", "quote\"and\nnewline.jpg", "animation.gif", "readme.txt"}
	if runtime.GOOS == "windows" {
		names[1] = "another image.jpg" // Windows disallows quotes and newlines.
	}
	var paths []string
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	folder := filepath.Join(dir, "folder.png")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	input := append(append([]string{}, paths...), paths[0], folder, filepath.Join(dir, "missing.png"), "relative.png", "https://example.com/photo.png")
	data, _ := json.Marshal(input)
	got, err := decodeImages(data)
	if err != nil || !reflect.DeepEqual(got, paths[:3]) {
		t.Fatal("selected images were reordered, corrupted or not filtered", got, err)
	}
}

func TestDecodeImagesRejectsEmptyNonImageAndMalformedSelections(t *testing.T) {
	for _, input := range []string{`[]`, `null`, `["/missing.png"]`, `["https://example.com/a.jpg"]`} {
		if images, err := decodeImages([]byte(input)); !errors.Is(err, ErrNoImages) || len(images) != 0 {
			t.Fatal("empty or invalid selection produced upload input", input, images, err)
		}
	}
	for _, input := range []string{`not json`, `[1]`, `{"path":"/a.png"}`} {
		if _, err := decodeImages([]byte(input)); err == nil {
			t.Fatal("malformed Finder output was accepted", input)
		}
	}
}
