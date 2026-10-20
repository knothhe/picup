package install

import (
	"os"
	"path/filepath"
	"strings"
)

// DisplayPath abbreviates only the current user's home, keeping other paths
// intact (including sibling directories with a similar prefix).
func DisplayPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(path) {
		return path
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	if rel == "." {
		return "~"
	}
	return filepath.Join("~", rel)
}

func expandHome(path string) (string, error) {
	if path != "~" && !(len(path) > 1 && path[0] == '~' && os.IsPathSeparator(path[1])) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}
