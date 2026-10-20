package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDestinationProtection(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS application bundles")
	}
	dir := t.TempDir()
	dst := filepath.Join(dir, "Picup.app")
	if err := checkDestination(dst, "me.knothhe.picup"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dst, "Contents"), 0755); err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(dst, "Contents", "Info.plist")
	for _, id := range []string{"other.app", "me.knothhe.picup"} {
		if err := os.WriteFile(plist, []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>`+id+`</string></dict></plist>`), 0644); err != nil {
			t.Fatal(err)
		}
		err := checkDestination(dst, "me.knothhe.picup")
		if (err == nil) != (id == "me.knothhe.picup") {
			t.Fatalf("identifier %s: %v", id, err)
		}
	}
	link := filepath.Join(dir, "link.app")
	if err := os.Symlink(dst, link); err != nil {
		t.Fatal(err)
	}
	if err := checkDestination(link, "me.knothhe.picup"); err == nil {
		t.Fatal("accepted a symlink destination")
	}
}
