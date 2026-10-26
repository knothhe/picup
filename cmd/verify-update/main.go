// Verify the pinned MyGo update format without depending on platform crypto CLIs.
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

func verify(path, public, signature string) error {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(public))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return errors.New("invalid update public key")
	}
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("invalid update signature")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, f); err != nil {
		return err
	}
	// MyGo v0.5.5 signs the archive's SHA-256 digest with Ed25519.
	if !ed25519.Verify(key, digest.Sum(nil), sig) {
		return errors.New("update archive signature does not match its public key")
	}
	return nil
}

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: verify-update ARCHIVE PUBLIC_KEY SIGNATURE")
		os.Exit(2)
	}
	if err := verify(os.Args[1], os.Args[2], os.Args[3]); err != nil {
		fmt.Fprintln(os.Stderr, "verify-update:", err)
		os.Exit(1)
	}
}
