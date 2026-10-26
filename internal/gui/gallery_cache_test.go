package gui

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/storage"
)

func testGalleryCache(t *testing.T, root string, cfg config.Config) *galleryDiskCache {
	t.Helper()
	cache := newGalleryDiskCache(cfg)
	if cache == nil {
		t.Fatal("cache directory unavailable")
	}
	cache.root = root
	cache.dir = filepath.Join(root, filepath.Base(cache.dir))
	return cache
}

func TestGalleryCacheDirectoryRespectsBuildChannelAndOverride(t *testing.T) {
	t.Setenv("PICUP_CACHE_DIR", "")
	systemCache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	defaultPath, err := galleryCacheDirectory()
	if err != nil || defaultPath != filepath.Join(systemCache, config.DirectoryName, "gallery-v1") {
		t.Fatal("gallery cache did not follow the compiled channel", defaultPath, err)
	}
	if config.BuildChannel == "development" && filepath.Base(filepath.Dir(defaultPath)) != "picup-dev" {
		t.Fatal("development cache shares the release directory")
	}
	override := t.TempDir()
	t.Setenv("PICUP_CACHE_DIR", override)
	path, err := galleryCacheDirectory()
	if err != nil || path != filepath.Join(override, "gallery-v1") {
		t.Fatal("isolated cache root was not honored", path, err)
	}
	t.Setenv("PICUP_CACHE_DIR", "relative/cache")
	if _, err = galleryCacheDirectory(); err == nil {
		t.Fatal("relative override could clear an unintended working directory")
	}
}

func cachedGalleryObject() storage.Object {
	return storage.Object{Key: "images/海边.png", URL: "https://images.example.com/images/photo.png", Size: 1234, Modified: time.Date(2026, 10, 20, 4, 30, 0, 100, time.UTC), ETag: `"version-one"`}
}

