package gui

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
)

func autosaveFixture(t *testing.T) (*application, *ui.Tester) {
	t.Helper()
	cfg := config.Default()
	a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: cfg, quality: float64(cfg.Quality), tab: 2}
	tt := ui.NewTester(nativeTestView(a), 1040, 760)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	return a, tt
}

func TestCredentialAutosaveDebouncesAndSurvivesNavigation(t *testing.T) {
	a, tt := autosaveFixture(t)
	tt.Advance(time.Second)
	if _, err := os.Stat(a.path); !os.IsNotExist(err) {
		t.Fatal("opening settings wrote a configuration")
	}
	label, ok := tt.Find("Secret key")
	if !ok {
		t.Fatal("secret key field is missing")
	}
	// The static label precedes the equally labeled input in the tester.
	tt.ClickAt(label.X+45, label.Y+label.H+23)
	if active, _ := tt.TextInputActive(); !active {
		t.Fatal("secret key input did not receive focus")
	}
	tt.Type("test-secret")
	tt.Advance(400 * time.Millisecond)
	tt.Type("-latest")
	tt.Advance(400 * time.Millisecond)
	if _, err := os.Stat(a.path); !os.IsNotExist(err) {
		t.Fatal("saved before the final edit settled")
	}
	a.openGuide(0)
	tt.Frame()
	tt.Advance(settingsAutosaveDelay)
	saved, err := config.Load(a.path)
	if err != nil || saved.SecretKey != "test-secret-latest" || a.settingsSave.pending {
		t.Fatal("latest credential did not autosave after leaving settings", err)
	}
	if strings.Contains(a.message, "test-secret") || !tt.HasText("设置已保存") {
		t.Fatal("save feedback is missing or exposes credentials")
	}
	if a.uploadDisplayConfig().SecretKey != saved.SecretKey {
		t.Fatal("upload still uses the previous credentials")
	}
}

func TestAutosaveFailureKeepsInputAndOffersRetry(t *testing.T) {
	a, tt := autosaveFixture(t)
	goodPath := a.path
	a.path = t.TempDir() // A directory cannot be replaced by the config file.
	a.config.SecretKey = "kept-secret"
	tt.Frame()
	tt.Advance(settingsAutosaveDelay + time.Millisecond)
	if !a.settingsSave.failed || a.config.SecretKey != "kept-secret" || !tt.HasText("重试") {
		t.Fatal("failed save lost the input or retry action", tt.Texts())
	}
	tt.Advance(10 * time.Second)
	if !tt.HasText("重试") {
		t.Fatal("save failure disappeared before it could be retried")
	}
	a.path = goodPath
	if err := tt.Click("重试"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	saved, err := config.Load(goodPath)
	if err != nil || saved.SecretKey != "kept-secret" || a.settingsSave.failed {
		t.Fatal("retry did not save the retained input", err)
	}
}

func TestAutosaveFlushesLastEditAndShortcutMap(t *testing.T) {
	a, tt := autosaveFixture(t)
	a.config.Shortcuts = map[string]string{"toggle-sidebar": "CmdOrCtrl+Shift+L"}
	tt.Frame()
	if !a.flushSettingsAutosave() {
		t.Fatal("could not flush pending settings")
	}
	a.config.Shortcuts["toggle-sidebar"] = ""
	tt.Frame()
	if !a.settingsSave.pending || !a.flushSettingsAutosave() {
		t.Fatal("editing the existing shortcut map was not observed")
	}
	saved, err := config.Load(a.path)
	if err != nil || saved.Shortcuts["toggle-sidebar"] != "" {
		t.Fatal("shortcut override did not persist", err)
	}
}

func TestEffectiveConfigFlushesPendingEditsBeforeCloudActions(t *testing.T) {
	a, tt := autosaveFixture(t)
	if err := config.Save(a.path, a.config); err != nil {
		t.Fatal(err)
	}
	a.config.Bucket, a.config.SecretKey = "new-bucket", "new-secret"
	tt.Frame()
	effective, err := a.effective()
	if err != nil || effective.Bucket != "new-bucket" || effective.SecretKey != "new-secret" || a.settingsSave.pending {
		t.Fatal("cloud action used settings from before the pending edit", err)
	}
	a.path = t.TempDir()
	a.config.SecretKey = "unsaved-secret"
	if _, err := a.effective(); err == nil || !a.settingsSave.failed {
		t.Fatal("cloud action continued after its new credentials could not be saved")
	}
}

func TestGlobalNotificationsAcrossNavigationAndPreview(t *testing.T) {
	a, tt := autosaveFixture(t)
	a.feedback("已复制图片链接", nil)
	tt.Frame()
	a.navigate(3)
	tt.Frame()
	if !tt.HasText("已复制图片链接") {
		t.Fatal("navigation dismissed the global notification")
	}
	tt.Advance(6 * time.Second)
	if tt.HasText("已复制图片链接") {
		t.Fatal("success notification did not expire")
	}
	a.gallery = galleryFixture()
	a.tab = 4
	a.gallery.preview = &a.gallery.objects[0]
	a.gallery.previewOpen = true
	a.feedback("复制失败", errors.New("clipboard unavailable"))
	tt.Frame()
	r, ok := tt.Find(a.message)
	if !ok || r.X > 80 || r.Y < 550 {
		t.Fatal("preview did not show the global lower-left notification", r)
	}
	if err := tt.Click("关闭提示"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText(a.message) {
		t.Fatal("notification did not close")
	}
}

func TestRenderPreferenceChanges(t *testing.T) {
	dir := os.Getenv("PICUP_SCREENSHOTS")
	if dir == "" {
		t.Skip("set PICUP_SCREENSHOTS to inspect preference changes")
	}
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, name := range []string{"autosave-success", "autosave-error", "shortcuts", "shortcuts-narrow-en", "settings-about", "gallery-toast"} {
		a, _ := autosaveFixture(t)
		width, height := 1040, 760
		switch name {
		case "autosave-success":
			a.feedback("设置已保存", nil)
		case "autosave-error":
			a.feedbackWithAction("保存失败，输入已保留", errors.New("permission denied"), "重试", a.save)
		case "shortcuts", "shortcuts-narrow-en":
			a.settingsTab = 4
			if name == "shortcuts-narrow-en" {
				SetUILanguage("en")
				width, height = 760, 620
				a.sidebarCollapsed = true
			}
		case "settings-about":
			a.settingsTab = 5
		case "gallery-toast":
			a.tab, a.gallery = 4, galleryFixture()
			a.feedback("已复制图片链接", nil)
		}
		tt := ui.NewTester(nativeTestView(a), width, height)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
		tt.Frame()
		tt.Advance(400 * time.Millisecond)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		SetUILanguage("zh-CN")
	}
}
