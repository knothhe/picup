package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestMyGoArchiveSignatureRejectsTamperingAndWrongKey(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "picup.tar.gz")
	content := []byte("signed archive fixture")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	key := base64.StdEncoding.EncodeToString(public)
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(private, digest[:]))
	if err := verify(path, key, signature); err != nil {
		t.Fatal(err)
	}
	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := verify(path, base64.StdEncoding.EncodeToString(other), signature); err == nil {
		t.Fatal("accepted a signature from another key")
	}
	if err := os.WriteFile(path, []byte("tampered archive"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verify(path, key, signature); err == nil {
		t.Fatal("accepted a modified archive")
	}
}
