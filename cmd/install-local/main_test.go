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
	if err := checkDestination(dst, "com.knothhe.picup"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dst, "Contents"), 0755); err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(dst, "Contents", "Info.plist")
	for _, id := range []string{"other.app", "com.knothhe.picup"} {
		if err := os.WriteFile(plist, []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>`+id+`</string></dict></plist>`), 0644); err != nil {
			t.Fatal(err)
		}
		err := checkDestination(dst, "com.knothhe.picup")
		if (err == nil) != (id == "com.knothhe.picup") {
			t.Fatalf("identifier %s: %v", id, err)
		}
	}
	link := filepath.Join(dir, "link.app")
	if err := os.Symlink(dst, link); err != nil {
		t.Fatal(err)
	}
	if err := checkDestination(link, "com.knothhe.picup"); err == nil {
		t.Fatal("accepted a symlink destination")
	}
}

func TestLegacyBundleUpgrade(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS application bundles")
	}
	dst := filepath.Join(t.TempDir(), "Picup.app")
	if err := os.MkdirAll(filepath.Join(dst, "Contents"), 0755); err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(dst, "Contents", "Info.plist")
	if err := os.WriteFile(plist, []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>me.knothhe.picup</string></dict></plist>`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkDestination(dst, "com.knothhe.picup"); err != nil {
		t.Fatalf("cannot upgrade legacy Picup: %v", err)
	}
	if err := checkBundle(dst, "com.knothhe.picup"); err == nil {
		t.Fatal("accepted legacy identifier for a newly built bundle")
	}
}
