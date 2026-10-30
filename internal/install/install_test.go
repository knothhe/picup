package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallUpdateAndConflict(t *testing.T) {
	src := filepath.Join(t.TempDir(), "companion")
	os.WriteFile(src, []byte("v1"), 0755)
	dir := t.TempDir()
	dst, e := Install(src, dir)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Install(src, dir); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(src, []byte("v2"), 0755)
	if _, e = Install(src, dir); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(dst)
	if string(b) != "v2" {
		t.Fatal("update failed")
	}
	st, _ := os.Stat(dst)
	if st.Mode().Perm() != 0755 {
		t.Fatal("CLI is not executable")
	}
	os.WriteFile(dst, []byte("user binary"), 0755)
	if _, e = Install(src, dir); e == nil {
		t.Fatal("overwrote unmanaged contents")
	}
	b, _ = os.ReadFile(dst)
	if string(b) != "user binary" {
		t.Fatal("conflict changed file")
	}
	if _, e = Install(src, "relative/path"); e == nil {
		t.Fatal("accepted relative install")
	}
}

func TestCompanionSourceKeepsLaunchPathAcrossApplicationReplacement(t *testing.T) {
	previous, previousError := companionExecutable, companionExecutableError
	t.Cleanup(func() { companionExecutable, companionExecutableError = previous, previousError })
	parent := t.TempDir()
	application := filepath.Join(parent, "Picup")
	if err := os.Mkdir(application, 0755); err != nil {
		t.Fatal(err)
	}
	companionExecutable, companionExecutableError = filepath.Join(application, "picup-gui"), nil
	if err := os.WriteFile(filepath.Join(application, Filename()), []byte("old companion"), 0755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(parent, "old")
	if err := os.Rename(application, old); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(old); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(application, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(application, Filename()), []byte("new companion"), 0755); err != nil {
		t.Fatal(err)
	}
	source, err := Source()
	if err != nil || source != filepath.Join(application, Filename()) {
		t.Fatal("prepared update lost the bundled CLI", source, err)
	}
	content, err := os.ReadFile(source)
	if err != nil || string(content) != "new companion" {
		t.Fatal("CLI management did not select the replacement package", err)
	}
}
