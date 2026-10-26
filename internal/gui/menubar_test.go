package gui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/cli"
	"github.com/knothhe/picup/internal/config"
)

func TestUploadPasteNativeMenuUsesApplicationAction(t *testing.T) {
	a := shortcutTestApplication(t)
	a.tab = 0
	calls := 0
	a.pasteImageAction = func() { calls++ }
	for _, binding := range []string{"CmdOrCtrl+V", "CmdOrCtrl+Alt+P", ""} {
		a.config.Shortcuts = map[string]string{"paste": binding}
		menu := a.applicationMenu()
		paste := menu.ItemByID("paste")
		if paste == nil || paste.Role != "" || paste.Click == nil || paste.Accelerator != binding {
			t.Fatalf("image paste does not own its native accelerator %q: %+v", binding, paste)
		}
		before := calls
		paste.Click(paste, nil)
		if calls != before+1 {
			t.Fatal("native Paste menu did not dispatch exactly one image-paste action")
		}
		if binding != "" {
			matches := 0
			var count func([]*mygo.MenuItem)
			count = func(items []*mygo.MenuItem) {
				for _, item := range items {
					if item.Accelerator == binding {
						matches++
					}
					count(item.Submenu)
				}
			}
			count(menu.Items())
			if matches != 1 {
				t.Fatalf("image paste has %d competing native accelerators", matches)
			}
		}
	}
	// A menu retained during navigation must not paste an image into another page.
	stale := a.applicationMenu().ItemByID("paste")
	a.tab = 2
	before := calls
	stale.Click(stale, nil)
	if calls != before {
		t.Fatal("a stale upload menu pasted from settings")
	}
}

func TestNativePasteMenuFollowsNavigationAndBindingChanges(t *testing.T) {
	previous := mygo.App.Menu()
	t.Cleanup(func() { mygo.App.SetMenu(previous) })
	a := shortcutTestApplication(t)
	a.tab, a.win = 0, &mygo.Window{}
	tt := ui.NewTester(func(c *ui.Context) { a.registerShortcuts(c) }, 200, 100)
	tt.Frame()
	assertPaste := func(role mygo.MenuRole, accelerator string) {
		t.Helper()
		paste := mygo.App.Menu().ItemByID("paste")
		if paste == nil || paste.Role != role || paste.Accelerator != accelerator {
			t.Fatalf("native Paste routing is stale: %+v", paste)
		}
	}
	assertPaste("", "CmdOrCtrl+V")
	for _, tab := range []int{2, 4} {
		a.tab = tab
		tt.Frame()
		assertPaste(mygo.RolePaste, "CmdOrCtrl+V")
	}
	a.tab = 0
	a.config.Shortcuts = map[string]string{"paste": "CmdOrCtrl+Alt+P"}
	tt.Frame()
	assertPaste("", "CmdOrCtrl+Alt+P")
	// Direct changes also occur when preferences are refreshed outside controls.
	a.config.Shortcuts["paste"] = ""
	tt.Frame()
	assertPaste("", "")
	delete(a.config.Shortcuts, "paste")
	tt.Frame()
	assertPaste("", "CmdOrCtrl+V")
	a.tab, a.settingsTab, a.shortcutEditing = 2, 4, "paste"
	tt.Frame()
	assertPaste(mygo.RolePaste, "")
	a.shortcutEditing = ""
	tt.Frame()
	assertPaste(mygo.RolePaste, "CmdOrCtrl+V")
}

