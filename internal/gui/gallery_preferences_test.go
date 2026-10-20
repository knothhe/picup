package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
)

func TestGalleryPreferencesRestoreBeforeLoadingAndSurviveStorageReset(t *testing.T) {
	a := &application{path: filepath.Join(t.TempDir(), "personal.json"), config: config.Default(), tab: 4}
	if err := a.restoreGalleryPreferences(); err != nil {
		t.Fatal(err)
	}
	want := galleryPreferences{Version: 1, Search: "海边", Prefix: "images/旅行/", Order: "oldest", Period: "custom", From: "2026-01-01", To: "2026-12-31", Page: 1}
	a.gallery.applyViewPreferences(want)
	a.queueGalleryPreferences()
	a.flushGalleryPreferences()
	path := filepath.Join(filepath.Dir(a.path), "personal.gallery.json")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("gallery preferences were not saved privately", info, err)
	}
	if _, err = os.Stat(a.path); !os.IsNotExist(err) {
		t.Fatal("gallery preferences touched the storage configuration", err)
	}
	b := &application{path: a.path, config: config.Default(), tab: 4}
	if err = b.restoreGalleryPreferences(); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(nativeTestView(b), 760, 620)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	if got := b.gallery.viewPreferences(); got != want {
		t.Fatalf("restored preferences changed before objects loaded: %+v", got)
	}
	b.gallery.replace(galleryFixture().objects)
	rows, err := b.gallery.filtered(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	page, pages := b.gallery.pageRows(rows)
	if b.gallery.page != 1 || pages != 2 || len(page) != 3 {
		t.Fatal("restored page was not used after loading", b.gallery.page, pages, len(page))
	}
	store := b.gallery.preferences
	b.resetGallery()
	if got := b.gallery.viewPreferences(); got != want || b.gallery.preferences != store || b.gallery.loaded {
		t.Fatal("storage reset lost view preferences or kept old cloud state", got)
	}
	b.flushGalleryPreferences()
}

func TestGalleryViewResetIsAvailableWithResultsAndEmptyStates(t *testing.T) {
	for _, size := range [][2]int{{1040, 760}, {760, 620}} {
		for _, state := range []string{"results", "empty", "unloaded"} {
			t.Run(fmt.Sprintf("%s/%dx%d", state, size[0], size[1]), func(t *testing.T) {
				a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: config.Default(), tab: 4, gallery: galleryFixture()}
				p := galleryPreferences{Version: 1, Search: "海边", Prefix: "images/旅行/", Order: "oldest", Period: "custom", From: "2026-01-01", To: "2026-12-31", Page: 1}
				if state == "empty" {
					p.Search = "no matching image"
				} else if state == "unloaded" {
					a.gallery.loaded = false
					a.gallery.objects = nil
				}
				a.gallery.applyViewPreferences(p)
				a.gallery.selected = map[string]bool{"kept-selection": true}
				objects := len(a.gallery.objects)
				tt := ui.NewTester(nativeTestView(a), size[0], size[1])
				tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
				tt.Frame()
				assertGalleryVisible(t, tt, "重置图库视图", size[0], size[1])
				if err := tt.Click("重置图库视图"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				if got := a.gallery.viewPreferences(); got != (galleryPreferences{Version: 1}) || a.gallery.hasViewPreferences() {
					t.Fatal("reset did not clear search, filters, sorting, and pagination", got)
				}
				if len(a.gallery.objects) != objects || !a.gallery.selected["kept-selection"] {
					t.Fatal("reset changed gallery data or selection")
				}
				a.flushGalleryPreferences()
				b := &application{path: a.path}
				if err := b.restoreGalleryPreferences(); err != nil || b.gallery.hasViewPreferences() {
					t.Fatal("reset preferences returned after restart", b.gallery.viewPreferences(), err)
				}
			})
		}
	}
}

func TestGalleryPreferencesCoalesceChangesAndFlushLatestOnQuit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.gallery.json")
	initial := galleryPreferences{Version: 1}
	store := &galleryPreferenceStore{path: path, pending: initial, saved: initial}
	first, latest := initial, initial
	first.Search, latest.Search = "h", "holiday"
	store.queue(first, nil)
	store.queue(latest, nil)
	store.queue(latest, nil)
	if store.revision != 2 {
		t.Fatal("unchanged frames scheduled another save", store.revision)
	}
	if err := store.save(1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a superseded save reached disk", err)
	}
	if err := store.flush(latest); err != nil {
		t.Fatal(err)
	}
	a := &application{path: filepath.Join(filepath.Dir(path), "config.json")}
	if err := a.restoreGalleryPreferences(); err != nil || a.gallery.search != "holiday" {
		t.Fatal("quit did not flush the latest search", a.gallery.search, err)
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	store.queue(latest, nil)
	if err := store.flush(latest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.ModTime().Equal(old) {
		t.Fatal("unchanged preferences rewrote the file", info, err)
	}
}

func TestGalleryPreferencesSaveAutomaticallyAfterSearchChanges(t *testing.T) {
	a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: config.Default(), tab: 4, gallery: galleryFixture()}
	defer a.flushGalleryPreferences()
	tt := ui.NewTester(nativeTestView(a), 760, 620)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	if err := tt.Click("搜索图片"); err != nil {
		t.Fatal(err)
	}
	tt.Type("holiday")
	tt.Frame()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b := &application{path: a.path}
		if err := b.restoreGalleryPreferences(); err != nil {
			t.Fatal(err)
		}
		if b.gallery.search == "holiday" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("search did not persist while the application was open")
}

func TestGalleryPreferencesRejectInvalidFilesAndReportSaveFailure(t *testing.T) {
	for _, data := range []string{
		`{`,
		`{"version":2}`,
		`{"version":1,"order":"unknown"}`,
		`{"version":1,"period":"unknown"}`,
		`{"version":1,"period":"custom","from":"2026-99-99"}`,
		`{"version":1,"period":"custom","from":"2026-10-21","to":"2026-10-20"}`,
	} {
		a := &application{path: filepath.Join(t.TempDir(), "config.json")}
		if err := os.WriteFile(galleryPreferencesPath(a.path), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := a.restoreGalleryPreferences(); err == nil || a.gallery.hasViewPreferences() {
			t.Fatal("invalid saved conditions entered the gallery", data, err)
		}
	}
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &application{path: filepath.Join(blocker, "config.json")}
	a.gallery.search = "holiday"
	a.flushGalleryPreferences()
	if !a.messageError || !strings.Contains(a.message, "保存图库偏好失败") {
		t.Fatal("save failure was not reported", a.message, a.messageError)
	}
}
