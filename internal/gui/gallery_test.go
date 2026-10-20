package gui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/storage"
)

func galleryFixture() galleryState {
	now := time.Date(2026, 10, 20, 11, 30, 0, 0, time.Local)
	g := galleryState{loaded: true, config: config.Default(), selected: map[string]bool{}}
	g.config.Bucket = "photos"
	for i := range 23 {
		key := fmt.Sprintf("images/旅行/海边-%02d.png", i+1)
		g.objects = append(g.objects, storage.Object{Key: key, URL: "https://img.example.com/" + key, Size: 149709, Modified: now.AddDate(0, 0, -i)})
	}
	return g
}
func TestGalleryFilteringAndCrossPageSelection(t *testing.T) {
	g := galleryFixture()
	now := g.objects[0].Modified
	rows, err := g.filtered(now)
	if err != nil || len(rows) != 23 || rows[0].Key != g.objects[len(g.objects)-1].Key {
		t.Fatal(rows, err)
	}
	page, pages := g.pageRows(rows)
	if len(page) != 20 || pages != 2 {
		t.Fatal(len(page), pages)
	}
	g.selectPage(page)
	g.page = 1
	page, _ = g.pageRows(rows)
	g.selectPage(page)
	if len(g.selected) != 23 {
		t.Fatal("selection did not survive pagination")
	}
	g.period = "7d"
	rows, _ = g.filtered(now)
	if len(rows) != 8 {
		t.Fatal(len(rows))
	}
	g.period = "custom"
	g.from = now.Format("2006-01-02")
	g.to = g.from
	rows, err = g.filtered(now)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	g.from = "2026-10-21"
	if _, err = g.filtered(now); err == nil {
		t.Fatal("accepted inverted date range")
	}
	g.from = "bad"
	if _, err = g.filtered(now); err == nil {
		t.Fatal("accepted malformed date")
	}
	g.period = ""
	g.search = "海边-02"
	rows, _ = g.filtered(now)
	if len(rows) == 0 || rows[0].Key != g.objects[1].Key {
		t.Fatal(rows)
	}
	g.search = ""
	g.prefix = "images/旅/"
	rows, _ = g.filtered(now)
	if len(rows) != 0 {
		t.Fatal("prefix crossed directory boundary")
	}
	g.prefix = "/"
	g.objects = append(g.objects, storage.Object{Key: "ROOT.JPG"})
	g.search = "root"
	rows, _ = g.filtered(now)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	g.search = ""
	g.prefix = ""
	g.order = "name-desc"
	rows, _ = g.filtered(now)
	if rows[0].Key != "images/旅行/海边-23.png" {
		t.Fatal(rows[0])
	}
	g.replace(g.objects[:2])
	if len(g.selected) != 2 {
		t.Fatal("refresh did not prune stale selections")
	}
	g.page = 99
	page, pages = g.pageRows(g.objects)
	if g.page != 0 || pages != 1 || len(page) != 2 {
		t.Fatal(g.page, pages, len(page))
	}
	g.remove(map[string]bool{g.objects[0].Key: true})
	if len(g.objects) != 1 || len(g.selected) != 1 {
		t.Fatal("partial deletion removed failed selections")
	}
	keys, labels := g.prefixes()
	if !reflect.DeepEqual(keys, []string{"", "/", "images/", "images/旅行/"}) || len(labels) != len(keys) {
		t.Fatal(keys, labels)
	}
}
func TestGalleryResetCancelsRequestsAndDropsOldBucket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	thumb, stop := context.WithCancel(context.Background())
	a := &application{gallery: galleryFixture()}
	a.gallery.cancel = cancel
	a.gallery.thumbCancel = stop
	a.gallery.generation = 4
	a.resetGallery()
	if ctx.Err() == nil || thumb.Err() == nil || a.gallery.generation != 5 || a.gallery.loaded || len(a.gallery.objects) > 0 || len(a.gallery.selected) > 0 {
		t.Fatal("old bucket state survived reset")
	}
}
func TestGalleryNativeActionsAndDialogs(t *testing.T) {
	for _, size := range [][2]int{{1040, 760}, {760, 620}} {
		for _, dark := range []bool{false, true} {
			for _, collapsed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%v/dark=%t/collapsed=%t", size, dark, collapsed), func(t *testing.T) {
					a := &application{config: config.Default(), tab: 4, sidebarCollapsed: collapsed, gallery: galleryFixture()}
					tt := ui.NewTester(nativeTestView(a), size[0], size[1])
					tt.SetDark(dark)
					tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
					tt.Frame()
					click := func(label string) {
						t.Helper()
						if err := tt.Click(label); err != nil {
							t.Fatal(err)
						}
						tt.Advance(300 * time.Millisecond)
					}
					for _, label := range []string{"搜索图片", "过滤选项", "排序选项", "下一页", "刷新图库"} {
						assertGalleryVisible(t, tt, label, size[0], size[1])
					}
					tt.Key(ui.Cmd, ui.KeyA)
					tt.Frame()
					if len(a.gallery.selected) != 20 {
						t.Fatal("select-page shortcut", len(a.gallery.selected))
					}
					click("下一页")
					tt.Key(ui.Cmd, ui.KeyA)
					tt.Frame()
					if len(a.gallery.selected) != 23 {
						t.Fatal("cross-page selection", len(a.gallery.selected))
					}
					click("删除所选 23 张")
					if !a.gallery.deleteOpen || len(a.gallery.objects) != 23 || len(a.gallery.deleteTargets) != 23 {
						t.Fatal("deletion preceded confirmation")
					}
					assertGalleryVisible(t, tt, "确认删除云端图片", size[0], size[1])
					click("取消")
					click("上一页")
					rows, _ := a.gallery.filtered(time.Now())
					o := rows[0]
					if err := tt.RightClick("选择 " + o.Key); err != nil {
						t.Fatal(err)
					}
					if err := tt.ChooseMenuItem("打开"); err != nil {
						t.Fatal(err)
					}
					tt.Frame()
					for _, label := range []string{"返回图库", "复制链接", "删除图片", "图片操作"} {
						assertGalleryVisible(t, tt, label, size[0], size[1])
					}
					click("图片操作")
					assertGalleryVisible(t, tt, "下载原图", size[0], size[1])
					click("复制 Markdown")
					if tt.Clipboard() != galleryMarkdown([]storage.Object{o}) {
						t.Fatal("copy failed", tt.Clipboard(), a.gallery.optionsOpen, a.message)
					}
					click("图片操作")
					click("重命名")
					click("新图片路径")
					tt.Key(ui.Cmd, ui.KeyA)
					tt.Type("images/new.png")
					tt.Frame()
					a.gallery.renameError = "目标路径已存在，请更换名称后重试。"
					tt.Frame()
					assertGalleryVisible(t, tt, "保存新名称", size[0], size[1])
					if a.gallery.renameKey != "images/new.png" {
						t.Fatal("rename input lost", a.gallery.renameKey)
					}
					click("取消")
					if a.gallery.renameOpen || !a.gallery.previewOpen {
						t.Fatal("cancel lost preview")
					}
					tt.Key(0, ui.KeyEscape)
					tt.Frame()
					if a.gallery.previewOpen {
						t.Fatal("Escape did not return to gallery")
					}
					click("清除选择")
					if len(a.gallery.selected) != 0 {
						t.Fatal("selection did not clear")
					}
					click("过滤选项")
					for _, label := range []string{"筛选目录", "选择日期范围", "筛选中搜索图片"} {
						assertGalleryVisible(t, tt, label, size[0], size[1])
					}
					click("选择日期范围")
					assertGalleryVisible(t, tt, "最近 7 天", size[0], size[1])
					click("最近 7 天")
					if a.gallery.period != "7d" || a.gallery.dateOpen {
						t.Fatal("date preset did not apply")
					}
				})
			}
		}
	}
}
func assertGalleryVisible(t *testing.T, tt *ui.Tester, label string, width, height int) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok || r.W <= 0 || r.H <= 0 || r.X < 0 || r.Y < 0 || r.X+r.W > float32(width)+1 || r.Y+r.H > float32(height)+1 {
		t.Fatalf("%q outside window: %v found=%t", label, r, ok)
	}
}

