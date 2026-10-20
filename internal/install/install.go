package install

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/knothhe/picup/internal/config"
)

func Filename() string {
	if runtime.GOOS == "windows" {
		return "picup.exe"
	}
	return "picup"
}
func DefaultDir() string {
	h, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "Picup", "bin")
	}
	return filepath.Join(h, ".local", "bin")
}
func Source() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	for _, p := range []string{filepath.Join(filepath.Dir(exe), "..", "Resources", Filename()), filepath.Join(filepath.Dir(exe), "..", "Helpers", Filename()), filepath.Join(filepath.Dir(exe), Filename())} {
		st, err := os.Stat(p)
		if err == nil && st.Mode().IsRegular() {
			return filepath.Clean(p), nil
		}
	}
	return "", fmt.Errorf("CLI companion is missing; build both binaries with make build")
}

type receipt struct {
	Hash string `json:"sha256"`
}

func hash(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

func Install(source, dir string) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("installation directory must be an absolute path")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", fmt.Errorf("CLI companion is empty")
	}
	dst := filepath.Join(dir, Filename())
	marker := filepath.Join(dir, ".picup-cli.json")
	old, err := os.ReadFile(dst)
	if err == nil {
		if hash(old) == hash(data) {
			return dst, nil
		}
		b, e := os.ReadFile(marker)
		var r receipt
		if e != nil || json.Unmarshal(b, &r) != nil || r.Hash != hash(old) {
			return "", fmt.Errorf("%s already exists and is not managed by Picup; choose another directory", dst)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err = config.AtomicWrite(dst, data, 0755); err != nil {
		return "", err
	}
	b, _ := json.Marshal(receipt{hash(data)})
	if err = config.AtomicWrite(marker, b, 0600); err != nil {
		return dst, fmt.Errorf("CLI installed but receipt could not be saved: %w", err)
	}
	return dst, nil
}
func OnPath(dir string) bool {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		a, _ := filepath.Abs(p)
		if filepath.Clean(a) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}
