package gui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
)

func shortcutTestApplication(t *testing.T) *application {
	t.Helper()
	return &application{path: filepath.Join(t.TempDir(), "config.json"), config: config.Default(), quality: 85, tab: 2, settingsTab: 4}
}

func TestSidebarDefaultUsesCommandB(t *testing.T) {
	a := shortcutTestApplication(t)
	if a.shortcutBinding("sidebar") != (shortcutBinding{mods: ui.Cmd, key: ui.KeyB}) {
		t.Fatal("sidebar default is not Command B")
	}
	a.config.Shortcuts = map[string]string{"sidebar": "CmdOrCtrl+Alt+B"}
	if a.shortcutBinding("sidebar") != (shortcutBinding{mods: ui.Cmd | ui.Alt, key: ui.KeyB}) {
		t.Fatal("new default replaced an explicit user shortcut")
	}
}

func TestImagePasteShortcutDispatchAndDisable(t *testing.T) {
	a := shortcutTestApplication(t)
	a.tab = 0
	calls := 0
	a.pasteImageAction = func() { calls++ }
	tt := ui.NewTester(nativeTestView(a), 1040, 760)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyV)
	if calls != 1 {
		t.Fatal("default image paste shortcut did not dispatch once", calls)
	}
	if !a.setShortcut("paste", shortcutBinding{mods: ui.Cmd | ui.Alt, key: ui.KeyP}) {
		t.Fatal(a.shortcutError)
	}
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyV)
	if calls != 1 {
		t.Fatal("old image paste shortcut survived customization", calls)
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyP)
	if calls != 2 {
		t.Fatal("custom image paste shortcut did not dispatch once", calls)
	}
	if !a.setShortcut("paste", shortcutBinding{}) {
		t.Fatal(a.shortcutError)
	}
	tt.Frame()
	tt.Key(ui.Cmd|ui.Alt, ui.KeyP)
	tt.Key(ui.Cmd, ui.KeyV)
	if calls != 2 {
		t.Fatal("disabled image paste shortcut still dispatches", calls)
	}
}

func TestShortcutOverridesPersistAndRestore(t *testing.T) {
	a := shortcutTestApplication(t)
	custom := shortcutBinding{mods: ui.Cmd | ui.Alt, key: ui.KeyB}
	if !a.setShortcut("sidebar", custom) || !a.setShortcut("quick-upload", shortcutBinding{}) {
		t.Fatal(a.shortcutError)
	}
	if err := config.Save(a.path, a.config); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(a.path)
	if err != nil {
		t.Fatal(err)
	}
	b := &application{config: loaded}
	if b.shortcutBinding("sidebar") != custom || b.shortcutBinding("quick-upload").key != ui.KeyUnknown {
		t.Fatal("custom and cleared shortcuts did not survive restart", loaded.Shortcuts)
	}
	defaults, _ := parseShortcut("CmdOrCtrl+B")
	if !b.setShortcut("sidebar", defaults) {
		t.Fatal(b.shortcutError)
	}
	if _, exists := b.config.Shortcuts["sidebar"]; exists {
		t.Fatal("restoring a default should remove its override")
	}
}

func TestShortcutConflictsRespectScopeAndTextEditing(t *testing.T) {
	a := shortcutTestApplication(t)
	previous := a.shortcutBinding("sidebar")
	if a.setShortcut("sidebar", a.shortcutBinding("settings")) || a.shortcutBinding("sidebar") != previous || !strings.Contains(a.shortcutError, "打开设置") {
		t.Fatal("conflict changed the old binding", a.shortcutError)
	}
	if a.setShortcut("quick-upload", shortcutBinding{mods: ui.Cmd, key: ui.KeyA}) {
		t.Fatal("native menu must not intercept text Select All")
	}
	if a.setShortcut("settings", shortcutBinding{key: ui.KeyLeft}) {
		t.Fatal("global command accepted a plain caret key")
	}
	if !a.setShortcut("paste", shortcutBinding{mods: ui.Cmd | ui.Shift, key: ui.KeyA}) || !a.setShortcut("gallery-select-all", shortcutBinding{mods: ui.Cmd | ui.Shift, key: ui.KeyA}) {
		t.Fatal("independent upload and gallery scopes should share a binding", a.shortcutError)
	}
	if a.setShortcut("sidebar", shortcutBinding{mods: ui.Cmd | ui.Shift, key: ui.KeyA}) {
		t.Fatal("global scope should conflict with scoped commands")
	}
	if !a.setShortcut("quick-paste", a.shortcutBinding("quick-paste")) {
		t.Fatal("default quick-paste binding was rejected", a.shortcutError)
	}
}

