package updatedmg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/netproxy"
)

// Source is the MyGo UpdateSource of release builds. It serves the stable
// GitHub feed of this architecture and implements the Extract hook that the
// patched MyGo updater calls with the downloaded DMG.
type Source struct {
	repo, public, arch string
	client             *http.Client
	mu                 sync.Mutex
	manifests          map[string]Manifest // by DMG URL
}

// NewSource uses MyGo's proxy-aware transport, HTTPS-only redirects, when
// client is nil.
func NewSource(repo, public string, client *http.Client) *Source {
	if client == nil {
		client = &http.Client{Transport: netproxy.Transport(), Timeout: 15 * time.Minute,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if req.URL.Scheme != "https" {
					return errors.New("update: redirect is not HTTPS")
				}
				if len(via) >= 10 {
					return errors.New("update: too many redirects")
				}
				return nil
			}}
	}
	return &Source{repo: repo, public: public, arch: runtime.GOARCH, client: client, manifests: map[string]Manifest{}}
}

func (s *Source) get(ctx context.Context, url string) (io.ReadCloser, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, errors.New("update: download is not HTTPS")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "picup-updater")
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("update: server returned HTTP %d", res.StatusCode)
	}
	return res.Body, nil
}

// Manifest reads and validates the latest stable release feed.
func (s *Source) Manifest(ctx context.Context, prereleases bool) (io.ReadCloser, error) {
	body, err := s.get(ctx, "https://github.com/"+s.repo+"/releases/latest/download/update-darwin-"+s.arch+".json")
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxManifestSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxManifestSize {
		return nil, errors.New("update: manifest exceeds one MiB")
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("update: invalid manifest: %w", err)
	}
	if err := ValidateManifest(m, s.repo, s.arch); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if len(s.manifests) >= 32 {
		clear(s.manifests)
	}
	s.manifests[m.URL] = m
	s.mu.Unlock()
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *Source) manifest(url string) (Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.manifests[url]
	if !ok {
		return m, errors.New("update: unknown or expired update; check again")
	}
	return m, nil
}

// Open downloads a DMG that a validated manifest named.
func (s *Source) Open(ctx context.Context, url string) (io.ReadCloser, error) {
	if _, err := s.manifest(url); err != nil {
		return nil, err
	}
	return s.get(ctx, url)
}

// Extract is called by the patched MyGo updater after it verified the
// download's signature. It verifies again and stages Picup.app in destination.
func (s *Source) Extract(ctx context.Context, url, image, destination string) error {
	m, err := s.manifest(url)
	if err != nil {
		return err
	}
	return Inspect(ctx, image, m, s.public, s.arch, destination)
}