func TestGalleryCacheRestoresMetadataWithoutCredentials(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Endpoint, cfg.Bucket = "https://endpoint.example.com", "photos"
	cfg.AccessKey, cfg.SecretKey, cfg.SessionToken = "private-access-key", "private-secret-key", "private-session-token"
	cache := testGalleryCache(t, root, cfg)
	object := cachedGalleryObject()
	// Even a configured public URL containing credentials is never cached.
	object.URL = "https://private-access-key:private-secret-key@example.com/photo.png"
	if err := cache.writeList([]storage.Object{object}); err != nil {
		t.Fatal(err)
	}
	restarted := testGalleryCache(t, root, cfg)
	got, err := restarted.readList()
	object.URL = ""
	if err != nil || !reflect.DeepEqual(got, []storage.Object{object}) {
		t.Fatal("restart lost metadata", got, err)
	}
	data, err := os.ReadFile(filepath.Join(cache.dir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{cfg.AccessKey, cfg.SecretKey, cfg.SessionToken, cfg.Endpoint, cfg.Bucket} {
		if strings.Contains(string(data), secret) || strings.Contains(cache.dir, secret) {
			t.Fatalf("cache leaked storage identity %q", secret)
		}
	}
	info, err := os.Stat(filepath.Join(cache.dir, "index.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("metadata permissions", info, err)
	}
}

func TestGalleryCacheSeparatesCompleteStorageIdentity(t *testing.T) {
	base := config.Default()
	base.Endpoint, base.Bucket, base.AccessKey, base.SecretKey = "https://s3.example.com", "photos", "access", "secret"
	root := t.TempDir()
	cache := testGalleryCache(t, root, base)
	if err := cache.writeList([]storage.Object{cachedGalleryObject()}); err != nil {
		t.Fatal(err)
	}
	changes := map[string]func(*config.Config){
		"provider":   func(c *config.Config) { c.Provider = "s3" },
		"endpoint":   func(c *config.Config) { c.Endpoint += "/other" },
		"region":     func(c *config.Config) { c.Region = "us-east-1" },
		"bucket":     func(c *config.Config) { c.Bucket += "-other" },
		"access":     func(c *config.Config) { c.AccessKey += "-other" },
		"secret":     func(c *config.Config) { c.SecretKey += "-other" },
		"token":      func(c *config.Config) { c.SessionToken = "new-session" },
		"oauth":      func(c *config.Config) { c.R2Connection = "another-connection" },
		"credential": func(c *config.Config) { c.CredentialRef = "another-system-credential" },
		"public-url": func(c *config.Config) { c.PublicURL = "https://cdn.example.com" },
		"path-style": func(c *config.Config) { c.PathStyle = !c.PathStyle },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			other := base
			change(&other)
			isolated := testGalleryCache(t, root, other)
			if cache.dir == isolated.dir {
				t.Fatal("storage identities collided")
			}
			if _, err := isolated.readList(); !os.IsNotExist(err) {
				t.Fatal("read another storage's cache", err)
			}
		})
	}
	base.Quality, base.Appearance, base.PathTemplate = 70, "dark", "new/{fileName}"
	if same := testGalleryCache(t, root, base); same.dir != cache.dir {
		t.Fatal("upload or appearance preferences changed gallery identity")
	}
}

func TestGalleryThumbnailCacheAvoidsDownloadsAndInvalidatesRevisions(t *testing.T) {
	var data bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 24, 16))
	img.Set(1, 1, color.NRGBA{R: 255, A: 100})
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	client := guiCloudClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(data.Bytes())
	})
	root := t.TempDir()
	cache := testGalleryCache(t, root, config.Default())
	object := cachedGalleryObject()
	if err := cache.writeList([]storage.Object{object}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		thumb := readGalleryCachedThumb(context.Background(), client, object, cache)
		if thumb.err != nil || thumb.bitmap == nil || thumb.width != 24 || thumb.height != 16 {
			t.Fatal("thumbnail dimensions or pixels lost", thumb.width, thumb.height, thumb.err)
		}
		cache = testGalleryCache(t, root, config.Default())
	}
	if requests.Load() != 1 {
		t.Fatal("restart downloaded cached thumbnail again", requests.Load())
	}
	changes := []func(*storage.Object){
		func(o *storage.Object) { o.ETag = `"version-two"` },
		func(o *storage.Object) { o.Size++ },
		func(o *storage.Object) { o.Modified = o.Modified.Add(time.Second) },
		func(o *storage.Object) { o.Key = "renamed.png" },
	}
	for i, change := range changes {
		old := object
		change(&object)
		if err := cache.writeList([]storage.Object{object}); err != nil {
			t.Fatal(err)
		}
		if _, hit := cache.readThumb(old); hit {
			t.Fatal("obsolete thumbnail retained")
		}
		thumb := readGalleryCachedThumb(context.Background(), client, object, cache)
		if thumb.err != nil || requests.Load() != int32(i+2) {
			t.Fatal("changed object reused obsolete thumbnail", i, requests.Load(), thumb.err)
		}
		// A download started before refresh must not restore stale disk data.
		if err := cache.writeThumb(old, img, 24, 16); err != nil {
			t.Fatal(err)
		}
		if _, hit := cache.readThumb(old); hit {
			t.Fatal("late download resurrected stale thumbnail")
		}
	}
	if err := cache.writeList(nil); err != nil {
		t.Fatal(err)
	}
	if err := cache.writeThumb(object, img, 24, 16); err != nil {
		t.Fatal(err)
	}
	if _, hit := cache.readThumb(object); hit {
		t.Fatal("deleted object reappeared in thumbnail cache")
	}
}

