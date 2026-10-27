package gui

import (
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/service"
)

func TestMain(m *testing.M) {
	preferredUILanguages = func() []string { return []string{"zh-CN"} }
	SetUILanguage("zh-CN")
	os.Exit(m.Run())
}

func TestFeedbackStaysVisible(t *testing.T) {
	for _, size := range [][2]int{{1040, 760}, {760, 620}} {
		for _, collapsed := range []bool{false, true} {
			for _, tab := range []int{0, 2, 3, 4, 5} {
				t.Run(fmt.Sprintf("%dx%d/collapsed=%t/tab=%d", size[0], size[1], collapsed, tab), func(t *testing.T) {
					a := &application{config: config.Default(), tab: tab, sidebarCollapsed: collapsed}
					if tab == 5 {
						a.navigate(5)
					}
					tt := ui.NewTester(nativeTestView(a), size[0], size[1])
					tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
					for _, result := range []struct {
						message string
						err     error
					}{
						{message: "正在写入并清理临时测试对象…"},
						{message: "上传成功，测试对象已清理"},
						{message: "配置检查失败", err: errors.New("bucket is required")},
						{message: "连接测试失败", err: errors.New("upload succeeded but cleanup failed for images/.picup-test-0123456789abcdef01234567: operation error S3: DeleteObject, https response error StatusCode: 403, AccessDenied: Access Denied")},
					} {
						a.feedback(result.message, result.err)
						tt.Frame()
						r, ok := tt.Find(a.message)
						if !ok || r.W <= 0 || r.H <= 0 || r.X < 0 || r.Y < 48 || r.X+r.W > float32(size[0]) || r.Y+r.H > float32(size[1]) {
							t.Fatalf("feedback %q is outside the window: %v (found=%t)", a.message, r, ok)
						}
						if r.X > 80 || r.Y+r.H < float32(size[1])-190 {
							t.Fatalf("feedback is not at the lower left: %v", r)
						}
					}
				})
			}
		}
	}
}

// Match the native shell in headless renders: content runs behind the
// titlebar, which overlays the page title and actions.
func nativeTestView(a *application) func(*ui.Context) {
	return func(c *ui.Context) {
		c.SetTheme(palette(c))
		ui.Box(c).Fill().Background(c.Theme().Background).Children(func() {
			ui.Column(c).Fill().Children(func() { a.view(c) })
			ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(48).Padding(6, 8, 6, headerInset(c)).AlignItems(ui.Center).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).FillHeight().AlignItems(ui.Center).Children(func() { a.header(c) })
				a.headerActions(c)
			})
		})
	}
}

func TestNativeViewsAndSharedSave(t *testing.T) {
	for _, size := range [][2]int{{960, 710}, {760, 570}} {
		for _, dark := range []bool{false, true} {
			a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: config.Default(), quality: 85, installDir: "/tmp/picup-cli"}
			tt := ui.NewTester(nativeTestView(a), size[0], size[1])

			tt.SetDark(dark)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
			tt.Frame()
			if !tt.HasText("选择图片") || !tt.HasText("从剪贴板粘贴") {
				t.Fatal("missing upload actions", tt.Texts())
			}
			openProcessingOptions(t, tt)
			if e := tt.Click("压缩图片"); e != nil {
				t.Fatal(e)
			}
			if a.config.Compress {
				t.Fatal("switch did not update settings")
			}
			tt.Advance(settingsAutosaveDelay + time.Millisecond)
			SetUILanguage("zh-CN")
			saved, e := config.Load(a.path)
			if e != nil || saved.Compress {
				t.Fatal("GUI preferences not shared with CLI", e)
			}
			a.tab = 2
			a.settingsTab = 1
			tt.Frame()
			if _, exists := tt.Find("压缩图片"); exists {
				t.Fatal("compression controls remain in settings")
			}
			a.settingsTab = 3
			tt.Frame()
			if !tt.HasText("安装 / 更新 CLI") {
				t.Fatal("missing install action")
			}
			a.jobs = []*job{{id: 1, name: "photo.png", status: "已上传", result: &service.Result{Name: "photo.png", URL: "https://img.example.com/photo.png"}}}
			a.tab = 0
			tt.Frame()
			if e := tt.Click("复制链接"); e != nil {
				t.Fatal(e)
			}
			if tt.Clipboard() != "https://img.example.com/photo.png" {
				t.Fatal("copy failed")
			}
			if e := tt.Click("清空"); e != nil {
				t.Fatal(e)
			}
			if len(a.jobs) != 0 {
				t.Fatal("clear failed")
			}
		}
	}
}

