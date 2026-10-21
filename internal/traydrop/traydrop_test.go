package traydrop

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestImageFilesAcceptsPickerFormatsAndRejectsDirectories(t *testing.T) {
	dir := t.TempDir()
	var paths, want []string
	for _, name := range []string{"照片 空格.PNG", "image.JpEg", "image.webp", "image.gif", "image.HEIC", "image.heif", "image.avif", "image.tif", "image.tiff", "notes.txt", "image.svg"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("the upload pipeline decodes this"), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
		if name != "notes.txt" && name != "image.svg" {
			want = append(want, path)
		}
	}
	folder := filepath.Join(dir, "folder.png")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	paths = append(paths, folder, filepath.Join(dir, "missing.png"), paths[0])
	if got := imageFiles(paths); !slices.Equal(got, want) {
		t.Fatalf("images = %q, want %q", got, want)
	}
}
