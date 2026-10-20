package gui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/disintegration/imaging"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/imageproc"
	"github.com/knothhe/picup/internal/storage"
)

const galleryPageSize = 20

type galleryThumb struct {
	bitmap        *ui.Bitmap
	width, height int
	err           error
}
type galleryState struct {
	objects                                 []storage.Object
	config                                  config.Config
	client                                  *storage.Client
	cache                                   *galleryDiskCache
	loaded, busy, dirty                     bool
	operation                               bool
	err                                     string
	generation                              int
	cancel                                  context.CancelFunc
	selected                                map[string]bool
	search, prefix, order, period, from, to string
	page                                    int
	filterKey                               string
	thumbs                                  map[string]galleryThumb
	thumbCancel                             context.CancelFunc
	thumbBatch                              string
	preview                                 *storage.Object
	previewOpen, renameOpen, deleteOpen     bool
	renameKey, renameError                  string
	deleteTargets                           []storage.Object
	filterOpen, sortOpen, dateOpen          bool
	prefixQuery, selectionAnchor            string
	selectionOrder                          []string
	calendarMonth                           time.Time
	preferences                             *galleryPreferenceStore
	optionsOpen, infoOpen                   bool
	optionsKey                              string
	full                                    galleryThumb
	fullKey                                 string
	fullCancel                              context.CancelFunc
	fullSerial                              int
}

func sameGalleryStorage(a, b config.Config) bool {
	return a.Provider == b.Provider && a.Endpoint == b.Endpoint && a.Region == b.Region && a.Bucket == b.Bucket && a.AccessKey == b.AccessKey && a.SecretKey == b.SecretKey && a.SessionToken == b.SessionToken && a.PublicURL == b.PublicURL && a.PathStyle == b.PathStyle
}
func (g *galleryState) stop() {
	if g.fullCancel != nil {
		g.fullCancel()
	}
	if g.cancel != nil {
		g.cancel()
	}
	if g.thumbCancel != nil {
		g.thumbCancel()
	}
}
func (a *application) resetGallery() {
	preferences, store := a.gallery.viewPreferences(), a.gallery.preferences
	a.gallery.stop()
	a.gallery = galleryState{generation: a.gallery.generation + 1, preferences: store}
	a.gallery.applyViewPreferences(preferences)
}
func (a *application) galleryClient() (*storage.Client, error) {
	c, err := a.effective()
	if err != nil {
		return nil, err
	}
	g := &a.gallery
	if g.client != nil && !sameGalleryStorage(g.config, c) {
		a.resetGallery()
	}
	g = &a.gallery
	client, err := storage.New(c)
	if err != nil {
		return nil, err
	}
	g.config, g.client = c, client
	if g.cache == nil {
		g.cache = newGalleryDiskCache(c)
	}
	return client, nil
}
func (a *application) enterGallery() {
	_, err := a.galleryClient()
	if err != nil {
		a.gallery.err = tf("请先完成存储设置：%s", err.Error())
		return
	}
	if !a.gallery.busy {
		a.loadGallery()
	}
}
func (a *application) loadGallery() {
	if a.gallery.busy {
		return
	}
	client, err := a.galleryClient()
	if err != nil {
		a.gallery.err = tf("请先完成存储设置：%s", err.Error())
		a.feedback(tr("图库加载失败"), err)
		return
	}
	g := &a.gallery
	g.busy = true
	g.err = ""
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	g.cancel = cancel
	generation := g.generation
	cache, showCache := g.cache, !g.loaded
	go func() {
		defer cancel()
		if showCache {
			if objects, cacheErr := cache.readList(); cacheErr == nil && ctx.Err() == nil {
				for i := range objects {
					objects[i].URL = client.URL(objects[i].Key)
				}
				mygo.RunOnMain(func() {
					if a.gallery.generation == generation && a.gallery.busy && !a.gallery.loaded {
						a.gallery.replace(objects)
						a.gallery.dirty = true
						if a.win != nil {
							a.win.Invalidate()
						}
					}
				})
			}
		}
		objects, err := client.List(ctx, "")
		if err == nil {
			_ = cache.writeList(objects)
		}
		mygo.RunOnMain(func() {
			if a.gallery.generation != generation {
				return
			}
			g := &a.gallery
			g.busy = false
			g.cancel = nil
			if err != nil {
				g.err = tf("图库加载失败：%s", err.Error())
				a.feedback(tr("图库加载失败，可重试"), err)
				return
			}
			g.replace(objects)
			a.feedback(tf("已加载 %d 张云端图片", len(objects)), nil)
		})
	}()
}
func (g *galleryState) replace(objects []storage.Object) {
	if g.thumbCancel != nil {
		g.thumbCancel()
	}
	oldObjects := map[string]storage.Object{}
	for _, o := range g.objects {
		if thumb, ok := g.thumbs[o.Key]; ok && thumb.err == nil {
			oldObjects[o.Key] = o
		}
	}
	thumbs := map[string]galleryThumb{}
	valid := map[string]bool{}
	previewFound := false
	for _, o := range objects {
		valid[o.Key] = true
		if old, ok := oldObjects[o.Key]; ok && sameGalleryObject(old, o) {
			thumbs[o.Key] = g.thumbs[o.Key]
		}
		if g.preview != nil && g.preview.Key == o.Key {
			previewFound = true
			if !sameGalleryObject(*g.preview, o) {
				g.clearFullPreview()
			}
			copy := o
			g.preview = &copy
		}
	}
	if g.preview != nil && !previewFound {
		g.clearFullPreview()
		g.preview = nil
		g.previewOpen = false
		g.renameOpen = false
	}
	g.objects = objects
	g.loaded = true
	g.dirty = false
	g.err = ""
	g.thumbs = thumbs
	g.thumbBatch = ""
	for key := range g.selected {
		if !valid[key] {
			delete(g.selected, key)
		}
	}
	g.pruneSelectionOrder()
}

