// sign-update signs a final release ZIP without exposing the key to build tools.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/knothhe/picup"
)

type signedArchive struct {
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
}

func signArchive(path, secret, trusted string) (signedArchive, error) {
	var result signedArchive
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(secret))
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return result, errors.New("invalid update signing key")
	}
	private := ed25519.NewKeyFromSeed(key[:ed25519.SeedSize])
	public, err := base64.StdEncoding.DecodeString(strings.TrimSpace(trusted))
	if err != nil || len(public) != ed25519.PublicKeySize ||
		!bytes.Equal(private, key) || !bytes.Equal(private.Public().(ed25519.PublicKey), public) {
		return result, errors.New("update signing key does not match the pinned public key")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1<<30 {
		return result, errors.New("update ZIP must be a nonempty regular file no larger than one GiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer file.Close()
	digest := sha256.New()
	count, err := io.Copy(digest, io.LimitReader(file, 1<<30+1))
	if err != nil {
		return result, err
	}
	if count != info.Size() {
		return result, errors.New("update ZIP changed while signing")
	}
	sum := digest.Sum(nil)
	result = signedArchive{count, hex.EncodeToString(sum), base64.StdEncoding.EncodeToString(ed25519.Sign(private, sum))}
	return result, nil
}

func main() {
	archive := flag.String("archive", "", "final release ZIP to sign")
	flag.Parse()
	result, err := signArchive(*archive, os.Getenv("MYGO_UPDATER_PRIVATE_KEY"), picup.UpdateConfig().PublicKey)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sign-update:", err)
		os.Exit(1)
	}
	json.NewEncoder(os.Stdout).Encode(result)
}
