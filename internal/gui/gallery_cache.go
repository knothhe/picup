package gui

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/imageproc"
	"github.com/knothhe/picup/internal/storage"
)

const (
	galleryCacheLimit = int64(256 << 20)
	galleryIndexLimit = 16 << 20
	galleryThumbLimit = 5 << 20
)

// All cache IO runs on workers. Serializing mutation also keeps a clear from
// racing an in-flight download that would otherwise recreate removed files.
var galleryCacheLock sync.Mutex
var galleryCacheEpoch atomic.Uint64

type galleryDiskCache struct {
	root, dir string
	limit     int64
	epoch     uint64
}

type galleryCachedObject struct {
	Key      string    `json:"key"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	ETag     string    `json:"etag,omitempty"`
}

type galleryCachedIndex struct {
	Version int                   `json:"version"`
	Objects []galleryCachedObject `json:"objects"`
}

type galleryCachedThumb struct {
	Version       int `json:"version"`
	Width, Height int
	PNG           []byte `json:"png"`
}

func galleryCacheDirectory() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "picup", "gallery-v1"), nil
}

func galleryDigest(value any) string {
	b, _ := json.Marshal(value)
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:])
}

func newGalleryDiskCache(c config.Config) *galleryDiskCache {
	root, err := galleryCacheDirectory()
	if err != nil {
		return nil
	}
	// Credentials participate in identity but are never written to the index,
	// filenames or thumbnail headers. Upload/appearance preferences do not.
	identity := struct {
		Provider, Endpoint, Region, Bucket, AccessKey, SecretKey, SessionToken, PublicURL string
		PathStyle                                                                         bool
	}{c.Provider, c.Endpoint, c.Region, c.Bucket, c.AccessKey, c.SecretKey, c.SessionToken, c.PublicURL, c.PathStyle}
	return &galleryDiskCache{root: root, dir: filepath.Join(root, galleryDigest(identity)), limit: galleryCacheLimit, epoch: galleryCacheEpoch.Load()}
}

func galleryObjectDigest(o storage.Object) string {
	return galleryDigest(galleryCachedObject{Key: o.Key, Size: o.Size, Modified: o.Modified.UTC(), ETag: o.ETag})
}

func readGalleryCacheFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = errors.New("gallery cache entry exceeds limit")
	}
	return b, err
}

func (c *galleryDiskCache) readListLocked() ([]storage.Object, error) {
	if c == nil || c.epoch != galleryCacheEpoch.Load() {
		return nil, os.ErrNotExist
	}
	b, err := readGalleryCacheFile(filepath.Join(c.dir, "index.json"), galleryIndexLimit)
	if err != nil {
		return nil, err
	}
	var index galleryCachedIndex
	if err = json.Unmarshal(b, &index); err != nil {
		return nil, err
	}
	if index.Version != 1 {
		return nil, errors.New("unsupported gallery cache version")
	}
	objects := make([]storage.Object, 0, len(index.Objects))
	for _, o := range index.Objects {
		objects = append(objects, storage.Object{Key: o.Key, Size: o.Size, Modified: o.Modified, ETag: o.ETag})
	}
	return objects, nil
}

func (c *galleryDiskCache) readList() ([]storage.Object, error) {
	galleryCacheLock.Lock()
	defer galleryCacheLock.Unlock()
	objects, err := c.readListLocked()
	if err == nil {
		now := time.Now()
		_ = os.Chtimes(filepath.Join(c.dir, "index.json"), now, now)
	}
	return objects, err
}

func (c *galleryDiskCache) writeList(objects []storage.Object) error {
	if c == nil {
		return nil
	}
	index := galleryCachedIndex{Version: 1, Objects: make([]galleryCachedObject, 0, len(objects))}
	valid := make(map[string]bool, len(objects))
	for _, o := range objects {
		index.Objects = append(index.Objects, galleryCachedObject{Key: o.Key, Size: o.Size, Modified: o.Modified.UTC(), ETag: o.ETag})
		valid[galleryObjectDigest(o)+".thumb"] = true
	}
	b, err := json.Marshal(index)
	if err != nil {
		return err
	}
	if len(b) > galleryIndexLimit {
		return errors.New("gallery cache index exceeds limit")
	}
	galleryCacheLock.Lock()
	defer galleryCacheLock.Unlock()
	if c.epoch != galleryCacheEpoch.Load() {
		return nil
	}
	if err = config.AtomicWrite(filepath.Join(c.dir, "index.json"), b, 0600); err != nil {
		return err
	}
	files, _ := os.ReadDir(c.dir)
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".thumb") && !valid[file.Name()] {
			_ = os.Remove(filepath.Join(c.dir, file.Name()))
		}
	}
	return c.pruneLocked()
}

func (c *galleryDiskCache) readThumb(o storage.Object) (galleryThumb, bool) {
	if c == nil {
		return galleryThumb{}, false
	}
	galleryCacheLock.Lock()
	if c.epoch != galleryCacheEpoch.Load() {
		galleryCacheLock.Unlock()
		return galleryThumb{}, false
	}
	path := filepath.Join(c.dir, galleryObjectDigest(o)+".thumb")
	b, err := readGalleryCacheFile(path, galleryThumbLimit)
	if err == nil {
		now := time.Now()
		_ = os.Chtimes(path, now, now)
	}
	galleryCacheLock.Unlock()
	if err != nil {
		return galleryThumb{}, false
	}
	var thumb galleryCachedThumb
	if json.Unmarshal(b, &thumb) != nil || thumb.Version != 1 || thumb.Width <= 0 || thumb.Height <= 0 || int64(thumb.Width) > imageproc.MaxPixels/int64(thumb.Height) {
		return galleryThumb{}, false
	}
	info, err := png.DecodeConfig(bytes.NewReader(thumb.PNG))
	if err != nil || info.Width <= 0 || info.Height <= 0 || info.Width > 960 || info.Height > 720 {
		return galleryThumb{}, false
	}
	img, err := png.Decode(bytes.NewReader(thumb.PNG))
	if err != nil {
		return galleryThumb{}, false
	}
	return galleryThumb{bitmap: ui.NewBitmap(img), width: thumb.Width, height: thumb.Height}, true
}

func (c *galleryDiskCache) writeThumb(o storage.Object, img image.Image, width, height int) error {
	if c == nil {
		return nil
	}
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		return err
	}
	b, err := json.Marshal(galleryCachedThumb{Version: 1, Width: width, Height: height, PNG: data.Bytes()})
	if err != nil {
		return err
	}
	if len(b) > galleryThumbLimit {
		return errors.New("gallery cache thumbnail exceeds limit")
	}
	galleryCacheLock.Lock()
	defer galleryCacheLock.Unlock()
	// Check the authoritative cached list again after encoding. A refreshed,
	// renamed or deleted object must not be resurrected by an older download.
	objects, err := c.readListLocked()
	if err != nil {
		return nil
	}
	digest := galleryObjectDigest(o)
	for _, current := range objects {
		if galleryObjectDigest(current) == digest {
			if err = config.AtomicWrite(filepath.Join(c.dir, digest+".thumb"), b, 0600); err != nil {
				return err
			}
			return c.pruneLocked()
		}
	}
	return nil
}

func (c *galleryDiskCache) discard() error {
	if c == nil {
		return nil
	}
	galleryCacheLock.Lock()
	defer galleryCacheLock.Unlock()
	if c.epoch != galleryCacheEpoch.Load() {
		return nil
	}
	return os.RemoveAll(c.dir)
}

type galleryCacheFile struct {
	path string
	size int64
	used time.Time
}

func galleryCacheFiles(root string) ([]galleryCacheFile, int64, error) {
	var files []galleryCacheFile
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files = append(files, galleryCacheFile{path, info.Size(), info.ModTime()})
		total += info.Size()
		return nil
	})
	return files, total, err
}

func (c *galleryDiskCache) pruneLocked() error {
	files, total, err := galleryCacheFiles(c.root)
	if err != nil || total <= c.limit {
		return err
	}
	sort.Slice(files, func(i, j int) bool {
		// Preserve compact object lists while there are thumbnails to evict, so
		// filling the image cache does not destroy next launch's offline list.
		iThumb := strings.HasSuffix(files[i].path, ".thumb")
		jThumb := strings.HasSuffix(files[j].path, ".thumb")
		if iThumb != jThumb {
			return iThumb
		}
		if files[i].used.Equal(files[j].used) {
			return files[i].path < files[j].path
		}
		return files[i].used.Before(files[j].used)
	})
	for _, file := range files {
		if total <= c.limit {
			break
		}
		if err = os.Remove(file.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		total -= file.size
	}
	return nil
}

// galleryCacheSize and clearGalleryCache perform disk IO; settings should call
// them on a worker or use the application's asynchronous clear method below.
func galleryCacheSize() (int64, error) {
	root, err := galleryCacheDirectory()
	if err != nil {
		return 0, err
	}
	galleryCacheLock.Lock()
	defer galleryCacheLock.Unlock()
	_, total, err := galleryCacheFiles(root)
	return total, err
}

func clearGalleryCache() error {
	root, err := galleryCacheDirectory()
	if err != nil {
		return err
	}
	galleryCacheLock.Lock()
	defer galleryCacheLock.Unlock()
	galleryCacheEpoch.Add(1)
	return os.RemoveAll(root)
}

func (a *application) clearGalleryCache() {
	if a.gallery.thumbCancel != nil {
		a.gallery.thumbCancel()
	}
	go func() {
		err := clearGalleryCache()
		mygo.RunOnMain(func() {
			a.gallery.cache = nil
			if a.gallery.client != nil {
				a.gallery.cache = newGalleryDiskCache(a.gallery.config)
			}
			a.gallery.thumbs = nil
			a.gallery.thumbBatch = ""
			if err != nil {
				a.feedback(tr("清理图库缓存失败"), err)
			} else {
				a.feedback(tr("图库本地缓存已清理"), nil)
			}
		})
	}()
}
