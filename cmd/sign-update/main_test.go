package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestSignFinalArchiveAndRejectWrongOrMalformedKey(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "Picup-1.2.3-linux-amd64.zip")
	content := []byte("final ZIP bytes")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	encode := base64.StdEncoding.EncodeToString
	result, err := signArchive(path, encode(private), encode(public))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	signature, _ := base64.StdEncoding.DecodeString(result.Signature)
	if result.Size != int64(len(content)) || result.SHA256 != hex.EncodeToString(digest[:]) || !ed25519.Verify(public, digest[:], signature) {
		t.Fatal("signature does not bind the final ZIP bytes")
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	for _, key := range []string{"", "invalid", encode(private[:32]), encode(append(private[:32:32], other...))} {
		if _, err := signArchive(path, key, encode(public)); err == nil {
			t.Fatal("accepted an invalid private key")
		}
	}
	if _, err := signArchive(path, encode(private), encode(other)); err == nil {
		t.Fatal("accepted a key outside the pinned trust root")
	}
	link := path + ".link"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := signArchive(link, encode(private), encode(public)); err == nil {
		t.Fatal("accepted a linked archive")
	}
}
