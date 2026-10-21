// Package traydrop adds file drops to a MyGo menu bar icon on macOS.
package traydrop

import (
	"os"
	"path/filepath"
	"strings"
)

// imageFiles uses the same input formats as the image picker. The upload
// pipeline still validates and decodes each image before sending any data.
func imageFiles(paths []string) []string {
	var images []string
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".heic", ".heif", ".avif", ".tif", ".tiff":
		default:
			continue
		}
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			continue
		}
		seen[path] = true
		images = append(images, path)
	}
	return images
}
