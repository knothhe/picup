// Package selection reads image files explicitly selected in the macOS Finder.
package selection

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrPermission = errors.New("Finder automation permission denied")
	ErrNoImages   = errors.New("no image files selected in Finder")
)

// JSON preserves spaces, Unicode, quotes and newlines in filenames. Only local,
// regular image files are accepted; folders are never traversed.
func decodeImages(data []byte) ([]string, error) {
	var paths []string
	if err := json.Unmarshal(data, &paths); err != nil {
		return nil, err
	}
	var images []string
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			continue
		}
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".heic", ".heif", ".avif", ".tif", ".tiff":
		default:
			continue
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			continue
		}
		seen[path] = true
		images = append(images, path)
	}
	if len(images) == 0 {
		return nil, ErrNoImages
	}
	return images, nil
}
