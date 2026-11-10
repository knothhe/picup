package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"debug/macho"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bundleFixture(t *testing.T, extra *tar.Header) (string, string, string) {
	t.Helper()
	directory := t.TempDir()
	archivePath := filepath.Join(directory, "picup-1.2.3-darwin-arm64.tar.gz")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(file)
	archive := tar.NewWriter(compressed)
	binaryContent := make([]byte, 32)
	for offset, value := range map[int]uint32{0: macho.Magic64, 4: uint32(macho.CpuArm64), 12: uint32(macho.TypeExec)} {
		binary.LittleEndian.PutUint32(binaryContent[offset:], value)
	}
	for path, content := range map[string][]byte{
		"Picup.app/Contents/Info.plist":  []byte(`<?xml version="1.0"?><plist><dict><key>CFBundleIdentifier</key><string>com.knothhe.picup</string><key>CFBundleExecutable</key><string>Picup</string><key>CFBundleVersion</key><string>1.2.3</string><key>CFBundleShortVersionString</key><string>1.2.3</string><key>Nested</key><array><dict><key>Nested</key><string>value</string></dict></array></dict></plist>`),
		"Picup.app/Contents/MacOS/Picup": binaryContent, "Picup.app/Contents/Resources/picup": binaryContent,
		"Picup.app/Contents/Resources/THIRD_PARTY_NOTICES.txt": []byte("licenses"),
	} {
		if err := archive.WriteHeader(&tar.Header{Name: path, Size: int64(len(content)), Mode: 0755}); err != nil {
			t.Fatal(err)
		}
		archive.Write(content)
	}
	if extra != nil {
		if err := archive.WriteHeader(extra); err != nil {
			t.Fatal(err)
		}
	}
	archive.Close()
	compressed.Close()
	file.Close()
	bytes, _ := os.ReadFile(archivePath)
	digest := sha256.Sum256(bytes)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	feed := updateFeed{Version: "1.2.3", URL: "https://github.com/knothhe/picup/releases/download/v1.2.3/" + filepath.Base(archivePath), Size: int64(len(bytes)), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, digest[:]))}
	encoded, _ := json.Marshal(feed)
	feedPath := filepath.Join(directory, "update-darwin-arm64.json")
	os.WriteFile(feedPath, encoded, 0600)
	return feedPath, archivePath, base64.StdEncoding.EncodeToString(public)
}

func TestFullArchiveVerificationAndExpectedReleaseIdentity(t *testing.T) {
	feed, archive, key := bundleFixture(t, nil)
	report, err := verifyBundle(feed, archive, key, "1.2.3", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if report.Stage != "signature-and-archive-verified" {
		t.Fatal(report)
	}
	if _, err := verifyBundle(feed, archive, key, "1.2.4", "arm64"); err == nil {
		t.Fatal("accepted wrong release version")
	}
	data, _ := os.ReadFile(feed)
	os.WriteFile(feed, []byte(strings.ReplaceAll(string(data), "knothhe/picup", "other/repo")), 0600)
	if _, err := verifyBundle(feed, archive, key, "1.2.3", "arm64"); err == nil {
		t.Fatal("accepted different repository")
	}
}

func TestSignedArchiveStillRejectsUnsupportedLinksAndPaths(t *testing.T) {
	for _, header := range []tar.Header{
		{Name: "../outside", Typeflag: tar.TypeDir, Mode: 0755},
		{Name: "Picup.app/escape", Typeflag: tar.TypeSymlink, Linkname: "../../outside"},
		// MyGo's update extractor accepts symbolic links but rejects hard links,
		// even when their target is an existing file inside this exact bundle.
		{Name: "Picup.app/hardlink", Typeflag: tar.TypeLink, Linkname: "Picup.app/Contents/Resources/picup", Mode: 0755},
		{Name: "Picup.app/device", Typeflag: tar.TypeChar},
	} {
		t.Run(header.Name, func(t *testing.T) {
			feed, archive, key := bundleFixture(t, &header)
			if _, err := verifyBundle(feed, archive, key, "1.2.3", "arm64"); err == nil {
				t.Fatal("accepted unsafe signed archive")
			}
		})
	}
}