func sameGalleryObject(a, b storage.Object) bool {
	return a.Key == b.Key && a.ETag == b.ETag && a.Size == b.Size && a.Modified.Equal(b.Modified)
}

func (g *galleryState) clearFullPreview() {
	if g.fullCancel != nil {
		g.fullCancel()
		g.fullCancel = nil
	}
	g.fullSerial++
	g.full = galleryThumb{}
	g.fullKey = ""
}

func (g *galleryState) filtered(now time.Time) ([]storage.Object, error) {
	var from, to time.Time
	if g.period == "custom" {
		var err error
		if g.from != "" {
			from, err = time.ParseInLocation("2006-01-02", g.from, time.Local)
			if err != nil {
				return nil, errors.New(tr("开始日期请使用 YYYY-MM-DD"))
			}
		}
		if g.to != "" {
			to, err = time.ParseInLocation("2006-01-02", g.to, time.Local)
			if err != nil {
				return nil, errors.New(tr("结束日期请使用 YYYY-MM-DD"))
			}
			to = to.AddDate(0, 0, 1)
		}
		if !from.IsZero() && !to.IsZero() && !from.Before(to) {
			return nil, errors.New(tr("开始日期不能晚于结束日期"))
		}
	} else {
		switch g.period {
		case "7d":
			from = now.AddDate(0, 0, -7)
		case "14d":
			from = now.AddDate(0, 0, -14)
		case "30d":
			from = now.AddDate(0, 0, -30)
		case "3m":
			from = now.AddDate(0, -3, 0)
		case "6m":
			from = now.AddDate(0, -6, 0)
		case "1y":
			from = now.AddDate(-1, 0, 0)
		}
	}
	if g.period != "custom" && !from.IsZero() {
		to = now.Add(time.Nanosecond)
	}
	var rows []storage.Object
	scores := map[string]float64{}
	query := strings.TrimSpace(g.search)
	for _, o := range g.objects {
		if query != "" {
			score, ok := gallerySearchScore(o.Key, query)
			if !ok {
				continue
			}
			scores[o.Key] = score
		}
		if g.prefix == "/" {
			if strings.Contains(o.Key, "/") {
				continue
			}
		} else if g.prefix != "" && !strings.HasPrefix(o.Key, g.prefix) {
			continue
		}
		if !from.IsZero() && o.Modified.Before(from) || !to.IsZero() && !o.Modified.Before(to) {
			continue
		}
		rows = append(rows, o)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if query != "" {
			return scores[rows[i].Key] < scores[rows[j].Key]
		}
		x, y := rows[i], rows[j]
		switch g.order {
		case "name-asc":
			return x.Key < y.Key
		case "", "name-desc":
			return x.Key > y.Key
		case "oldest":
			if !x.Modified.Equal(y.Modified) {
				return x.Modified.Before(y.Modified)
			}
		default:
			if !x.Modified.Equal(y.Modified) {
				return x.Modified.After(y.Modified)
			}
		}
		return x.Key < y.Key
	})
	return rows, nil
}
func (g *galleryState) pageRows(rows []storage.Object) ([]storage.Object, int) {
	pages := max(1, (len(rows)+galleryPageSize-1)/galleryPageSize)
	g.page = max(0, min(g.page, pages-1))
	start := min(len(rows), g.page*galleryPageSize)
	return rows[start:min(len(rows), start+galleryPageSize)], pages
}
func (g *galleryState) prefixes() ([]string, []string) {
	set := map[string]bool{}
	for _, o := range g.objects {
		parts := strings.Split(o.Key, "/")
		for i := 1; i < len(parts); i++ {
			set[strings.Join(parts[:i], "/")+"/"] = true
		}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return append([]string{"", "/"}, keys...), append([]string{tr("全部目录"), tr("仅根目录")}, keys...)
}
func (g *galleryState) selectPage(rows []storage.Object) {
	if g.selected == nil {
		g.selected = map[string]bool{}
	}
	for _, o := range rows {
		if !g.selected[o.Key] {
			g.selectionOrder = append(g.selectionOrder, o.Key)
			g.selectionAnchor = o.Key
		}
		g.selected[o.Key] = true
	}
}
func (g *galleryState) pruneSelectionOrder() {
	kept := g.selectionOrder[:0]
	for _, key := range g.selectionOrder {
		if g.selected[key] {
			kept = append(kept, key)
		}
	}
	g.selectionOrder = kept
	g.selectionAnchor = ""
	if len(kept) > 0 {
		g.selectionAnchor = kept[len(kept)-1]
	}
}
func (g *galleryState) selectedObjects() []storage.Object {
	var rows []storage.Object
	for _, o := range g.objects {
		if g.selected[o.Key] {
			rows = append(rows, o)
		}
	}
	return rows
}
func (g *galleryState) remove(keys map[string]bool) {
	var keep []storage.Object
	for _, o := range g.objects {
		if !keys[o.Key] {
			keep = append(keep, o)
		} else {
			delete(g.selected, o.Key)
			delete(g.thumbs, o.Key)
		}
	}
	g.objects = keep
	g.pruneSelectionOrder()
	g.thumbBatch = ""
	if g.preview != nil && keys[g.preview.Key] {
		g.clearFullPreview()
		g.previewOpen = false
		g.renameOpen = false
		g.preview = nil
	}
}

// Preview reads signed object data instead of assuming the public URL is public.
// Bound both encoded and decoded size before creating a small UI bitmap.
type previewWriter struct {
	buffer bytes.Buffer
	limit  int
}

func (w *previewWriter) Write(b []byte) (int, error) {
	limit := w.limit
	if limit == 0 {
		limit = 32 << 20
	}
	if w.buffer.Len()+len(b) > limit {
		return 0, errors.New(tf("图片超过 %d MiB 预览上限，请下载原图", limit>>20))
	}
	return w.buffer.Write(b)
}
func readGalleryThumb(ctx context.Context, client *storage.Client, key string) galleryThumb {
	return readGalleryImage(ctx, client, key, false)
}
func readGalleryImage(ctx context.Context, client *storage.Client, key string, full bool) galleryThumb {
	return readGalleryImageWithCache(ctx, client, key, full, nil, storage.Object{})
}
func readGalleryCachedThumb(ctx context.Context, client *storage.Client, object storage.Object, cache *galleryDiskCache) galleryThumb {
	if thumb, ok := cache.readThumb(object); ok {
		return thumb
	}
	return readGalleryImageWithCache(ctx, client, object.Key, false, cache, object)
}
func readGalleryImageWithCache(ctx context.Context, client *storage.Client, key string, full bool, cache *galleryDiskCache, object storage.Object) galleryThumb {
	var b previewWriter
	if full {
		b.limit = imageproc.MaxBytes
	}
	if err := client.Read(ctx, key, &b); err != nil {
		return galleryThumb{err: err}
	}
	info, _, err := imageproc.DecodeConfig(bytes.NewReader(b.buffer.Bytes()))
	if err != nil {
		return galleryThumb{err: errors.New(tr("此格式暂不支持预览，可下载原图"))}
	}
	if !imageproc.ValidDimensions(info.Width, info.Height) {
		return galleryThumb{err: errors.New(tr("图片尺寸超过预览上限，请下载原图"))}
	}
	img, err := imageproc.Decode(b.buffer.Bytes())
	if err != nil {
		return galleryThumb{err: err}
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if full {
		return galleryThumb{bitmap: ui.NewBitmap(img), width: w, height: h}
	}
	img = imaging.Fit(img, 960, 720, imaging.Lanczos)
	if ctx.Err() == nil {
		_ = cache.writeThumb(object, img, w, h)
	}
	return galleryThumb{bitmap: ui.NewBitmap(img), width: w, height: h}
}
func (a *application) galleryFullPreview() {
	g := &a.gallery
	if !g.previewOpen || g.preview == nil {
		if g.fullCancel != nil {
			g.fullCancel()
			g.fullCancel = nil
		}
		g.fullKey = ""
		g.full = galleryThumb{}
		return
	}
	if a.win == nil || g.client == nil || g.fullKey == g.preview.Key {
		return
	}
	if g.fullCancel != nil {
		g.fullCancel()
	}
	g.fullSerial++
	serial, generation, key, client := g.fullSerial, g.generation, g.preview.Key, g.client
	g.fullKey = key
	g.full = galleryThumb{}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	g.fullCancel = cancel
	go func() {
		defer cancel()
		full := readGalleryImage(ctx, client, key, true)
		mygo.RunOnMain(func() {
			g := &a.gallery
			if g.generation != generation || g.fullSerial != serial || g.fullKey != key || !g.previewOpen {
				return
			}
			g.full = full
			g.fullCancel = nil
			a.win.Invalidate()
		})
	}()
}
func (g *galleryState) thumbnailKeys(rows []storage.Object) []string {
	keys := make([]string, 0, len(rows)+1)
	previewIncluded := false
	for _, o := range rows {
		keys = append(keys, o.Key)
		if g.preview != nil && o.Key == g.preview.Key {
			previewIncluded = true
		}
	}
	if g.previewOpen && g.preview != nil && !previewIncluded {
		keys = append(keys, g.preview.Key)
	}
	return keys
}
func (a *application) galleryThumbs(rows []storage.Object) {
	g := &a.gallery
	if a.win == nil || g.client == nil {
		return
	}
	keys := g.thumbnailKeys(rows)
	batch := strings.Join(keys, "\x00")
	if g.thumbBatch == batch {
		return
	}
	if g.thumbCancel != nil {
		g.thumbCancel()
	}
	g.thumbBatch = batch
	// Retain just the current page and preview, keeping memory bounded.
	cache := map[string]galleryThumb{}
	for _, key := range keys {
		if t, ok := g.thumbs[key]; ok {
			cache[key] = t
		}
	}
	if g.preview != nil {
		if t, ok := g.thumbs[g.preview.Key]; ok {
			cache[g.preview.Key] = t
		}
	}
	g.thumbs = cache
	ctx, cancel := context.WithCancel(context.Background())
	g.thumbCancel = cancel
	generation, client, disk := g.generation, g.client, g.cache
	objects := make(map[string]storage.Object, len(rows)+1)
	for _, o := range rows {
		objects[o.Key] = o
	}
	if g.preview != nil {
		objects[g.preview.Key] = *g.preview
	}
	var missing []storage.Object
	for _, key := range keys {
		if _, ok := cache[key]; !ok {
			missing = append(missing, objects[key])
		}
	}
	go func() {
		queue := make(chan storage.Object)
		var workers sync.WaitGroup
		for range 2 {
			workers.Go(func() {
				for object := range queue {
					if ctx.Err() != nil {
						continue
					}
					one, stop := context.WithTimeout(ctx, 40*time.Second)
					thumb := readGalleryCachedThumb(one, client, object, disk)
					stop()
					mygo.RunOnMain(func() {
						if a.gallery.generation == generation && ctx.Err() == nil {
							a.gallery.thumbs[object.Key] = thumb
							a.win.Invalidate()
						}
					})
				}
			})
		}
		for _, object := range missing {
			select {
			case queue <- object:
			case <-ctx.Done():
				close(queue)
				workers.Wait()
				return
			}
		}
		close(queue)
		workers.Wait()
	}()
}

func (a *application) galleryOperation(run func(context.Context, *storage.Client) func()) {
	g := &a.gallery
	if g.busy || g.client == nil {
		return
	}
	g.busy = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	g.operation = true
	g.cancel = cancel
	generation, client := g.generation, g.client
	go func() {
		defer cancel()
		complete := run(ctx, client)
		mygo.RunOnMain(func() {
			if a.gallery.generation != generation {
				return
			}
			a.gallery.busy = false
			a.gallery.operation = false
			a.gallery.cancel = nil
			complete()
			if a.win != nil {
				a.win.Invalidate()
			}
		})
	}()
}
func (a *application) deleteGallery() {
	targets := append([]storage.Object(nil), a.gallery.deleteTargets...)
	objects, cache := append([]storage.Object(nil), a.gallery.objects...), a.gallery.cache
	a.galleryOperation(func(ctx context.Context, client *storage.Client) func() {
		removed := map[string]bool{}
		var first error
		for _, o := range targets {
			if err := client.Delete(ctx, o.Key); err != nil {
				if first == nil {
					first = fmt.Errorf("%s：%w", o.Key, err)
				}
			} else {
				removed[o.Key] = true
			}
		}
		if len(removed) > 0 {
			var kept []storage.Object
			for _, object := range objects {
				if !removed[object.Key] {
					kept = append(kept, object)
				}
			}
			_ = cache.writeList(kept)
		}
		return func() {
			a.gallery.remove(removed)
			a.gallery.deleteTargets = nil
			if first != nil {
				a.feedback(tf("已删除 %d 张，%d 张未删除，可重新选择后重试", len(removed), len(targets)-len(removed)), first)
			} else {
				a.feedback(tf("已删除 %d 张云端图片", len(removed)), nil)
			}
		}
	})
}
func (a *application) renameGallery() {
	g := &a.gallery
	if g.preview == nil {
		return
	}
	old, newKey := *g.preview, g.renameKey
	objects, cache := append([]storage.Object(nil), g.objects...), g.cache
	if err := storage.ValidateImageKey(newKey); err != nil {
		g.renameError = err.Error()
		return
	}
	g.renameError = ""
	a.galleryOperation(func(ctx context.Context, client *storage.Client) func() {
		renamed, err := client.Rename(ctx, old, newKey)
		if err == nil {
			for i, object := range objects {
				if object.Key == old.Key {
					objects[i] = renamed
				}
			}
			_ = cache.writeList(objects)
		} else {
			var partial *storage.RenamePartialError
			if errors.As(err, &partial) {
				_ = cache.discard()
			}
		}
		return func() {
			g := &a.gallery
			if err != nil {
				g.renameError = err.Error()
				g.dirty = true
				var partial *storage.RenamePartialError
				if errors.As(err, &partial) {
					g.renameOpen = false
					g.previewOpen = false
					a.feedback(tr("重命名未完全完成，请刷新图库检查两份图片"), err)
				}
				return
			}
			selected := g.selected[old.Key]
			thumb, hasThumb := g.thumbs[old.Key]
			oldFull, oldFullKey := g.full, g.fullKey
			g.remove(map[string]bool{old.Key: true})
			g.objects = append(g.objects, renamed)
			if hasThumb {
				g.thumbs[newKey] = thumb
			}
			if selected {
				if g.selected == nil {
					g.selected = map[string]bool{}
				}
				g.selected[newKey] = true
				g.selectionOrder = append(g.selectionOrder, newKey)
				g.selectionAnchor = newKey
			}
			g.preview = &renamed
			g.previewOpen = true
			if oldFullKey == old.Key && oldFull.bitmap != nil {
				g.full = oldFull
				g.fullKey = newKey
			}
			g.renameOpen = false
			g.renameError = ""
			g.thumbBatch = ""
			a.feedback(tr("图片已重命名，请使用新链接"), nil)
		}
	})
}
func saveGalleryFile(ctx context.Context, client *storage.Client, key, dst string) error {
	f, err := os.CreateTemp(filepath.Dir(dst), ".picup-download-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = client.Read(ctx, key, f)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return os.Rename(f.Name(), dst)
}
func (a *application) downloadGallery(o storage.Object) {
	a.galleryOperation(func(ctx context.Context, client *storage.Client) func() {
		dst, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: a.win, Title: tr("下载原图"), DefaultPath: path.Base(o.Key), CreateDirectories: true})
		if err == nil && dst != "" {
			err = saveGalleryFile(ctx, client, o.Key, dst)
		}
		return func() {
			if dst == "" && err == nil {
				return
			}
			if err != nil {
				a.feedback(tr("下载失败，可重试"), err)
			} else {
				a.feedback(tf("已下载到 %s", dst), nil)
			}
		}
	})
}
