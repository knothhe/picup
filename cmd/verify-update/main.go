// Verify a release ZIP against the unified feed and pinned update public key.
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/knothhe/picup"
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
	// Keep the existing trust root: Ed25519 signs the archive's SHA-256 digest.
	if !ed25519.Verify(key, digest.Sum(nil), sig) {
		return errors.New("update archive signature does not match its public key")
	}
	return nil
}

func main() {
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "-") {
		flags := flag.NewFlagSet("verify-update", flag.ExitOnError)
		feed := flags.String("feed", "", "local update feed JSON")
		archive := flags.String("archive", "", "local full update archive")
		version := flags.String("version", "", "explicit expected release version")
		goos := flags.String("os", "", "expected release OS: darwin or linux")
		arch := flags.String("arch", "", "expected release architecture: arm64 or amd64")
		public := flags.String("public-key", picup.UpdateConfig().PublicKey, "trusted Ed25519 public key (defaults to the compiled release key)")
		flags.Parse(os.Args[1:])
		report, err := verifyBundle(*feed, *archive, *public, *version, *goos, *arch)
		if err != nil {
			fmt.Fprintln(os.Stderr, "verify-update:", err)
			os.Exit(1)
		}
		json.NewEncoder(os.Stdout).Encode(report)
		return
	}
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: verify-update ARCHIVE PUBLIC_KEY SIGNATURE")
		os.Exit(2)
	}
	if err := verify(os.Args[1], os.Args[2], os.Args[3]); err != nil {
		fmt.Fprintln(os.Stderr, "verify-update:", err)
		os.Exit(1)
	}
}
