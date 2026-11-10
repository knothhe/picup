// Package updatedmg verifies Picup's signed macOS DMG releases. One DMG is both
// the manual installer and the auto-update archive: MyGo's patched build signs
// the final DMG bytes, and the patched updater hands the download to Extract.
package updatedmg

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	maxDMGSize      int64 = 1 << 30
	maxManifestSize int64 = 1 << 20
	Repository            = "knothhe/picup"
)

// Manifest is MyGo's update-<target>.json for a full (non-delta) update.
type Manifest struct {
	Version   string            `json:"version"`
	Notes     string            `json:"notes,omitempty"`
	Date      string            `json:"date,omitempty"`
	URL       string            `json:"url"`
	Size      int64             `json:"size"`
	Signature string            `json:"signature"`
	Deltas    []json.RawMessage `json:"deltas,omitempty"`
	Previous  json.RawMessage   `json:"previous,omitempty"`
}

// AssetName is the DMG name MyGo writes for the Picup slug.
func AssetName(version, arch string) string {
	return "picup-" + version + "-" + arch + ".dmg"
}

// AssetURL is the only accepted download location of a release DMG.
func AssetURL(repo, version, arch string) string {
	return "https://github.com/" + repo + "/releases/download/v" + version + "/" + AssetName(version, arch)
}

func validArch(arch string) bool { return arch == "arm64" || arch == "amd64" }

func validVersion(version string) bool {
	if version == "" || len(version) > 64 {
		return false
	}
	for _, c := range version {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '.' || c == '-' || c == '+') {
			return false
		}
	}
	return strings.Count(strings.SplitN(strings.SplitN(version, "+", 2)[0], "-", 2)[0], ".") == 2
}

// ValidateManifest checks the feed before anything is downloaded.
func ValidateManifest(m Manifest, repo, arch string) error {
	if !validVersion(m.Version) {
		return errors.New("update: invalid manifest version")
	}
	if !validArch(arch) {
		return errors.New("update: unsupported architecture")
	}
	if m.URL != AssetURL(repo, m.Version, arch) {
		return errors.New("update: manifest does not point to this release's DMG")
	}
	if m.Size <= 0 || m.Size > maxDMGSize {
		return errors.New("update: invalid DMG size")
	}
	if len(m.Deltas) != 0 || (len(m.Previous) != 0 && string(m.Previous) != "null") {
		return errors.New("update: delta updates are not supported")
	}
	if sig, err := base64.StdEncoding.DecodeString(m.Signature); err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("update: invalid signature")
	}
	return nil
}

// VerifySignature checks MyGo's Ed25519 signature over the SHA-256 of the exact
// DMG bytes, before any OS image parser sees them.
func VerifySignature(file io.ReadSeeker, size int64, signature, public string) error {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(public))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return errors.New("update: invalid public key")
	}
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("update: invalid signature")
	}
	end, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if end != size || size <= 0 || size > maxDMGSize {
		return fmt.Errorf("update: DMG is %d bytes, manifest says %d", end, size)
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	digest := sha256.New()
	if n, err := io.Copy(digest, io.LimitReader(file, size+1)); err != nil {
		return err
	} else if n != size {
		return errors.New("update: DMG changed while it was verified")
	}
	if !ed25519.Verify(key, digest.Sum(nil), sig) {
		return errors.New("update: DMG signature does not match the release key")
	}
	return nil
}
