package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/internal/update"
)

// Injected only into the private, pinned MyGo build copy by the script test.
func TestPicupSignedDMGContract(t *testing.T) {
	private := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	public := private.Public().(ed25519.PublicKey)
	t.Setenv("MYGO_UPDATER_PRIVATE_KEY", base64.StdEncoding.EncodeToString(private))
	noDeltas := 0
	stage := t.TempDir()
	config := &Config{Name: "Picup", Version: "1.2.3", root: stage, Updates: &Updates{
		GitHub: "knothhe/picup", TagPrefix: "v", PublicKey: base64.StdEncoding.EncodeToString(public), Deltas: &noDeltas,
	}}
	name := dmgFileName(config, "arm64")
	if name != "picup-1.2.3-arm64.dmg" {
		t.Fatalf("unexpected DMG name %q", name)
	}
	original := []byte("final signed and stapled DMG fixture")
	image := filepath.Join(stage, name)
	if err := os.WriteFile(image, original, 0600); err != nil {
		t.Fatal(err)
	}
	files, err := writeArchive(config, stage, "darwin-arm64", []string{"Picup.app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "update-darwin-arm64.json" {
		t.Fatalf("unexpected artifacts: %v", files)
	}
	data, err := os.ReadFile(image)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatal("final DMG was rewritten while signing")
	}
	encoded, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var manifest update.Manifest
	if err = json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.URL != "https://github.com/knothhe/picup/releases/download/v1.2.3/"+name || manifest.Size != int64(len(original)) || manifest.Version != "1.2.3" || len(manifest.Deltas) != 0 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	hash := sha256.Sum256(original)
	signature, err := base64.StdEncoding.DecodeString(manifest.Signature)
	if err != nil || !ed25519.Verify(public, hash[:], signature) {
		t.Fatal("manifest does not authenticate the final DMG")
	}
	entries, err := os.ReadDir(stage)
	if err != nil || len(entries) != 2 {
		t.Fatalf("extra update artifacts were generated: %v %v", entries, err)
	}
}