func guiCloudClient(t *testing.T, handler http.HandlerFunc) *storage.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c := config.Default()
	c.Endpoint = server.URL
	c.Bucket = "photos"
	c.AccessKey = "test"
	c.SecretKey = "secret"
	c.PublicURL = "https://img.example.com"
	client, err := storage.New(c)
	if err != nil {
		t.Fatal(err)
	}
	return client
}
func TestGalleryDownloadPreservesDestinationOnFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			client := guiCloudClient(t, func(w http.ResponseWriter, r *http.Request) {
				if fail {
					w.WriteHeader(403)
					return
				}
				fmt.Fprint(w, "original image bytes")
			})
			dir := t.TempDir()
			dst := filepath.Join(dir, "photo.png")
			if err := os.WriteFile(dst, []byte("existing file"), 0600); err != nil {
				t.Fatal(err)
			}
			err := saveGalleryFile(context.Background(), client, "a.png", dst)
			if (err != nil) != fail {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(dst)
			want := "original image bytes"
			if fail {
				want = "existing file"
			}
			if string(b) != want {
				t.Fatal(string(b))
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 1 {
				t.Fatal("partial download left files", files)
			}
		})
	}
}
func TestGalleryPreviewDecodesAndBoundsData(t *testing.T) {
	var data bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1200, 900))
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	client := guiCloudClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "bad.png") {
			fmt.Fprint(w, "invalid image")
			return
		}
		w.Write(data.Bytes())
	})
	thumb := readGalleryThumb(context.Background(), client, "a.png")
	if thumb.err != nil || thumb.bitmap == nil {
		t.Fatal(thumb.err)
	}
	full := readGalleryImage(context.Background(), client, "a.png", true)
	if full.err != nil || full.bitmap == nil || full.width != 1200 || full.height != 900 {
		t.Fatal("full preview lost original dimensions", full.width, full.height, full.err)
	}
	fullWidth, fullHeight := full.bitmap.Size()
	if fullWidth != 1200 || fullHeight != 900 {
		t.Fatal("full preview reused thumbnail", fullWidth, fullHeight)
	}
	bad := readGalleryThumb(context.Background(), client, "bad.png")
	if bad.err == nil || bad.bitmap != nil {
		t.Fatal("invalid preview accepted")
	}
	var w previewWriter
	// ReaderFrom/WriterTo fast paths must not bypass the preview bound.
	_, err := io.Copy(&w, bytes.NewReader(make([]byte, (32<<20)+1)))
	if err == nil {
		t.Fatal("preview limit bypassed")
	}
}
func TestRenderGallery(t *testing.T) {
	dir := os.Getenv("PICUP_GALLERY_SCREENSHOTS")
	if dir == "" {
		t.Skip("set PICUP_GALLERY_SCREENSHOTS to render gallery")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"normal", "narrow", "dark", "filter", "sort", "calendar", "preview", "preview-options", "rename", "delete", "empty", "initial", "loading", "error"} {
		a := &application{config: config.Default(), tab: 4, gallery: galleryFixture()}
		g := &a.gallery
		// A synthetic image makes native bitmap drawing visible without cloud data.
		img := image.NewRGBA(image.Rect(0, 0, 360, 180))
		for y := range 180 {
			for x := range 360 {
				v := uint8(x * 100 / 360)
				col := color.RGBA{120 + v, 178, 201, 255}
				if y > 90+x/5 {
					col = color.RGBA{34 + v, 90, 115, 255}
				}
				img.SetRGBA(x, y, col)
			}
		}
		bitmap := ui.NewBitmap(img)
		g.thumbs = map[string]galleryThumb{}
		for i, o := range g.objects {
			h := []int{240, 480, 200}[i%3]
			varied := image.NewRGBA(image.Rect(0, 0, 360, h))
			for y := range h {
				for x := range 360 {
					col := color.RGBA{uint8(120 + x*100/360), 178, 201, 255}
					if y > h/2+x/5 {
						col = color.RGBA{uint8(34 + x*100/360), 90, 115, 255}
					}
					varied.SetRGBA(x, y, col)
				}
			}
			g.thumbs[o.Key] = galleryThumb{bitmap: ui.NewBitmap(varied), width: 360, height: h}
		}
		g.selected[g.objects[len(g.objects)-1].Key] = true
		width, height := 1040, 760
		if state == "narrow" {
			width, height = 760, 620
		}
		switch state {
		case "preview", "preview-options", "rename":
			g.preview = &g.objects[0]
			g.previewOpen = true
			g.full = galleryThumb{bitmap: bitmap, width: 360, height: 180}
			g.fullKey = g.preview.Key
			if state == "preview-options" {
				g.optionsKey = g.preview.Key
				g.optionsOpen = true
				g.infoOpen = true
			}
			if state == "rename" {
				g.renameOpen = true
				g.renameKey = "images/new.png"
				g.renameError = "目标路径已存在，请更换名称后重试。"
			}
		case "filter":
			g.filterOpen = true
		case "sort":
			g.sortOpen = true
		case "calendar":
			g.filterOpen = true
			g.dateOpen = true
			g.calendarMonth = time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
		case "delete":
			g.deleteTargets = g.selectedObjects()
			g.deleteOpen = true
		case "empty":
			g.objects = nil
		case "initial":
			g.loaded = false
		case "loading":
			g.loaded = false
			g.busy = true
		case "error":
			g.err = "访问被拒绝，请检查列出对象权限。"
			a.feedback("图库加载失败，可重试", errors.New("AccessDenied"))
		}
		tt := ui.NewTester(nativeTestView(a), width, height)
		tt.SetDark(state == "dark")
		tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
		tt.Frame()
		tt.Advance(400 * time.Millisecond)
		f, err := os.Create(filepath.Join(dir, state+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err = png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}

func TestGalleryPreviewSurvivesRenameOutsideFilter(t *testing.T) {
	g := galleryFixture()
	g.preview = &storage.Object{Key: "images/renamed.png"}
	g.previewOpen = true
	// Renaming can remove the previewed image from the current search results.
	if got := g.thumbnailKeys(nil); !reflect.DeepEqual(got, []string{"images/renamed.png"}) {
		t.Fatal("preview would remain loading", got)
	}
	if got := g.thumbnailKeys([]storage.Object{*g.preview}); len(got) != 1 {
		t.Fatal("preview requested twice", got)
	}
	g.previewOpen = false
	if got := g.thumbnailKeys(nil); len(got) != 0 {
		t.Fatal("closed preview requested data", got)
	}
}

func TestGalleryFiltersWrapOnlyWhenNeeded(t *testing.T) {
	for _, width := range []int{1040, 760} {
		a := &application{config: config.Default(), tab: 4, gallery: galleryFixture()}
		tt := ui.NewTester(nativeTestView(a), width, 760)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
		tt.Frame()
		search, _ := tt.Find("搜索图片")
		filter, _ := tt.Find("过滤选项")
		order, _ := tt.Find("排序选项")
		sameLine := func(x, y ui.Rect) bool { return x.Y < y.Y+y.H && y.Y < x.Y+x.H }
		if !sameLine(search, filter) || !sameLine(filter, order) {
			t.Fatal("compact controls unnecessarily wrap", search, filter, order)
		}
		if order.X+order.W > float32(width)-24 {
			t.Fatal("sort exceeds content", order)
		}
		if _, ok := tt.Find("筛选目录"); ok {
			t.Fatal("directory filter must stay in popover")
		}
	}
}

func TestGalleryReferenceSelectionSearchAndDates(t *testing.T) {
	g := galleryFixture()
	rows, _ := g.filtered(time.Now())
	g.toggleSelection(rows[0].Key, false, rows)
	g.toggleSelection(rows[len(rows)-1].Key, true, rows)
	if len(g.selected) != len(rows) {
		t.Fatal("Shift did not select across pages")
	}
	g.toggleSelection(rows[2].Key, false, rows)
	if g.selected[rows[2].Key] {
		t.Fatal("selected image did not toggle off")
	}
	g.objects = []storage.Object{{Key: "images/holiday.jpg"}, {Key: "images/holidax.jpg"}, {Key: "images/other.jpg"}}
	g.search = "holiday"
	g.order = "name-asc"
	found, _ := g.filtered(time.Now())
	if len(found) != 2 || found[0].Key != "images/holiday.jpg" || found[1].Key != "images/holidax.jpg" {
		t.Fatal("fuzzy match order", found)
	}
	g.setSort("date", false)
	if property, desc := g.sortSettings(); property != "date" || desc {
		t.Fatal(property, desc)
	}
	g.setSort("key", true)
	if property, desc := g.sortSettings(); property != "key" || !desc {
		t.Fatal(property, desc)
	}
	if g.filterCount() != 0 {
		t.Fatal("search counted as property filter")
	}
	g.prefix = "/"
	g.period = "7d"
	if g.filterCount() != 2 {
		t.Fatal(g.filterCount())
	}
	start := time.Date(2026, 10, 12, 0, 0, 0, 0, time.Local)
	end := start.AddDate(0, 0, 8)
	g.chooseDate(start)
	if g.from != "2026-10-12" || g.to != "" {
		t.Fatal(g.from, g.to)
	}
	g.dateOpen = true
	g.chooseDate(end)
	if g.from != "2026-10-12" || g.to != "2026-10-20" || g.dateOpen {
		t.Fatal(g.from, g.to, g.dateOpen)
	}
	g.chooseDate(end)
	g.chooseDate(start)
	if g.from != "2026-10-12" || g.to != "2026-10-20" {
		t.Fatal("reverse range", g.from, g.to)
	}
}

func TestGalleryJustifiedImagesKeepProportions(t *testing.T) {
	for _, width := range []float32{516, 796, 984} {
		g := galleryFixture()
		g.thumbs = map[string]galleryThumb{}
		ratios := []float32{1.5, .75, 1.8}
		for i, o := range g.objects {
			g.thumbs[o.Key] = galleryThumb{width: int(ratios[i%3] * 400), height: 400}
		}
		cells, height := g.justified(g.objects, width)
		if len(cells) != len(g.objects) || height <= 0 {
			t.Fatal(len(cells), height)
		}
		for i, cell := range cells {
			original := g.thumbs[cell.object.Key]
			if cell.x < 0 || cell.y < 0 || cell.x+cell.w > width+.01 || cell.y+cell.h > height+.01 || cell.w <= 0 || cell.h <= 0 {
				t.Fatal("cell out of bounds", cell, width, height)
			}
			if math.Abs(float64(cell.w/cell.h-float32(original.width)/float32(original.height))) > .001 {
				t.Fatal("image was cropped or stretched", cell)
			}
			if i > 0 {
				previous := cells[i-1]
				if previous.y == cell.y && cell.x < previous.x+previous.w+7.9 {
					t.Fatal("overlapping row", previous, cell)
				}
			}
		}
	}
}

func TestGalleryDoubleClickAndSearchSelectAll(t *testing.T) {
	a := &application{config: config.Default(), tab: 4, gallery: galleryFixture()}
	tt := ui.NewTester(nativeTestView(a), 1040, 760)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	rows, _ := a.gallery.filtered(time.Now())
	label := "选择 " + rows[0].Key
	r, ok := tt.Find(label)
	if !ok {
		t.Fatal("missing photo")
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if !a.gallery.selected[rows[0].Key] || a.gallery.previewOpen {
		t.Fatal("single click should select")
	}
	tt.Advance(80 * time.Millisecond)
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if !a.gallery.previewOpen {
		t.Fatal("double click did not open full preview")
	}
	selectedBeforeReturn := a.gallery.selected[rows[0].Key]
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.gallery.previewOpen || a.gallery.selected[rows[0].Key] != selectedBeforeReturn {
		t.Fatal("return lost gallery selection", a.gallery.previewOpen, a.gallery.selected, a.gallery.optionsOpen)
	}
	a.gallery.selected = nil
	tt.Frame()
	if err := tt.Click("搜索图片"); err != nil {
		t.Fatal(err)
	}
	tt.Type("holiday")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Frame()
	if len(a.gallery.selected) != 0 {
		t.Fatal("input select-all also selected images")
	}
}

func TestGallerySortPauseAndCalendarRange(t *testing.T) {
	for _, size := range [][2]int{{1040, 760}, {760, 620}} {
		a := &application{config: config.Default(), tab: 4, gallery: galleryFixture()}
		a.gallery.search = "海边"
		tt := ui.NewTester(nativeTestView(a), size[0], size[1])
		tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
		tt.Frame()
		click := func(label string) {
			t.Helper()
			if err := tt.Click(label); err != nil {
				t.Fatal(err)
			}
			tt.Advance(200 * time.Millisecond)
		}
		click("排序选项")
		old := a.gallery.order
		click("排序顺序切换")
		if a.gallery.order != old {
			t.Fatal("sort changed during relevance search")
		}
		click("关闭排序选项")
		click("过滤选项")
		click("选择日期范围")
		a.gallery.calendarMonth = time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
		tt.Frame()
		for _, label := range []string{"全部（重置）", "最近 1 年", "选择日期 2026-10-12", "选择日期 2026-10-20"} {
			assertGalleryVisible(t, tt, label, size[0], size[1])
		}
		click("选择日期 2026-10-12")
		click("选择日期 2026-10-20")
		if a.gallery.from != "2026-10-12" || a.gallery.to != "2026-10-20" || a.gallery.dateOpen {
			t.Fatal("calendar range did not apply", a.gallery.from, a.gallery.to)
		}
	}
}
