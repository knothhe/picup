package r2oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sync"

	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/r2connect/keyring"
)

var ErrSecretDenied = keyring.ErrAccessDenied
var ErrSecretUnavailable = keyring.ErrUnavailable
var errSecretMissing = keyring.ErrNotFound

const Channel = config.BuildChannel

var systemKeyrings sync.Map

func systemKeyring(service, label string) *keyring.Store {
	created := keyring.New(service, label)
	actual, _ := systemKeyrings.LoadOrStore(service, created)
	return actual.(*keyring.Store)
}

func newSDKSecrets(path string) *keyring.Store {
	// Manager owns and caches the actual instance. A fresh store lets New bind
	// its denial callback before LoadOrStore without mutating a shared callback.
	return keyring.New(namespace(path)+".sdk", "Picup Cloudflare credentials")
}

func legacySecrets(path string) *keyring.Store {
	return systemKeyring(namespace(path), "Picup legacy Cloudflare credentials")
}

// RetryLegacyAccess is only for an explicit user disconnect/recovery action.
// Status, listing, a new unrelated authorization, and Resolve never call it.
func RetryLegacyAccess(path string) { legacySecrets(path).RetryAccess() }

// Resolve the nearest existing ancestor so first-run aliases retain identity.
func canonicalConfigPath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		path = filepath.Clean(absolute)
	}
	directory := filepath.Dir(path)
	var suffix []string
	for {
		if real, err := filepath.EvalSymlinks(directory); err == nil {
			for index := len(suffix) - 1; index >= 0; index-- {
				real = filepath.Join(real, suffix[index])
			}
			return filepath.Join(real, filepath.Base(path))
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return path
		}
		suffix = append(suffix, filepath.Base(directory))
		directory = parent
	}
}

func namespace(path string) string { return namespaceForChannel(path, Channel) }
func namespaceForChannel(path, channel string) string {
	digest := sha256.Sum256([]byte(filepath.Dir(canonicalConfigPath(path))))
	return "com.knothhe.picup.r2oauth." + channel + "." + hex.EncodeToString(digest[:16])
}
func randomID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(random[:]), nil
}
