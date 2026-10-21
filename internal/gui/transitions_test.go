package gui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/storage"
)

type nativeMotionLayout struct {
	name          string
	width, height int
	collapsed     bool
}

var nativeMotionLayouts = []nativeMotionLayout{
	{name: "expanded", width: 1040, height: 760},
	{name: "compact", width: 760, height: 620, collapsed: true},
}

func nativeMotionApplication(t *testing.T, layout nativeMotionLayout) *application {
	t.Helper()
	return &application{
		path: filepath.Join(t.TempDir(), "config.json"), config: config.Default(),
		quality: 85, sidebarCollapsed: layout.collapsed, installDir: "/tmp/picup-cli",
	}
}

func nativeMotionTester(a *application, layout nativeMotionLayout, reduced bool) *ui.Tester {
	tt := ui.NewTester(nativeTestView(a), layout.width, layout.height)
	tt.SetPreferences(ui.Preferences{ReduceMotion: reduced, TextScale: 1})
	tt.Frame()
	return tt
}

func nativeMotionBounds(t *testing.T, tt *ui.Tester, label string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok || r.W <= 0 || r.H <= 0 {
		t.Fatalf("new tab content %q is missing or clipped: %v (found=%t)", label, r, ok)
	}
	return r
}

func captureNativeMotion(t *testing.T, tt *ui.Tester, phase string, reduced bool) {
	t.Helper()
	dir := os.Getenv("PICUP_MOTION_SCREENSHOTS")
	if dir == "" || reduced {
		return
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	name := strings.NewReplacer("/", "-", "=", "-").Replace(t.Name()) + "-" + phase + ".png"
	file, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, tt.Image()); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

// Sample the rendered native view, rather than the transition configuration.
// A new tab starts six DIPs below its final position and arrives in 240 ms.
func assertNativeTabMotion(t *testing.T, tt *ui.Tester, label string, reduced bool) {
	t.Helper()
	start := nativeMotionBounds(t, tt, label)
	captureNativeMotion(t, tt, "start", reduced)
	tt.Advance(120 * time.Millisecond)
	tt.Frame()
	middle := nativeMotionBounds(t, tt, label)
	captureNativeMotion(t, tt, "middle", reduced)
	tt.Advance(140 * time.Millisecond)
	tt.Frame()
	end := nativeMotionBounds(t, tt, label)
	captureNativeMotion(t, tt, "end", reduced)
	if reduced {
		if start != end || middle != end {
			t.Fatalf("reduced motion moved %q: start=%v middle=%v end=%v", label, start, middle, end)
		}
		return
	}
	if math.Abs(float64(start.Y-end.Y-6)) > .25 {
		t.Fatalf("%q did not enter from six DIPs below its final position: start=%v middle=%v end=%v", label, start, middle, end)
	}
	if !(end.Y < middle.Y && middle.Y < start.Y) || math.Abs(float64(middle.Y-end.Y-3)) > .5 {
		t.Fatalf("%q did not move smoothly through its midpoint: start=%v middle=%v end=%v", label, start, middle, end)
	}
	if start.X != end.X || start.W != end.W || start.H != end.H || middle.X != end.X || middle.W != end.W || middle.H != end.H {
		t.Fatalf("tab motion changed the layout of %q: start=%v middle=%v end=%v", label, start, middle, end)
	}
	tt.Advance(100 * time.Millisecond)
	tt.Frame()
	if settled := nativeMotionBounds(t, tt, label); settled != end {
		t.Fatalf("%q kept moving after its tab transition ended: end=%v settled=%v", label, end, settled)
	}
}

func TestNativeMainTabTransitions(t *testing.T) {
	unsetUploadDisplayEnv(t)
	for _, layout := range nativeMotionLayouts {
		for _, reduced := range []bool{false, true} {
			for _, target := range []struct {
				name, label string
				tab         int
			}{
				{name: "upload", label: "选择图片", tab: 0},
				{name: "gallery", label: "搜索图片", tab: 4},
				{name: "settings", label: "配置管理", tab: 2},
				{name: "guide", label: "01  连接你的存储", tab: 3},
			} {
				t.Run(fmt.Sprintf("%s/reduced=%t/%s", layout.name, reduced, target.name), func(t *testing.T) {
					a := nativeMotionApplication(t, layout)
					a.tab, a.settingsTab = 3, settingsProfilesTab
					if target.tab == 3 {
						a.tab = 0
					}
					tt := nativeMotionTester(a, layout, reduced)
					a.navigate(target.tab)
					tt.Frame()
					assertNativeTabMotion(t, tt, target.label, reduced)
				})
			}
		}
	}
}

func TestNativeSettingsTabTransitions(t *testing.T) {
	for _, layout := range nativeMotionLayouts {
		for _, reduced := range []bool{false, true} {
			for _, target := range []struct {
				name, label string
				tab         int
			}{
				{name: "storage", label: "当前配置", tab: 0},
				{name: "image-output", label: "图片处理", tab: 1},
				{name: "general", label: "界面", tab: 2},
				{name: "cli", label: "命令行工具", tab: 3},
				{name: "shortcuts", label: "点击键位后按下新的快捷键，修改会自动保存。", tab: 4},
				{name: "about", label: "轻量图片上传工具，连接 S3 / R2，上传并管理你的图片。", tab: 5},
				{name: "profiles", label: "配置管理", tab: settingsProfilesTab},
			} {
				t.Run(fmt.Sprintf("%s/reduced=%t/%s", layout.name, reduced, target.name), func(t *testing.T) {
					a := nativeMotionApplication(t, layout)
					a.tab, a.settingsTab = 2, settingsProfilesTab
					if target.tab == settingsProfilesTab {
						a.settingsTab = 0
					}
					tt := nativeMotionTester(a, layout, reduced)
					if err := tt.Click(settingsLabels[target.tab]); err != nil {
						t.Fatal(err)
					}
					if a.settingsTab != target.tab {
						t.Fatalf("settings navigation selected %d, want %d", a.settingsTab, target.tab)
					}
					assertNativeTabMotion(t, tt, target.label, reduced)
				})
			}
		}
	}
}

func TestNativeTabTransitionsRapidReturn(t *testing.T) {
	for _, layout := range nativeMotionLayouts {
		t.Run(layout.name, func(t *testing.T) {
			a := nativeMotionApplication(t, layout)
			a.tab, a.settingsTab = 2, 1
			tt := nativeMotionTester(a, layout, false)
			before := nativeMotionBounds(t, tt, "默认压缩图片")
			a.settingsTab = 2
			tt.Frame()
			tt.Advance(60 * time.Millisecond)
			a.settingsTab = 1
			tt.Frame()
			if err := tt.Click("关闭窗口后继续运行"); err == nil {
				t.Fatal("a fading settings category still exposes an interactive control")
			}
			if err := tt.Click("默认压缩图片"); err != nil {
				t.Fatal(err)
			}
			if a.config.Compress {
				t.Fatal("the returning settings category did not receive its click")
			}
			tt.Advance(300 * time.Millisecond)
			tt.Frame()
			if after := nativeMotionBounds(t, tt, "默认压缩图片"); after != before {
				t.Fatalf("returning settings content did not settle in its original layout: before=%v after=%v", before, after)
			}
			a.navigate(3)
			tt.Frame()
			tt.Advance(60 * time.Millisecond)
			a.navigate(2)
			tt.Frame()
			if err := tt.Click("打开存储设置"); err == nil {
				t.Fatal("a fading main tab still exposes an interactive control")
			}
			tt.Advance(300 * time.Millisecond)
			tt.Frame()
			if after := nativeMotionBounds(t, tt, "默认压缩图片"); after != before {
				t.Fatalf("returning main tab did not settle in its original layout: before=%v after=%v", before, after)
			}
			// General settings contains segmented controls. Their old paint
			// callbacks must also remain safe when the entire main tab leaves.
			a.settingsTab = 2
			tt.Frame()
			tt.Advance(300 * time.Millisecond)
			tt.Frame()
			general := nativeMotionBounds(t, tt, "界面")
			a.navigate(3)
			tt.Frame()
			tt.Advance(60 * time.Millisecond)
			a.navigate(2)
			tt.Frame()
			if err := tt.Click("打开存储设置"); err == nil {
				t.Fatal("the old guide still receives input after returning to general settings")
			}
			tt.Advance(300 * time.Millisecond)
			tt.Frame()
			if after := nativeMotionBounds(t, tt, "界面"); after != general {
				t.Fatalf("returning general settings changed layout: before=%v after=%v", general, after)
			}
		})
	}
}

func nativeGalleryMotionFixture(t *testing.T, layout nativeMotionLayout, cached bool) (*application, []storage.Object, map[string]galleryThumb) {
	t.Helper()
	a := nativeMotionApplication(t, layout)
	a.tab, a.gallery = 4, galleryFixture()
	g := &a.gallery
	g.filterKey = g.viewFilterKey()
	rows, err := g.filtered(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	thumbs := make(map[string]galleryThumb, len(rows))
	for page, shade := range []color.RGBA{{R: 200, G: 55, B: 45, A: 255}, {R: 35, G: 110, B: 220, A: 255}} {
		img := image.NewRGBA(image.Rect(0, 0, 4, 4))
		for y := range 4 {
			for x := range 4 {
				img.SetRGBA(x, y, shade)
			}
		}
		bitmap := ui.NewBitmap(img)
		for _, object := range rows[page*galleryPageSize : min(len(rows), (page+1)*galleryPageSize)] {
			thumbs[object.Key] = galleryThumb{bitmap: bitmap, width: 300, height: 200}
		}
	}
	first, _ := g.pageRows(rows)
	g.thumbBatch = "ready"
	firstThumbs := make(map[string]galleryThumb, len(first))
	for _, object := range first {
		firstThumbs[object.Key] = thumbs[object.Key]
	}
	if !g.commitThumbnailPage("ready", first, 0, g.filterKey, firstThumbs) {
		t.Fatal("initial gallery page was not committed")
	}
	if cached {
		// Cached navigation takes the real synchronous galleryThumbs path;
		// every object is decoded, so the client performs no storage IO.
		a.win, g.client, g.thumbs = &mygo.Window{}, &storage.Client{}, thumbs
	}
	return a, rows, thumbs
}

func nativeGalleryPhotoLabel(object storage.Object) string {
	return "选择 " + object.Key
}

func nativeGalleryPixel(tt *ui.Tester, bounds ui.Rect) color.RGBA {
	return tt.Image().RGBAAt(int(bounds.X+bounds.W/2), int(bounds.Y+bounds.H/2))
}

func assertNativeGalleryStable(t *testing.T, tt *ui.Tester, label, phase string, before ui.Rect, pixel color.RGBA) {
	t.Helper()
	for i, delay := range []time.Duration{0, 120 * time.Millisecond, 140 * time.Millisecond} {
		tt.Advance(delay)
		tt.Frame()
		if after := nativeMotionBounds(t, tt, label); after != before {
			t.Fatalf("%s moved the retained gallery photo: before=%v after=%v", phase, before, after)
		}
		if after := nativeGalleryPixel(tt, before); after != pixel {
			t.Fatalf("%s changed the retained photo brightness: before=%v after=%v", phase, pixel, after)
		}
		captureNativeMotion(t, tt, fmt.Sprintf("%s-%d", phase, i), false)
	}
}

func TestNativeGalleryCachedPageTransitions(t *testing.T) {
	for _, layout := range nativeMotionLayouts {
		for _, reduced := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reduced=%t", layout.name, reduced), func(t *testing.T) {
				a, rows, _ := nativeGalleryMotionFixture(t, layout, true)
				tt := nativeMotionTester(a, layout, reduced)
				for _, step := range []struct {
					name, action string
					from, to     int
				}{
					{name: "next", action: "下一页", from: 0, to: 1},
					{name: "previous", action: "上一页", from: 1, to: 0},
				} {
					t.Run(step.name, func(t *testing.T) {
						oldPhoto := nativeGalleryPhotoLabel(rows[step.from*galleryPageSize])
						before := nativeMotionBounds(t, tt, oldPhoto)
						pixel := nativeGalleryPixel(tt, before)
						if err := tt.Click(step.action); err != nil {
							t.Fatal(err)
						}
						if a.gallery.page != step.to || a.gallery.thumbLoading {
							t.Fatal("cached navigation did not immediately commit the requested page")
						}
						if _, found := tt.Find(oldPhoto); found {
							t.Fatal("the outgoing page still exposes photo controls")
						}
						if !reduced && nativeGalleryPixel(tt, before) != pixel {
							t.Fatal("the previous photo disappeared before the incoming page faded in")
						}
						label := nativeGalleryPhotoLabel(rows[step.to*galleryPageSize])
						assertNativeTabMotion(t, tt, label, reduced)
						if nativeGalleryPixel(tt, nativeMotionBounds(t, tt, label)) == pixel {
							t.Fatal("the previous photo remained after the page transition ended")
						}
					})
				}
			})
		}
	}
}