func TestGalleryCacheCorruptionAndFailedRefreshRetainList(t *testing.T) {
	cache := testGalleryCache(t, t.TempDir(), config.Default())
	object := cachedGalleryObject()
	if err := cache.writeList([]storage.Object{object}); err != nil {
		t.Fatal(err)
	}
	client := guiCloudClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) })
	before, err := cache.readList()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.List(context.Background(), ""); err == nil {
		t.Fatal("refresh unexpectedly succeeded")
	}
	after, err := cache.readList()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("failed refresh destroyed offline list", after, err)
	}
	path := filepath.Join(cache.dir, galleryObjectDigest(object)+".thumb")
	for _, data := range [][]byte{[]byte("interrupted cache write"), []byte(`{"version":2}`), []byte(`{"version":1,"Width":1000,"Height":10,"png":"aW52YWxpZA=="}`)} {
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, ok := cache.readThumb(object); ok {
			t.Fatal("corrupt thumbnail accepted")
		}
	}
	if err = os.WriteFile(filepath.Join(cache.dir, "index.json"), []byte(`{"version":99,"objects":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = cache.readList(); err == nil {
		t.Fatal("unsupported cache version accepted")
	}
}

func TestGalleryCacheEvictionPreservesListsAndUsesLeastRecentThumbnail(t *testing.T) {
	cache := testGalleryCache(t, t.TempDir(), config.Default())
	cache.limit = 90
	if err := os.MkdirAll(cache.dir, 0700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	files := []string{"index.json", "old.thumb", "new.thumb"}
	for i, name := range files {
		path := filepath.Join(cache.dir, name)
		if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, 40), 0600); err != nil {
			t.Fatal(err)
		}
		used := now.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(path, used, used); err != nil {
			t.Fatal(err)
		}
	}
	galleryCacheLock.Lock()
	err := cache.pruneLocked()
	galleryCacheLock.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(cache.dir, "old.thumb")); !os.IsNotExist(err) {
		t.Fatal("least recent thumbnail not evicted", err)
	}
	for _, name := range []string{"index.json", "new.thumb"} {
		if _, err = os.Stat(filepath.Join(cache.dir, name)); err != nil {
			t.Fatal("evicted metadata or recent thumbnail", name, err)
		}
	}
	_, total, err := galleryCacheFiles(cache.root)
	if err != nil || total > cache.limit {
		t.Fatal("cache remains over limit", total, err)
	}
}

func TestGalleryCacheRejectsLateWritesAfterClear(t *testing.T) {
	cache := testGalleryCache(t, t.TempDir(), config.Default())
	object := cachedGalleryObject()
	if err := cache.writeList([]storage.Object{object}); err != nil {
		t.Fatal(err)
	}
	// Mirror clear's epoch change without touching the real user cache root.
	galleryCacheLock.Lock()
	galleryCacheEpoch.Add(1)
	err := os.RemoveAll(cache.root)
	galleryCacheLock.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = cache.writeList([]storage.Object{object}); err != nil {
		t.Fatal(err)
	}
	if err = cache.writeThumb(object, image.NewRGBA(image.Rect(0, 0, 4, 4)), 4, 4); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(cache.root); !os.IsNotExist(err) {
		t.Fatal("old worker recreated cleared cache", err)
	}
	if _, err = cache.readList(); !os.IsNotExist(err) {
		t.Fatal("old cache handle survived clear", err)
	}
	// A new storage handle can cache future navigation normally.
	cache = testGalleryCache(t, cache.root, config.Default())
	if err = cache.writeList(nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(cache.dir, "index.json"))
	var index galleryCachedIndex
	if err != nil || json.Unmarshal(data, &index) != nil || index.Version != 1 {
		t.Fatal("cache did not recover after clear", err)
	}
}

func TestGalleryRefreshKeepsUnchangedThumbnailsAndInvalidatesChangedPreview(t *testing.T) {
	object := cachedGalleryObject()
	bitmap := ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 4, 4)))
	thumb := galleryThumb{bitmap: bitmap, width: 4, height: 4}
	g := galleryState{objects: []storage.Object{object}, thumbs: map[string]galleryThumb{object.Key: thumb}, preview: &object, previewOpen: true, fullKey: object.Key, full: thumb}
	g.replace([]storage.Object{object})
	if g.thumbs[object.Key].bitmap != bitmap || g.full.bitmap != bitmap {
		t.Fatal("unchanged refresh discarded visible images")
	}
	ctx, cancel := context.WithCancel(context.Background())
	g.fullCancel = cancel
	changed := object
	changed.ETag = `"changed"`
	g.replace([]storage.Object{changed})
	if len(g.thumbs) != 0 || g.full.bitmap != nil || g.fullKey != "" || ctx.Err() == nil || g.preview.ETag != changed.ETag {
		t.Fatal("changed remote object kept stale preview or thumbnail")
	}
	g.replace(nil)
	if g.preview != nil || g.previewOpen {
		t.Fatal("removed remote object kept preview open")
	}
}