func TestNativePasteStillEditsSettingsAndGalleryText(t *testing.T) {
	for _, tab := range []int{2, 4} {
		a := shortcutTestApplication(t)
		a.tab, a.settingsTab = tab, 0
		a.gallery = galleryFixture()
		calls := 0
		a.pasteImageAction = func() { calls++ }
		paste := a.applicationMenu().ItemByID("paste")
		if paste.Role != mygo.RolePaste || paste.Click != nil || paste.Accelerator != "CmdOrCtrl+V" {
			t.Fatal("text editing page replaced the native Paste role", paste)
		}
		tt := ui.NewTester(nativeTestView(a), 1040, 760)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
		label := "Bucket"
		if tab == 4 {
			label = "搜索图片"
		}
		if tab == 2 {
			// A new R2 profile starts on Cloudflare authorization.
			if err := tt.Click("访问密钥"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
		}
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.SetClipboard("photos")
		tt.Command("paste")
		value := a.config.Bucket
		if tab == 4 {
			value = a.gallery.search
		}
		if value != "photos" || calls != 0 {
			t.Fatal("native Paste did not stay in the focused text field", tab, value, calls)
		}
	}
}

func TestUtilityNavigationMovesWithSidebar(t *testing.T) {
	for _, size := range [][2]int{{1040, 760}, {760, 620}} {
		a := &application{config: config.Default()}
		tt := ui.NewTester(nativeTestView(a), size[0], size[1])
		tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
		tt.Frame()
		for _, label := range []string{"指南", "设置"} {
			r, ok := tt.Find(label)
			if !ok || r.Y < float32(size[1])/2 || r.X >= sidebarExpandedWidth {
				t.Fatalf("utility is missing from sidebar footer: %s %v", label, r)
			}
		}
		if tt.HasText("历史") || tt.HasText("picup "+cli.Version+" · S3 / R2") {
			t.Fatal("obsolete history or version footer is visible")
		}
		if err := tt.Click("收起侧边栏"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(400 * time.Millisecond)
		for _, label := range []string{"指南", "设置"} {
			r, ok := tt.Find(label)
			if !ok || r.Y < float32(size[1])/2 || r.X+r.W > sidebarRailWidth {
				t.Fatalf("utility did not stay in the collapsed rail: %s %v", label, r)
			}
		}
		if _, ok := tt.Find("关于"); ok {
			t.Fatal("about remains a top-level utility")
		}
		if err := tt.Click("设置"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(400 * time.Millisecond)
		if err := tt.Click("关于"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(400 * time.Millisecond)
		if a.tab != 2 || a.settingsTab != 5 || !tt.HasText("版本 "+cli.Version) {
			t.Fatal("about navigation failed")
		}
	}
}

func TestQuickMenuValidatesStorageAndKeepsQueue(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: config.Default(), jobs: []*job{{id: 1, name: "queued.png"}}}
	for _, language := range []string{"zh-CN", "en"} {
		SetUILanguage(language)
		launcher := a.trayMenu()
		if len(launcher.Items()) != 1 || launcher.ItemByID("quick-panel").Label != tr("快捷上传") {
			t.Fatal("Linux tray menu did not expose only the quick-panel launcher")
		}
		menu := a.applicationMenu()
		if menu.ItemByID("quick-upload").Label != tr("上传图片…") {
			t.Fatal("menu did not use the selected language")
		}
		for _, id := range []string{"quick-upload", "quick-paste"} {
			a.busy, a.tab = true, 0
			menu.ItemByID(id).Click(nil, nil)
			if a.tab != 0 || len(a.jobs) != 1 {
				t.Fatal("busy action changed the queue")
			}
			a.busy = false
			menu.ItemByID(id).Click(nil, nil)
			if a.tab != 2 || !a.messageError || !strings.Contains(a.message, tr("请先完成存储设置")) || len(a.jobs) != 1 {
				t.Fatal("quick upload did not recover through storage settings", a.message)
			}
		}
	}
}

func TestEnglishPagesAndCancelGalleryFromSettings(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	SetUILanguage("en")
	for _, collapsed := range []bool{false, true} {
		for _, page := range [][2]int{{0, 0}, {2, 0}, {2, 1}, {2, 2}, {2, 3}, {2, 4}, {2, 5}, {3, 0}} {
			a := &application{path: "/tmp/picup/config.json", config: config.Default(), tab: page[0], settingsTab: page[1], sidebarCollapsed: collapsed}
			tt := ui.NewTester(nativeTestView(a), 760, 620)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
			tt.Frame()
			for _, text := range tt.Texts() {
				if strings.IndexFunc(text, func(r rune) bool { return unicode.Is(unicode.Han, r) }) >= 0 {
					t.Fatalf("untranslated English UI on page %v: %s", page, text)
				}
			}
		}
	}
	cancelled := false
	a := &application{config: config.Default(), tab: 2}
	a.gallery.busy = true
	a.gallery.cancel = context.CancelFunc(func() { cancelled = true })
	tt := ui.NewTester(nativeTestView(a), 760, 620)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	if err := tt.Click(tr("取消图库请求")); err != nil {
		t.Fatal(err)
	}
	if !cancelled {
		t.Fatal("settings could not cancel a pending gallery request")
	}
}
