package gui

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
)

func TestWindowSettingsNavigationTogglesAndAutosave(t *testing.T) {
	a, tt := autosaveFixture(t)
	a.lifecycle.setOpenAtLogin = func(bool) error { return nil }
	if err := tt.Click("通用"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(400 * time.Millisecond)
	if a.settingsTab != 2 || slices.Contains(settingsLabels, "窗口与启动") {
		t.Fatal("window settings must be a group in General")
	}
	if a.config.TrayClickAction != config.TrayClickQuickPanel {
		t.Fatal("tray clicks must default to the quick panel")
	}
	click := func(label string) {
		t.Helper()
		r, ok := tt.Find(label)
		if !ok {
			t.Fatal("missing setting", label)
		}
		if r.Y < 150 || r.Y+r.H > 690 {
			tt.Scroll(950, 400, 0, r.Y-350)
		}
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	for _, label := range []string{"关闭窗口后继续运行", "轻量模式", "开机自启"} {
		click(label)
	}
	if a.config.CloseToTray || !a.config.LightweightMode || !a.config.LaunchAtLogin {
		t.Fatal("window setting switches did not update")
	}
	wantDock := config.DockIconShow
	if runtime.GOOS == "darwin" {
		click("跟随窗口")
		wantDock = config.DockIconFollowWindow
	}
	click("打开主窗口")
	click("打开快捷面板")
	if a.config.TrayClickAction != config.TrayClickQuickPanel {
		t.Fatal("quick-panel segment did not restore the default behavior")
	}
	click("打开主窗口")
	tt.Advance(settingsAutosaveDelay + time.Millisecond)
	saved, err := config.Load(a.path)
	if err != nil || saved.CloseToTray || !saved.LightweightMode || !saved.LaunchAtLogin || saved.DockIcon != wantDock || saved.TrayClickAction != config.TrayClickMainWindow {
		t.Fatal("window preferences did not autosave", err)
	}
}

func TestWindowSettingsLayoutAndTranslations(t *testing.T) {
	previous := uiLanguage()
	t.Cleanup(func() { SetUILanguage(previous) })
	for _, language := range []string{"zh-CN", "en"} {
		for _, width := range []int{1040, 760} {
			for _, collapsed := range []bool{false, true} {
				name := fmt.Sprintf("%s-%d-collapsed-%t", language, width, collapsed)
				t.Run(name, func(t *testing.T) {
					SetUILanguage(language)
					cfg := config.Default()
					a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: cfg, quality: 85, tab: 2, settingsTab: 2, sidebarCollapsed: collapsed}
					tt := ui.NewTester(nativeTestView(a), width, 620)
					tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
					tt.SetDark(collapsed)
					tt.Frame()
					for _, label := range []string{"窗口与启动", "关闭窗口后继续运行", "Dock 图标", "点击托盘图标", "轻量模式", "开机自启", "展示", "隐藏", "跟随窗口", "打开快捷面板", "打开主窗口"} {
						r, ok := tt.Find(tr(label))
						for attempt := 0; ok && attempt < 4 && (r.W == 0 || r.H == 0 || r.Y < 140 || r.Y+r.H > 550); attempt++ {
							tt.Scroll(float32(width)-60, 400, 0, r.Y-350)
							tt.Advance(250 * time.Millisecond)
							r, ok = tt.Find(tr(label))
						}
						if !ok || r.X < 0 || r.W <= 0 || r.X+r.W > float32(width)+1 {
							t.Fatalf("window setting %s exceeds available width: %v", label, r)
						}
					}
					if language == "en" {
						for _, text := range tt.Texts() {
							if strings.IndexFunc(text, func(r rune) bool { return unicode.Is(unicode.Han, r) }) >= 0 {
								t.Fatal("untranslated window preference", text)
							}
						}
					}
					if dir := os.Getenv("PICUP_SCREENSHOTS"); dir != "" {
						tt.Scroll(float32(width)-60, 400, 0, -10000)
						tt.Advance(250 * time.Millisecond)
						group, _ := tt.Find(tr("窗口与启动"))
						top := float32(155)
						if collapsed {
							top = 200
						}
						tt.Scroll(float32(width)-60, 400, 0, group.Y-top)
						tt.Advance(250 * time.Millisecond)
						if err := os.MkdirAll(dir, 0700); err != nil {
							t.Fatal(err)
						}
						f, err := os.Create(filepath.Join(dir, "windows-"+name+".png"))
						if err != nil {
							t.Fatal(err)
						}
						defer f.Close()
						if err := png.Encode(f, tt.Image()); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		}
	}
}
