package gui

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/cli"
	"github.com/knothhe/picup/internal/config"
)

func TestGuidePreservesUnsavedSettingsAndCopiesExamples(t *testing.T) {
	for _, collapsed := range []bool{false, true} {
		t.Run(fmt.Sprintf("collapsed=%t", collapsed), func(t *testing.T) {
			a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: config.Default(), tab: 2, sidebarCollapsed: collapsed}
			a.config.PathTemplate = "unsaved/{date}/{md5}.{extName}"
			tt := ui.NewTester(nativeTestView(a), 760, 620)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
			click := func(text string) {
				t.Helper()
				if err := tt.Click(text); err != nil {
					t.Fatal(err)
				}
				tt.Advance(400 * time.Millisecond)
			}
			click("使用入门")
			if a.tab != 3 || a.guideTab != 0 {
				t.Fatal("settings help did not open the compact guide")
			}
			for _, step := range []string{"01  连接你的存储", "02  检查连接", "03  上传与管理"} {
				if !tt.HasText(step) {
					t.Fatalf("missing step: %s", step)
				}
			}
			if tt.HasText("完整 JSON 配置示例") || tt.HasText("复制服务商 JSON 示例") {
				t.Fatal("the compact guide still exposes long configuration chapters")
			}
			a.openGuide(2)
			tt.Frame()
			for range 16 {
				if r, ok := tt.Find("复制路径模板"); ok && r.Y >= 140 && r.Y+r.H <= 550 {
					break
				}
				tt.Scroll(600, 450, 0, 100)
				tt.Advance(400 * time.Millisecond)
			}
			click("复制路径模板")
			if tt.Clipboard() != "{year}/{month}-{day}-{md5}.{extName}" {
				t.Fatal("wrong full path template copied")
			}
			if a.config.PathTemplate != "unsaved/{date}/{md5}.{extName}" {
				t.Fatal("copy overwrote unsaved settings")
			}
			if _, err := os.Stat(a.path); !os.IsNotExist(err) {
				t.Fatal("opening help or copying wrote configuration")
			}
			click("打开存储设置")
			if a.tab != 2 || a.config.PathTemplate != "unsaved/{date}/{md5}.{extName}" {
				t.Fatal("returning from guide lost unsaved input")
			}
			a.openPathStyleGuide()
			if a.tab != 3 || !a.guideFAQ[2] {
				t.Fatal("contextual connection help did not expand")
			}
			a.openGuide(4)
			if !a.guideFAQ[1] {
				t.Fatal("contextual CLI help did not expand")
			}
		})
	}
}

func TestCompactGuideAndAboutLayouts(t *testing.T) {
	for _, size := range [][2]int{{1040, 760}, {760, 620}} {
		for _, collapsed := range []bool{false, true} {
			for _, dark := range []bool{false, true} {
				for _, tab := range []int{3, 5} {
					t.Run(fmt.Sprintf("%dx%d/collapsed=%t/dark=%t/tab=%d", size[0], size[1], collapsed, dark, tab), func(t *testing.T) {
						a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: config.Default(), tab: tab, sidebarCollapsed: collapsed}
						if tab == 5 {
							a.navigate(5)
						}
						tt := ui.NewTester(nativeTestView(a), size[0], size[1])
						tt.SetDark(dark)
						tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
						tt.Frame()
						label := "打开存储设置"
						if tab == 5 {
							label = "复制配置路径"
							if !tt.HasText("版本 " + cli.Version) {
								t.Fatal("about page is missing application version")
							}
						}
						r, ok := tt.Find(label)
						if !ok || r.X < 0 || r.X+r.W > float32(size[0])+1 || r.Y < 0 || r.Y+r.H > float32(size[1]) {
							t.Fatalf("primary action outside window: %s %v", label, r)
						}
						if tab == 5 {
							if err := tt.Click(label); err != nil {
								t.Fatal(err)
							}
							if tt.Clipboard() != a.path {
								t.Fatal("about page copied the wrong configuration path")
							}
						}
					})
				}
			}
		}
	}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	c := config.Default()
	c.PathTemplate = "images/{date}/{time}/{md5}.{extName}"
	if got := pathPreview(c, at); got != "images/2026-01-02/03-04-05/0123456789abcdef0123456789abcdef.png" {
		t.Fatal(got)
	}
}

func TestRenderGuideViews(t *testing.T) {
	dir := os.Getenv("PICUP_SCREENSHOTS")
	if dir == "" {
		t.Skip("set PICUP_SCREENSHOTS to render native guide screenshots")
	}
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"guide-start", "guide-narrow", "guide-dark", "guide-collapsed", "settings-storage", "settings-general", "settings-general-en", "about", "about-dark", "about-en"} {
		a := &application{path: "/Users/example/.config/picup/config.json", config: config.Default(), tab: 3}
		width, height := 1040, 760
		switch name {
		case "settings-storage":
			a.tab = 2
		case "settings-general", "settings-general-en":
			a.tab, a.settingsTab = 2, 2
		case "about", "about-dark", "about-en":
			a.tab, a.settingsTab = 2, 5
		}
		if name == "guide-narrow" || name == "guide-collapsed" || name == "about-en" || name == "settings-general-en" {
			width, height = 760, 620
		}
		a.sidebarCollapsed = name == "guide-collapsed" || name == "about-dark" || name == "settings-general-en"
		SetUILanguage("zh-CN")
		if name == "about-en" || name == "settings-general-en" {
			SetUILanguage("en")
		}
		tt := ui.NewTester(nativeTestView(a), width, height)
		tt.SetDark(name == "guide-dark" || name == "about-dark")
		tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
		tt.Frame()
		tt.Advance(400 * time.Millisecond)
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err = png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		if err = f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	SetUILanguage("zh-CN")
}

func TestSettingsLanguagePreviewsAndAutosaves(t *testing.T) {
	SetUILanguage("zh-CN")
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: config.Default(), tab: 2, settingsTab: 2, quality: 85}
	a.config.Language = "zh-CN"
	tt := ui.NewTester(nativeTestView(a), 760, 620)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	if err := tt.Click("简体中文"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(400 * time.Millisecond)
	if err := tt.Click("English"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(400 * time.Millisecond)
	if a.config.Language != "en" || !tt.HasText("Shared by all profiles") || !tt.HasText("Saving…") {
		t.Fatal("language selection did not preview immediately", a.config.Language, tt.Texts())
	}
	if _, err := os.Stat(a.path); !os.IsNotExist(err) {
		t.Fatal("preview unexpectedly persisted configuration")
	}
	tt.Advance(settingsAutosaveDelay)
	saved, err := config.Load(a.path)
	if err != nil || saved.Language != "en" {
		t.Fatal("language choice was not persisted", saved.Language, err)
	}
}