func TestNativeGalleryPendingPageTransitions(t *testing.T) {
	for _, layout := range nativeMotionLayouts {
		for _, complete := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/complete=%t", layout.name, complete), func(t *testing.T) {
				a, rows, thumbs := nativeGalleryMotionFixture(t, layout, false)
				g := &a.gallery
				tt := nativeMotionTester(a, layout, false)
				oldPhoto := nativeGalleryPhotoLabel(rows[0])
				before := nativeMotionBounds(t, tt, oldPhoto)
				pixel := nativeGalleryPixel(tt, before)
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				g.page, g.thumbBatch, g.thumbLoading, g.thumbCancel = 1, "pending", true, cancel
				tt.Frame()
				assertNativeGalleryStable(t, tt, oldPhoto, "pending", before, pixel)
				newPhoto := nativeGalleryPhotoLabel(rows[galleryPageSize])
				if _, found := tt.Find(newPhoto); found {
					t.Fatal("the pending page exposed photos before the complete batch was committed")
				}
				if !complete {
					if err := tt.Click("取消请求"); err != nil {
						t.Fatal(err)
					}
					if ctx.Err() == nil || g.page != 0 || g.thumbLoading {
						t.Fatal("cancellation did not restore the retained gallery page")
					}
					assertNativeGalleryStable(t, tt, oldPhoto, "cancelled", before, pixel)
					return
				}
				next := rows[galleryPageSize:]
				if !g.commitThumbnailPage("pending", next, 1, g.filterKey, thumbs) {
					t.Fatal("the complete pending gallery page was not committed")
				}
				tt.Frame()
				if nativeGalleryPixel(tt, before) != pixel {
					t.Fatal("commit removed the retained photo before the new page faded in")
				}
				assertNativeTabMotion(t, tt, newPhoto, false)
			})
		}
	}
}

