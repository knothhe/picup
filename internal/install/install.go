package install

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/knothhe/picup/internal/buildidentity"
	"github.com/knothhe/picup/internal/config"
)

func Filename() string {
	if runtime.GOOS == "windows" {
		return buildidentity.CLIName() + ".exe"
	}
	return buildidentity.CLIName()
}
func DefaultDir() string {
	h, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "Picup", "bin")
	}
	return filepath.Join(h, ".local", "bin")
}

// Keep the launch path before an in-app update replaces the application. On
// Linux, /proc/self/exe can subsequently refer to the deleted old executable.
var companionExecutable, companionExecutableError = launchExecutable()

func launchExecutable() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(executable); err == nil {
		executable = real
	}
	return executable, nil
}

func Source() (string, error) {
	if companionExecutableError != nil {
		return "", companionExecutableError
	}
	exe := companionExecutable
	for _, p := range []string{filepath.Join(filepath.Dir(exe), "..", "Resources", Filename()), filepath.Join(filepath.Dir(exe), "..", "Helpers", Filename()), filepath.Join(filepath.Dir(exe), Filename())} {
		st, err := os.Stat(p)
		if err == nil && st.Mode().IsRegular() {
			return filepath.Clean(p), nil
		}
	}
	return "", fmt.Errorf("CLI companion is missing; build both binaries with make build")
}

type receipt struct {
	Hash             string `json:"sha256"`
	Schema           int    `json:"schema,omitempty"`
	Path             string `json:"path,omitempty"`
	Channel          string `json:"channel,omitempty"`
	UninstallPending bool   `json:"uninstall_pending,omitempty"`
}

func hash(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

func Install(source, dir string) (string, error) {
	return InstallNamed(source, dir, Filename())
}

// InstallNamed lets the formal installer select the Dev companion without
// adopting the installer's own build channel.
func InstallNamed(source, dir, filename string) (string, error) {
	dst, marker, data, unchanged, err := prepareCLIInstall(source, dir, filename)
	if err != nil {
		return "", err
	}
	if !unchanged {
		if err = config.AtomicWrite(dst, data, 0755); err != nil {
			return "", err
		}
	}
	b, _ := json.Marshal(receipt{Hash: hash(data), Schema: 2, Path: dst, Channel: cliChannel(filename)})
	if err = config.AtomicWrite(marker, b, 0600); err != nil {
		return dst, fmt.Errorf("CLI installed but receipt could not be saved: %w", err)
	}
	return dst, nil
}

func prepareCLIInstall(source, dir, filename string) (string, string, []byte, bool, error) {
	if filename != "picup" && filename != "picup-dev" && filename != "picup.exe" && filename != "picup-dev.exe" {
		return "", "", nil, false, fmt.Errorf("unsupported Picup CLI identity: %s", filename)
	}
	dir, err := expandHome(dir)
	if err != nil {
		return "", "", nil, false, err
	}
	if !filepath.IsAbs(dir) {
		return "", "", nil, false, fmt.Errorf("installation directory must be an absolute path")
	}
	dir, err = checkedInstallDirectory(dir)
	if err != nil {
		return "", "", nil, false, err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return "", "", nil, false, err
	}
	if len(data) == 0 {
		return "", "", nil, false, fmt.Errorf("CLI companion is empty")
	}
	dst := filepath.Join(dir, filename)
	markerName := ".picup-cli.json"
	if filename == "picup-dev" || filename == "picup-dev.exe" {
		markerName = ".picup-dev-cli.json"
	}
	marker := filepath.Join(dir, markerName)
	for _, path := range []string{dst, marker} {
		if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
			return "", "", nil, false, fmt.Errorf("refusing to replace non-regular file or symlink: %s", path)
		} else if err != nil && !os.IsNotExist(err) {
			return "", "", nil, false, err
		}
	}
	old, err := os.ReadFile(dst)
	if err == nil {
		b, e := os.ReadFile(marker)
		var r receipt
		if os.IsNotExist(e) && hash(old) == hash(data) {
			return dst, marker, data, true, nil
		}
		if e != nil || json.Unmarshal(b, &r) != nil || !validCLIReceipt(r, dst, filename) {
			return "", "", nil, false, fmt.Errorf("%s already exists and is not managed by Picup; choose another directory", dst)
		}
		if hash(old) == hash(data) {
			return dst, marker, data, true, nil
		}
		if r.Hash != hash(old) {
			return "", "", nil, false, fmt.Errorf("%s changed since installation; choose another directory", dst)
		}
	} else if !os.IsNotExist(err) {
		return "", "", nil, false, err
	} else if b, e := os.ReadFile(marker); e == nil {
		var r receipt
		if json.Unmarshal(b, &r) != nil || !validCLIReceipt(r, dst, filename) {
			return "", "", nil, false, fmt.Errorf("%s has an unknown installation receipt; choose another directory", dst)
		}
	} else if !os.IsNotExist(e) {
		return "", "", nil, false, e
	}
	return dst, marker, data, false, nil
}
func OnPath(dir string) bool {
	dir, err := expandHome(dir)
	if err != nil {
		return false
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		a, _ := filepath.Abs(p)
		if real, err := filepath.EvalSymlinks(a); err == nil {
			a = real
		}
		if filepath.Clean(a) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}