func TestShortcutRecorderAndDispatch(t *testing.T) {
	a := shortcutTestApplication(t)
	tt := ui.NewTester(nativeTestView(a), 1040, 760)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	if err := tt.Click("修改 切换侧边栏 的快捷键"); err != nil {
		t.Fatal(err)
	}
	if a.shortcutEditing != "sidebar" {
		t.Fatal("shortcut recorder did not start")
	}
	tt.Key(ui.Cmd, ui.KeyComma)
	if a.shortcutEditing != "sidebar" || a.shortcutError == "" {
		t.Fatal("recorder did not preserve the binding after conflict", a.shortcutError)
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyB)
	if a.shortcutEditing != "" || a.shortcutBinding("sidebar") != (shortcutBinding{mods: ui.Cmd | ui.Alt, key: ui.KeyB}) {
		t.Fatal("recorded shortcut was not applied", a.config.Shortcuts, a.shortcutError)
	}
	a.tab = 0
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyB)
	if a.sidebarCollapsed {
		t.Fatal("old shortcut still dispatched")
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyB)
	if !a.sidebarCollapsed {
		t.Fatal("custom shortcut did not dispatch", a.shortcutEditing, a.shortcutBinding("sidebar"), a.tab, tt.Texts())
	}
	a.tab, a.settingsTab, a.sidebarCollapsed = 2, 4, false
	tt.Frame()
	if err := tt.Click("修改 打开设置 的快捷键"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEscape)
	if a.shortcutEditing != "" || a.shortcutBinding("settings") != (shortcutBinding{mods: ui.Cmd, key: ui.KeyComma}) {
		t.Fatal("Escape did not cancel recording")
	}
	if err := tt.Click("清除 切换侧边栏 的快捷键"); err != nil {
		t.Fatal(err)
	}
	if a.shortcutBinding("sidebar").key != ui.KeyUnknown {
		t.Fatal("clear button did not disable the command")
	}
	if err := tt.Click("恢复 切换侧边栏 的默认快捷键"); err != nil {
		t.Fatal(err)
	}
	if a.shortcutBinding("sidebar") != (shortcutBinding{mods: ui.Cmd, key: ui.KeyB}) {
		t.Fatal("single restore did not restore the default")
	}
	if !a.setShortcut("sidebar", shortcutBinding{mods: ui.Cmd | ui.Alt, key: ui.KeyB}) {
		t.Fatal(a.shortcutError)
	}
	tt.Frame()
	if err := tt.Click("恢复全部默认"); err != nil {
		t.Fatal(err)
	}
	if len(a.config.Shortcuts) != 0 || a.shortcutBinding("sidebar") != (shortcutBinding{mods: ui.Cmd, key: ui.KeyB}) {
		t.Fatal("reset all left an override")
	}
}

func TestNativeShortcutRolesCanBeCleared(t *testing.T) {
	a := shortcutTestApplication(t)
	for _, id := range []string{"quit", "settings", "quick-upload", "quick-paste", "window-close", "window-minimize", "window-hide", "window-hide-others"} {
		a.config.Shortcuts = map[string]string{id: ""}
		menu := a.applicationMenu()
		if item := menu.ItemByID(id); item == nil || item.Accelerator != "" {
			t.Fatalf("native role restored a cleared binding %s: %+v", id, item)
		}
		a.config.Shortcuts[id] = "CmdOrCtrl+Alt+F8"
		if item := a.applicationMenu().ItemByID(id); item.Accelerator != "CmdOrCtrl+Alt+F8" {
			t.Fatalf("custom native accelerator did not propagate for %s: %s", id, item.Accelerator)
		}
	}
	a.shortcutEditing = "gallery-select-all"
	var assertNoAccelerators func([]*mygo.MenuItem)
	assertNoAccelerators = func(items []*mygo.MenuItem) {
		for _, item := range items {
			if item.Accelerator != "" {
				t.Fatalf("menu accelerator prevents recording: %s", item.Accelerator)
			}
			assertNoAccelerators(item.Submenu)
		}
	}
	assertNoAccelerators(a.applicationMenu().Items())
	a.shortcutEditing = ""
	if a.nativeShortcut("gallery-select-all") != "" || a.nativeShortcut("preview-next") != "" {
		t.Fatal("scoped editor keys must stay out of native accelerators")
	}
}

func TestGalleryShortcutLeavesTextSelectionToEditor(t *testing.T) {
	a := shortcutTestApplication(t)
	a.tab, a.gallery = 4, galleryFixture()
	tt := ui.NewTester(nativeTestView(a), 1040, 760)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	if err := tt.Click("搜索图片"); err != nil {
		t.Fatal(err)
	}
	tt.Type("海边")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("旅行")
	if a.gallery.search != "旅行" || len(a.gallery.selected) != 0 {
		t.Fatal("gallery Select All intercepted the editor", a.gallery.search, a.gallery.selected)
	}
}

func TestPreviewCustomNavigationUsesFilteredOrder(t *testing.T) {
	a := shortcutTestApplication(t)
	a.tab, a.gallery = 4, galleryFixture()
	g := &a.gallery
	g.order = "name-asc"
	g.openPhoto(g.objects[0])
	custom := shortcutBinding{mods: ui.Cmd | ui.Shift, key: ui.KeyN}
	if !a.setShortcut("preview-next", custom) {
		t.Fatal(a.shortcutError)
	}
	tt := ui.NewTester(nativeTestView(a), 1040, 760)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	tt.Key(0, ui.KeyRight)
	if g.preview.Key != g.objects[0].Key {
		t.Fatal("old preview shortcut still navigates")
	}
	tt.Key(custom.mods, custom.key)
	if g.preview.Key != g.objects[1].Key {
		t.Fatal("custom preview shortcut did not navigate")
	}
	tt.Key(0, ui.KeyLeft)
	if g.preview.Key != g.objects[0].Key || g.movePreview(-1, time.Now()) {
		t.Fatal("previous image did not respect the first filtered item")
	}
	tt.Key(0, ui.KeyEscape)
	if g.previewOpen {
		t.Fatal("preview close shortcut did not close")
	}
}