func TestNativeGalleryPageTransitionsRapidReturn(t *testing.T) {
	layout := nativeMotionLayouts[1]
	a, rows, _ := nativeGalleryMotionFixture(t, layout, true)
	tt := nativeMotionTester(a, layout, false)
	label := nativeGalleryPhotoLabel(rows[0])
	before := nativeMotionBounds(t, tt, label)
	if err := tt.Click("下一页"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(60 * time.Millisecond)
	if err := tt.Click("上一页"); err != nil {
		t.Fatal(err)
	}
	if a.gallery.page != 0 {
		t.Fatal("rapid pagination did not return to the first page")
	}
	if err := tt.Click(nativeGalleryPhotoLabel(rows[galleryPageSize])); err == nil {
		t.Fatal("the outgoing page still exposes interactive photos after a rapid return")
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	if !a.gallery.selected[rows[0].Key] {
		t.Fatal("the returning page did not receive the photo selection")
	}
	captureNativeMotion(t, tt, "return-start", false)
	tt.Advance(120 * time.Millisecond)
	tt.Frame()
	captureNativeMotion(t, tt, "return-middle", false)
	tt.Advance(180 * time.Millisecond)
	tt.Frame()
	captureNativeMotion(t, tt, "return-end", false)
	if after := nativeMotionBounds(t, tt, label); after != before {
		t.Fatalf("rapidly returning photos did not settle in their original layout: before=%v after=%v", before, after)
	}
}