func TestNavigationAndSidebar(t *testing.T) {
	for _, reducedMotion := range []bool{false, true} {
		a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: config.Default(), quality: 85}
		tt := ui.NewTester(nativeTestView(a), 760, 620)
		tt.SetPreferences(ui.Preferences{ReduceMotion: reducedMotion, TextScale: 1})
		tt.Frame()
		click := func(label string) {
			t.Helper()
			if err := tt.Click(label); err != nil {
				t.Fatal(err)
			}
			tt.Advance(400 * time.Millisecond)
		}
		click("展开输出设置")
		click("压缩图片")
		// Let the save notice leave the lower-left navigation before clicking
		// another page; this test exercises navigation rather than toast input.
		tt.Advance(settingsAutosaveDelay + 6*time.Second)
		click("图库")
		click("设置")
		if a.tab != 2 || a.previousTab != 4 {
			t.Fatal("settings did not retain the previous page")
		}
		click("图片与输出")
		click("收起侧边栏")
		if !a.sidebarCollapsed || a.settingsTab != 1 || a.config.Compress {
			t.Fatal("sidebar collapse lost settings state")
		}
		tt.Advance(settingsAutosaveDelay)
		SetUILanguage("zh-CN")
		saved, err := config.Load(a.path)
		if err != nil || saved.Compress {
			t.Fatal("collapsed settings did not save", err)
		}
		click("返回图库")
		if a.tab != 4 {
			t.Fatal("settings back button did not return to gallery")
		}
		tt.Key(ui.Cmd, ui.KeyComma)
		tt.Advance(400 * time.Millisecond)
		if a.tab != 2 {
			t.Fatal("settings shortcut failed")
		}
		tt.Key(ui.Cmd, ui.KeyB)
		tt.Advance(400 * time.Millisecond)
		if a.sidebarCollapsed {
			t.Fatal("sidebar shortcut failed")
		}
		click("通用")
		if slices.Contains(tt.Texts(), "侧边栏") || tt.HasText("⌘B 切换。") {
			t.Fatal("general settings still exposes a sidebar toggle")
		}
		click("收起侧边栏")
		click("展开侧边栏")
		click("CLI")
		if !tt.HasText("安装 / 更新 CLI") {
			t.Fatal("CLI settings missing after navigation")
		}
	}
}

func TestSidebarContentMotion(t *testing.T) {
	a := &application{config: config.Default()}
	tt := ui.NewTester(nativeTestView(a), 1040, 760)
	before, _ := tt.Find("选择图片")
	if err := tt.Click("收起侧边栏"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(160 * time.Millisecond)
	middle, _ := tt.Find("选择图片")
	tt.Advance(240 * time.Millisecond)
	after, _ := tt.Find("选择图片")
	if !(after.X < middle.X && middle.X < before.X) {
		t.Fatalf("content did not move with sidebar: before=%v middle=%v after=%v", before, middle, after)
	}
}

// Optional artifact output makes the rendered native view inspectable without
// putting generated screenshots in the repository.
func TestRenderNativeViews(t *testing.T) {
	dir := os.Getenv("PICUP_SCREENSHOTS")
	if dir == "" {
		t.Skip("set PICUP_SCREENSHOTS to render native screenshots")
	}
	os.MkdirAll(dir, 0700)
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: config.Default(), quality: 85, installDir: "/tmp/picup-cli"}
	for _, name := range []string{"upload", "settings", "settings-success", "settings-error", "images", "general", "cli", "collapsed", "narrow", "dark", "about", "guide", "english", "english-collapsed", "english-about", "english-settings"} {
		a.tab, a.settingsTab, a.sidebarCollapsed = 0, 0, false
		a.message, a.messageError = "", false
		SetUILanguage("zh-CN")
		width, height := 1040, 760
		switch name {
		case "settings":
			a.tab = 2
		case "settings-success":
			a.tab = 2
			a.feedback("上传成功，测试对象已清理", nil)
		case "settings-error":
			a.tab = 2
			width, height = 760, 620
			a.feedback("连接测试失败", errors.New("AccessDenied: check credentials and permissions"))
		case "cli":
			a.tab, a.settingsTab = 2, 3
		case "images":
			a.tab, a.settingsTab = 2, 1
		case "general":
			a.tab, a.settingsTab = 2, 2
		case "collapsed":
			a.tab, a.settingsTab, a.sidebarCollapsed = 2, 1, true
		case "narrow":
			a.tab, a.settingsTab = 2, 1
			width, height = 760, 620
		case "about":
			a.tab, a.settingsTab = 2, 5
		case "guide":
			a.tab = 3
		case "english", "english-collapsed", "english-about", "english-settings":
			SetUILanguage("en")
			width, height = 760, 620
			if name == "english-collapsed" {
				a.sidebarCollapsed = true
			}
			if name == "english-about" {
				a.tab, a.settingsTab = 2, 5
			}
			if name == "english-settings" {
				a.tab, a.settingsTab = 2, 2
			}
		}
		tt := ui.NewTester(nativeTestView(a), width, height)
		tt.SetDark(name == "dark" || name == "history-dark")
		tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
		tt.Frame()
		tt.Advance(400 * time.Millisecond)
		f, e := os.Create(filepath.Join(dir, name+".png"))
		if e != nil {
			t.Fatal(e)
		}
		if e = png.Encode(f, tt.Image()); e != nil {
			t.Fatal(e)
		}
		f.Close()

	}
}
