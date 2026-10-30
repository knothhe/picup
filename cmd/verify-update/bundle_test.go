package main

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"debug/macho"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/knothhe/picup/internal/updatefeed"
)

func bundleFixture(t *testing.T) (string, string, string) {
	t.Helper()
	directory := t.TempDir()
	archivePath := filepath.Join(directory, "Picup-1.2.3-darwin-arm64.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	binaryContent := make([]byte, 32)
	for offset, value := range map[int]uint32{0: macho.Magic64, 4: uint32(macho.CpuArm64), 12: uint32(macho.TypeExec)} {
		binary.LittleEndian.PutUint32(binaryContent[offset:], value)
	}
	for path, content := range map[string][]byte{
		"Picup.app/Contents/Info.plist":  []byte(`<?xml version="1.0"?><plist><dict><key>CFBundleIdentifier</key><string>com.knothhe.picup</string><key>CFBundleExecutable</key><string>Picup</string><key>CFBundleVersion</key><string>1.2.3</string><key>CFBundleShortVersionString</key><string>1.2.3</string></dict></plist>`),
		"Picup.app/Contents/MacOS/Picup": binaryContent, "Picup.app/Contents/Resources/picup": binaryContent,
		"Picup.app/Contents/Resources/THIRD_PARTY_NOTICES.txt": []byte("licenses"),
	} {
		header := &zip.FileHeader{Name: path, Method: zip.Deflate}
		header.SetMode(0755)
		entry, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	file.Close()
	content, _ := os.ReadFile(archivePath)
	digest := sha256.Sum256(content)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	feed := updatefeed.Feed{Schema: 1, Version: "1.2.3", Date: "2026-10-29T00:00:00Z", Platforms: map[string]updatefeed.Asset{
		"darwin-arm64": {URL: "https://github.com/knothhe/picup/releases/download/v1.2.3/" + filepath.Base(archivePath),
			Size: int64(len(content)), SHA256: hex.EncodeToString(digest[:]), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, digest[:]))},
	}}
	encoded, _ := json.Marshal(feed)
	feedPath := filepath.Join(directory, "update-feed.json")
	if err := os.WriteFile(feedPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	return feedPath, archivePath, base64.StdEncoding.EncodeToString(public)
}

func TestUnifiedFeedVerificationAndExpectedReleaseIdentity(t *testing.T) {
	feed, archive, key := bundleFixture(t)
	report, err := verifyBundle(feed, archive, key, "1.2.3", "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if report.Stage != "signature-and-archive-verified" || report.OS != "darwin" {
		t.Fatal(report)
	}
	for _, args := range [][3]string{{"1.2.4", "darwin", "arm64"}, {"1.2.3", "linux", "amd64"}, {"1.2.3", "windows", "amd64"}, {"1.2.3", "darwin", ""}} {
		if _, err := verifyBundle(feed, archive, key, args[0], args[1], args[2]); err == nil {
			t.Fatal("accepted incorrect release target", args)
		}
	}
	data, _ := os.ReadFile(feed)
	os.WriteFile(feed, []byte(strings.ReplaceAll(string(data), "knothhe/picup", "other/repo")), 0600)
	if _, err := verifyBundle(feed, archive, key, "1.2.3", "darwin", "arm64"); err == nil {
		t.Fatal("accepted different repository")
	}
}
