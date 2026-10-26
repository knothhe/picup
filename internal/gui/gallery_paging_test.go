package gui

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/storage"
)

func TestGalleryPageSnapshotsNeverReplaceWholeBucketCache(t *testing.T) {
	cache := testGalleryCache(t, t.TempDir(), config.Default())
	all := []storage.Object{{Key: "a/one.png"}, {Key: "b/two.png"}, {Key: "root.png"}}
	if err := cache.writeList(all); err != nil {
		t.Fatal(err)
	}
	if err := cache.writePage("a/", all[:1], "more-a"); err != nil {
		t.Fatal(err)
	}
	if err := cache.writePage("", all[:2], "more-bucket"); err != nil {
		t.Fatal(err)
	}
	complete, err := cache.readList()
	if err != nil || !reflect.DeepEqual(complete, all) {
		t.Fatal("a partial page replaced the complete bucket snapshot", complete, err)
	}
	restarted := *cache
	objects, next, err := restarted.readPage("a/")
	if err != nil || len(objects) != 1 || objects[0].Key != "a/one.png" || next != "more-a" {
		t.Fatal("scoped continuation did not survive restart", objects, next, err)
	}
	if _, _, err = restarted.readPage("b/"); !os.IsNotExist(err) {
		t.Fatal("another prefix read the first prefix's snapshot", err)
	}
}

func TestGalleryScopedMutationPreservesOtherDirectoriesAndContinuations(t *testing.T) {
	cache := testGalleryCache(t, t.TempDir(), config.Default())
	all := []storage.Object{{Key: "a/one.png"}, {Key: "b/two.png"}, {Key: "root.png"}}
	if err := cache.writeList(all); err != nil {
		t.Fatal(err)
	}
	if err := cache.writePage("a/", all[:1], "more-a"); err != nil {
		t.Fatal(err)
	}
	if err := cache.writePage("b/", all[1:2], "more-b"); err != nil {
		t.Fatal(err)
	}
	renamed := storage.Object{Key: "b/renamed.png"}
	if err := cache.mutateLists(map[string]bool{"a/one.png": true}, &renamed); err != nil {
		t.Fatal(err)
	}
	whole, err := cache.readList()
	if err != nil || len(whole) != 3 || whole[0].Key != "b/two.png" || whole[1].Key != "root.png" || whole[2].Key != renamed.Key {
		t.Fatal("scoped rename discarded unrelated whole-bucket objects", whole, err)
	}
	a, aNext, err := cache.readPage("a/")
	if err != nil || len(a) != 0 || aNext != "more-a" {
		t.Fatal("source prefix retained the moved object or lost its cursor", a, aNext, err)
	}
	b, bNext, err := cache.readPage("b/")
	if err != nil || len(b) != 2 || bNext != "more-b" {
		t.Fatal("target prefix did not receive the moved object", b, bNext, err)
	}
	// Updating a partial-only cache must not invent a complete bucket index.
	partialOnly := testGalleryCache(t, t.TempDir(), config.Default())
	if err = partialOnly.writePage("a/", all[:1], "next"); err != nil {
		t.Fatal(err)
	}
	if err = partialOnly.mutateLists(map[string]bool{"a/one.png": true}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = partialOnly.readList(); !os.IsNotExist(err) {
		t.Fatal("partial mutation invented a complete listing", err)
	}
}

func TestGalleryPagedThumbnailCacheUsesCurrentScopeVersion(t *testing.T) {
	cache := testGalleryCache(t, t.TempDir(), config.Default()).scoped("a/")
	old := storage.Object{Key: "a/one.png", ETag: "old"}
	current := old
	current.ETag = "current"
	if err := cache.writeList([]storage.Object{old}); err != nil {
		t.Fatal(err)
	}
	if err := cache.writePage("a/", []storage.Object{current}, "more"); err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	if err := cache.writeThumb(old, img, 3, 2); err != nil {
		t.Fatal(err)
	}
	if _, hit := cache.readThumb(old); hit {
		t.Fatal("an old complete cache allowed a stale scoped thumbnail to return")
	}
	if err := cache.writeThumb(current, img, 3, 2); err != nil {
		t.Fatal(err)
	}
	if thumb, hit := cache.readThumb(current); !hit || thumb.width != 3 || thumb.height != 2 {
		t.Fatal("partial listing could not persist its current thumbnail", hit)
	}
}

func TestGalleryPrefixChangeCancelsAndRejectsOlderResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	thumbCtx, cancelThumb := context.WithCancel(context.Background())
	a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: config.Default(), gallery: galleryFixture()}
	g := &a.gallery
	g.paged, g.listPrefix, g.prefix, g.nextToken = true, "old/", "old/", "old-token"
	g.busy, g.cancel, g.thumbCancel = true, cancel, cancelThumb
	g.generation, g.listSerial = 9, 3
	g.selected = map[string]bool{g.objects[0].Key: true}
	a.setGalleryPrefix("new/")
	if ctx.Err() == nil || thumbCtx.Err() == nil || len(g.objects) != 0 || len(g.selected) != 0 || g.nextToken != "" || g.prefix != "new/" {
		t.Fatal("changing directory retained old reads, rows, selection, or continuation")
	}
	if g.acceptsListing(9, 3, "old/") || g.acceptsListing(g.generation, g.listSerial, "old/") || g.acceptsListing(g.generation, g.listSerial-1, "new/") {
		t.Fatal("stale prefix or refresh completion could replace the current listing")
	}
}

func TestGalleryAppendDeduplicatesObjectsAndRetainsSelection(t *testing.T) {
	g := galleryFixture()
	first := g.objects[0]
	changed := first
	changed.ETag = "new"
	newObject := storage.Object{Key: "next-page.png"}
	g.selected[first.Key] = true
	g.replace(mergeGalleryObjects(g.objects, []storage.Object{changed, newObject, newObject}))
	if len(g.objects) != 24 || g.objects[0].ETag != "new" || !g.selected[first.Key] {
		t.Fatal("loading another page duplicated objects or lost an existing selection")
	}
}

func TestGalleryPartialListControlsFitNativeWindow(t *testing.T) {
	for _, size := range [][2]int{{1040, 760}, {760, 620}} {
		a := &application{config: config.Default(), tab: 4, gallery: galleryFixture()}
		a.gallery.paged, a.gallery.listComplete, a.gallery.nextToken = true, false, "next"
		tt := ui.NewTester(nativeTestView(a), size[0], size[1])
		tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
		tt.Frame()
		tt.Advance(time.Second)
		for _, label := range []string{"目录前缀", "加载目录", "全部目录", "加载更多图片", "下一页"} {
			assertGalleryVisible(t, tt, label, size[0], size[1])
		}
	}
}
