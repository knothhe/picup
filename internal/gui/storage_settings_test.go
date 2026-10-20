package gui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
)

func TestStorageRegionEditStaysInCurrentProfileWithoutTestingConnection(t *testing.T) {
	a, archiveID := profileFixture(t)
	a.tab, a.settingsTab = 2, 0
	tt := ui.NewTester(nativeTestView(a), 1040, 760)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	label, ok := tt.Find("Region")
	if !ok {
		t.Fatal("Region is missing from the main R2 storage form")
	}
	tt.ClickAt(label.X+45, label.Y+label.H+23)
	if active, _ := tt.TextInputActive(); !active {
		t.Fatal("Region input did not receive focus")
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("custom-region")
	tt.Advance(settingsAutosaveDelay + time.Millisecond)
	if a.config.Region != "custom-region" || a.busy || a.uploading || a.gallery.operation {
		t.Fatal("editing the storage form changed the target or started a cloud test")
	}
	saved, err := config.Load(a.path)
	if err != nil || saved.ActiveProfile != "default" || saved.Region != "custom-region" {
		t.Fatal("Region did not autosave into the current profile", err)
	}
	if err := saved.SwitchProfile(archiveID); err != nil || saved.Region != "auto" {
		t.Fatal("editing Region changed another profile", err)
	}
	if err := tt.Click("管理配置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.tab != 2 || a.settingsTab != settingsProfilesTab || a.busy || a.uploading {
		t.Fatal("Manage profiles did not navigate without starting a connection test")
	}
}

func TestStorageAdvancedSettingsAutosaveWithoutConnectionTest(t *testing.T) {
	a, _ := profileFixture(t)
	a.tab, a.settingsTab = 2, 0
	tt := ui.NewTester(nativeTestView(a), 1040, 760)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	tt.Scroll(900, 500, 0, 1200)
	if err := tt.Click("高级选项"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tt.Scroll(900, 500, 0, 400)
	label, ok := tt.Find("Session token")
	if !ok {
		t.Fatal("advanced settings did not reveal the temporary credential input")
	}
	tt.ClickAt(label.X+45, label.Y+label.H+23)
	if active, _ := tt.TextInputActive(); !active {
		t.Fatal("Session token input did not receive focus")
	}
	tt.Type("temporary-token")
	tt.Advance(settingsAutosaveDelay + time.Millisecond)
	saved, err := config.Load(a.path)
	if err != nil || saved.SessionToken != "temporary-token" || a.busy || a.uploading {
		t.Fatal("temporary credential did not autosave without a connection test", err)
	}
	if tt.HasText("temporary-token") {
		t.Fatal("temporary credential is visible in the password input")
	}
	if dir := os.Getenv("PICUP_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(dir, "storage-advanced.png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, tt.Image())
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}
