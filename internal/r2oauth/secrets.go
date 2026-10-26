package r2oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/knothhe/picup/internal/config"
)

var ErrSecretDenied = errors.New("system credential access was denied; allow access and start a new Cloudflare connection to retry")
var ErrSecretUnavailable = errors.New("system credential storage is unavailable on this build")
var errSecretMissing = errors.New("credential not found")

// Channel is the same compile-time identity used by default configuration paths.
const Channel = config.BuildChannel

type secretStore interface {
	Get(string) ([]byte, error)
	Set(string, []byte) error
	Delete(string) error
}

type secretCoordinator struct {
	store  secretStore
	denied bool
}

func (s *secretCoordinator) check(err error) error {
	if errors.Is(err, ErrSecretDenied) {
		s.denied = true
	}
	return err
}
func (s *secretCoordinator) get(key string) ([]byte, error) {
	if s.denied {
		return nil, ErrSecretDenied
	}
	b, e := s.store.Get(key)
	return b, s.check(e)
}
func (s *secretCoordinator) set(key string, b []byte) error {
	if s.denied {
		return ErrSecretDenied
	}
	return s.check(s.store.Set(key, b))
}
func (s *secretCoordinator) del(key string) error {
	if s.denied {
		return ErrSecretDenied
	}
	return s.check(s.store.Delete(key))
}

// Resolve the nearest existing ancestor as well as existing complete paths, so
// first-run directories below a symlink keep their credential identity later.
func canonicalConfigPath(path string) string {
	if p, e := filepath.Abs(path); e == nil {
		path = filepath.Clean(p)
	}
	dir := filepath.Dir(path)
	var suffix []string
	for {
		if resolved, e := filepath.EvalSymlinks(dir); e == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Join(resolved, filepath.Base(path))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}
		suffix = append(suffix, filepath.Base(dir))
		dir = parent
	}
}
func namespace(path string) string { return namespaceForChannel(path, Channel) }
func namespaceForChannel(path, channel string) string {
	hash := sha256.Sum256([]byte(filepath.Dir(canonicalConfigPath(path))))
	return "com.knothhe.picup.r2oauth." + channel + "." + hex.EncodeToString(hash[:16])
}
func randomID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return hex.EncodeToString(b[:]), nil
}

type blobManifest struct {
	Parts  int    `json:"parts"`
	SHA256 string `json:"sha256"`
}

const blobPartSize = 2000
const maxBlobSize = 256 * 1024

// Each key is immutable and unique. Publish the manifest only after all chunks
// exist; metadata never points at a partially written new credential generation.
func (s *secretCoordinator) writeBlob(value any) (string, error) {
	b, e := json.Marshal(value)
	if e != nil {
		return "", e
	}
	if len(b) > maxBlobSize {
		return "", errors.New("credential exceeds system storage limit")
	}
	id, e := randomID()
	if e != nil {
		return "", e
	}
	parts := (len(b) + blobPartSize - 1) / blobPartSize
	for i := 0; i < parts; i++ {
		end := min(len(b), (i+1)*blobPartSize)
		if e = s.set(id+"."+strconv.Itoa(i), b[i*blobPartSize:end]); e != nil {
			for j := 0; j < i; j++ {
				_ = s.del(id + "." + strconv.Itoa(j))
			}
			return "", e
		}
	}
	digest := sha256.Sum256(b)
	m, _ := json.Marshal(blobManifest{parts, hex.EncodeToString(digest[:])})
	if e = s.set(id, m); e != nil {
		for i := 0; i < parts; i++ {
			_ = s.del(id + "." + strconv.Itoa(i))
		}
		return "", e
	}
	return id, nil
}
func (s *secretCoordinator) readBlob(id string, value any) error {
	if id == "" {
		return errSecretMissing
	}
	b, e := s.get(id)
	if e != nil {
		return e
	}
	var m blobManifest
	if json.Unmarshal(b, &m) != nil || m.Parts < 1 || m.Parts > maxBlobSize/blobPartSize+1 {
		return errors.New("invalid stored credential manifest")
	}
	var all []byte
	for i := 0; i < m.Parts; i++ {
		p, e := s.get(id + "." + strconv.Itoa(i))
		if e != nil {
			return e
		}
		all = append(all, p...)
		if len(all) > maxBlobSize {
			return errors.New("stored credential is too large")
		}
	}
	digest := sha256.Sum256(all)
	if hex.EncodeToString(digest[:]) != m.SHA256 {
		return errors.New("stored credential is incomplete")
	}
	if e = json.Unmarshal(all, value); e != nil {
		return errors.New("invalid stored credential")
	}
	return nil
}
func (s *secretCoordinator) deleteBlob(id string) error {
	b, e := s.get(id)
	if errors.Is(e, errSecretMissing) {
		return nil
	}
	if e != nil {
		return e
	}
	var m blobManifest
	if json.Unmarshal(b, &m) != nil || m.Parts < 1 || m.Parts > maxBlobSize/blobPartSize+1 {
		return errors.New("invalid stored credential manifest")
	}
	for i := 0; i < m.Parts; i++ {
		if e = s.del(id + "." + strconv.Itoa(i)); e != nil && !errors.Is(e, errSecretMissing) {
			return fmt.Errorf("remove credential part: %w", e)
		}
	}
	return s.del(id)
}
