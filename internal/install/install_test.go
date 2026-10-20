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
