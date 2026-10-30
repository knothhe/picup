package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxReleaseLoginKeepsLaunchPathAcrossApplicationReplacement(t *testing.T) {
	previous, previousError := companionExecutable, companionExecutableError
	t.Cleanup(func() { companionExecutable, companionExecutableError = previous, previousError })
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	application := filepath.Join(root, "Picup release")
	if err := os.Mkdir(application, 0755); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(application, "picup-gui")
	if err := os.WriteFile(executable, []byte("old GUI"), 0755); err != nil {
		t.Fatal(err)
	}
	companionExecutable, companionExecutableError = executable, nil
	old := filepath.Join(root, "old")
	if err := os.Rename(application, old); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(old); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(application, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("new GUI"), 0755); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "autostart")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(directory, "other.desktop")
	if err := os.WriteFile(unrelated, []byte("keep unrelated registration"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SetLinuxReleaseOpenAtLogin(true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "com.knothhe.picup.desktop")
	content, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(content), "Exec=\""+executable+"\" --mygo-opened-at-login\n") {
		t.Fatalf("autostart lost the stable launch path: %q, %v", content, err)
	}
	for range 2 {
		if err := SetLinuxReleaseOpenAtLogin(false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("autostart registration remains", err)
	}
	if content, err := os.ReadFile(unrelated); err != nil || string(content) != "keep unrelated registration" {
		t.Fatal("unrelated autostart registration changed", err)
	}
}

func TestLinuxReleaseLoginRejectsInvalidLaunchExecutable(t *testing.T) {
	previous, previousError := companionExecutable, companionExecutableError
	t.Cleanup(func() { companionExecutable, companionExecutableError = previous, previousError })
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	nonExecutable := filepath.Join(root, "picup-gui")
	if err := os.WriteFile(nonExecutable, []byte("GUI"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, executable string
		err              error
	}{
		{"unavailable", nonExecutable, errors.New("cannot locate executable")},
		{"relative", "picup-gui", nil},
		{"wrong name", filepath.Join(root, "picup"), nil},
		{"missing", filepath.Join(root, "missing", "picup-gui"), nil},
		{"non-executable", nonExecutable, nil},
		{"control character", filepath.Join(root, "bad\npath", "picup-gui"), nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			companionExecutable, companionExecutableError = test.executable, test.err
			if err := SetLinuxReleaseOpenAtLogin(true); err == nil {
				t.Fatal("invalid executable enabled autostart")
			}
			path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "autostart", "com.knothhe.picup.desktop")
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid executable wrote an autostart registration", err)
			}
		})
	}
}
