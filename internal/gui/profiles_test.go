package gui

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
)

func profileFixture(t *testing.T) (*application, string) {
	t.Helper()
	c := config.Default()
	c.Bucket, c.SecretKey = "original-bucket", "original-secret"
	c.EnsureProfiles()
	id, err := c.AddProfile("Archive", true)
	if err != nil {
		t.Fatal(err)
	}
	c.Bucket, c.SecretKey, c.Quality = "archive-bucket", "archive-secret", 60
	if err := c.SwitchProfile("default"); err != nil {
		t.Fatal(err)
	}
	a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: c, quality: float64(c.Quality)}
	if err := config.Save(a.path, c); err != nil {
		t.Fatal(err)
	}
	a.initSettingsAutosave()
	return a, id
}

func TestProfileSwitchFlushesEditsAndPreservesUploadQueue(t *testing.T) {
	a, id := profileFixture(t)
	a.config.SecretKey = "latest-secret"
	a.quality = 91
	j := &job{id: 1, name: "pending.png", status: "等待上传"}
	a.jobs = []*job{j}
	if !a.switchProfile(id) {
		t.Fatal(a.profiles.err)
	}
	if a.config.Bucket != "archive-bucket" || a.quality != 60 || a.uploadDisplayConfig().SecretKey != "archive-secret" || a.jobs[0] != j {
		t.Fatal("switch did not update destination and processing options or retained queue")
	}
	loaded, err := config.Load(a.path)
	if err != nil || loaded.ActiveProfile != id {
		t.Fatal("selection was not persisted", err)
	}
	if !a.switchProfile("default") || a.config.SecretKey != "latest-secret" || a.quality != 91 {
		t.Fatal("unsaved edits did not survive a round trip")
	}
}

func TestProfileSwitchFailureKeepsCurrentTargetAndEditor(t *testing.T) {
	a, id := profileFixture(t)
	a.path = t.TempDir()
	if a.switchProfile(id) || a.config.ActiveProfile != "default" || a.uploadDisplayConfig().Bucket != "original-bucket" {
		t.Fatal("failed selection changed the upload target")
	}
	a.config.SecretKey = "pending-secret"
	if a.switchProfile(id) || a.config.SecretKey != "pending-secret" || !a.settingsSave.failed {
		t.Fatal("switch lost a failed pending edit")
	}
}

func TestEffectiveUsesVisibleProfileUntilExternalSelectionIsApplied(t *testing.T) {
	a, id := profileFixture(t)
	latest, err := config.Load(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := latest.SwitchProfile(id); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(a.path, latest); err != nil {
		t.Fatal(err)
	}
	c, err := a.effective()
	if err != nil || c.ActiveProfile != a.config.ActiveProfile || c.Bucket != "original-bucket" {
		t.Fatal("an external selection silently changed the visible upload destination", err)
	}
	saved, err := config.Load(a.path)
	if err != nil || saved.ActiveProfile != id {
		t.Fatal("reading the visible profile rewrote external settings", err)
	}
}

func TestProfileSwitchCancelsOldGalleryAndIsolatesViewPreferences(t *testing.T) {
	a, id := profileFixture(t)
	a.gallery = galleryFixture()
	a.gallery.search, a.gallery.prefix, a.gallery.page = "海边", "images/旅行/", 1
	a.gallery.selected = map[string]bool{"old-image.png": true}
	a.gallery.previewOpen = true
	ctx, cancel := context.WithCancel(context.Background())
	a.gallery.cancel, a.gallery.busy = cancel, true
	generation := a.gallery.generation
	if !a.switchProfile(id) {
		t.Fatal(a.profiles.err)
	}
	if ctx.Err() == nil || a.gallery.generation <= generation || a.gallery.loaded || len(a.gallery.objects) != 0 || len(a.gallery.selected) != 0 || a.gallery.previewOpen || a.gallery.hasViewPreferences() {
		t.Fatal("old gallery state survived the profile switch")
	}
	a.gallery.search = "archive"
	if !a.switchProfile("default") || a.gallery.search != "海边" || a.gallery.prefix != "images/旅行/" || a.gallery.page != 1 {
		t.Fatal("returning to a profile did not restore its own view preferences")
	}
	if !a.switchProfile(id) || a.gallery.search != "archive" {
		t.Fatal("profiles share gallery view preferences")
	}
	a.flushGalleryPreferences()
}

func TestProfileSwitchBlockedWhileUploadOrGalleryOperationRuns(t *testing.T) {
	for _, state := range []string{"upload", "dialog", "gallery-operation"} {
		t.Run(state, func(t *testing.T) {
			a, id := profileFixture(t)
			a.uploading = state == "upload"
			a.busy = state == "dialog"
			a.gallery.operation = state == "gallery-operation"
			if a.switchProfile(id) || a.config.ActiveProfile != "default" {
				t.Fatal("active operation changed profile")
			}
		})
	}
}

func TestSettingsSnapshotsDoNotAliasProfiles(t *testing.T) {
	a, _ := profileFixture(t)
	a.config.Profiles[1].SecretKey = "edited"
	if a.settingsSave.persisted.Profiles[1].SecretKey == "edited" || a.settingsSave.observed.Profiles[1].SecretKey == "edited" {
		t.Fatal("profile edit mutated autosave snapshots")
	}
}

func profileTester(a *application, width, height int) *ui.Tester {
	tt := ui.NewTester(nativeTestView(a), width, height)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	return tt
}

func TestProfileSelectorSwitchesUploadDestination(t *testing.T) {
	a, id := profileFixture(t)
	tt := profileTester(a, 760, 620)
	if err := tt.Click("切换配置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Archive"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.config.ActiveProfile != id || !tt.HasText("R2 · archive-bucket") || !tt.HasText("默认压缩 · 质量 60") {
		t.Fatal("selector did not change visible destination and upload options", tt.Texts())
	}
}

func TestProfileDialogCreateRenameAndDelete(t *testing.T) {
	a, _ := profileFixture(t)
	a.tab, a.settingsTab = 2, settingsProfilesTab
	tt := profileTester(a, 760, 620)
	if err := tt.Click("新建配置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("新项目")
	if err := tt.Click("创建并切换"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.profiles.editOpen || a.config.CurrentProfileName() != "新项目" || a.config.SecretKey != "" || a.config.Bucket != "" {
		t.Fatal("creating blank profile did not select an empty destination", tt.Texts())
	}
	a.beginProfileEdit("rename")
	a.profiles.name = "Archive"
	tt.Frame()
	if err := tt.Click("保存名称"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.profiles.editOpen || !tt.HasText("配置名称已存在，请使用其他名称。") {
		t.Fatal("duplicate name was not rejected in place", tt.Texts())
	}
	a.profiles.name = "项目图片"
	tt.Frame()
	if err := tt.Click("保存名称"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.profiles.editOpen || a.config.CurrentProfileName() != "项目图片" {
		t.Fatal("renaming did not persist")
	}
	tt.Scroll(500, 400, 0, 400)
	if err := tt.Click("删除配置 项目图片"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.profiles.deleteOpen || !tt.HasText("删除配置「项目图片」？") {
		t.Fatal("profile deletion did not request confirmation")
	}
	if err := tt.Click("删除配置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.profiles.deleteOpen || a.config.ActiveProfile != "default" || len(a.config.Profiles) != 2 {
		t.Fatal("deletion did not select the fallback profile")
	}
}

func TestProfileControlsFitMinimumWindow(t *testing.T) {
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, language := range []string{"zh-CN", "en"} {
		for _, tab := range []int{0, 2, 4} {
			t.Run(fmt.Sprintf("%s/tab=%d", language, tab), func(t *testing.T) {
				SetUILanguage(language)
				a, _ := profileFixture(t)
				a.tab, a.gallery = tab, galleryFixture()
				a.config.Profiles[0].Name = "旅行影像与长期归档 profile with a long name"
				tt := profileTester(a, 760, 620)
				for _, label := range []string{"切换配置"} {
					r, ok := tt.Find(tr(label))
					if !ok || r.W < 100 || r.X < 0 || r.X+r.W > sidebarExpandedWidth || r.Y > 120 {
						t.Fatal("profile control does not fit", label, r)
					}
				}
				if tab == 2 {
					a.settingsTab = settingsProfilesTab
					tt.Frame()
					for _, label := range []string{"新建配置", "编辑存储", "复制", "重命名", "删除"} {
						r, ok := tt.Find(tr(label))
						if !ok || r.X < sidebarExpandedWidth || r.X+r.W > 760 || r.Y+r.H > 620 {
							t.Fatal("profile action does not fit", label, r)
						}
					}
				}
			})
		}
	}
}

func TestProfileManagerActionsTargetTheSelectedCard(t *testing.T) {
	for _, action := range []string{"rename", "copy", "delete", "edit"} {
		t.Run(action, func(t *testing.T) {
			a, archiveID := profileFixture(t)
			a.tab, a.settingsTab = 2, settingsProfilesTab
			tt := profileTester(a, 1040, 760)
			switch action {
			case "rename":
				if err := tt.Click("重命名配置 Archive"); err != nil {
					t.Fatal(err)
				}
				a.profiles.name = "Archive renamed"
				tt.Frame()
				if err := tt.Click("保存名称"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				if a.config.ActiveProfile != "default" || a.config.Profiles[1].Name != "Archive renamed" {
					t.Fatal("renaming an inactive profile changed the selection or the wrong name")
				}
			case "copy":
				if err := tt.Click("复制配置 Archive"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				if err := tt.Click("创建并切换"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				if a.config.Bucket != "archive-bucket" || a.config.SecretKey != "archive-secret" || a.config.ActiveProfile == archiveID || a.config.ActiveProfile == "default" {
					t.Fatal("copying an inactive profile used the current profile's settings")
				}
			case "delete":
				if err := tt.Click("删除配置 Archive"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				if !a.profiles.deleteOpen || tt.HasText("删除后自动切换") {
					t.Fatal("inactive deletion confirmation promised an unnecessary selection change")
				}
				if err := tt.Click("删除配置"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				if a.config.ActiveProfile != "default" || len(a.config.Profiles) != 1 || a.config.Bucket != "original-bucket" {
					t.Fatal("deleting an inactive profile changed the current profile")
				}
			case "edit":
				if err := tt.Click("编辑 Archive 的存储"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				if a.config.ActiveProfile != archiveID || a.settingsTab != 0 || a.config.Bucket != "archive-bucket" || !tt.HasText("连接方式") {
					t.Fatal("Edit storage did not open the card's profile")
				}
			}
		})
	}
}

func TestRenderProfiles(t *testing.T) {
	dir := os.Getenv("PICUP_SCREENSHOTS")
	if dir == "" {
		t.Skip("set PICUP_SCREENSHOTS to inspect profiles")
	}
	t.Cleanup(func() { SetUILanguage("zh-CN") })
	for _, state := range []string{"upload", "selector", "settings", "profiles", "create", "delete", "narrow-en", "dark", "profiles-narrow-en", "profiles-dark", "compact-selector"} {
		SetUILanguage("zh-CN")
		a, _ := profileFixture(t)
		width, height := 1040, 760
		switch state {
		case "settings", "dark":
			a.tab = 2
		case "profiles", "create", "delete", "profiles-dark":
			a.tab, a.settingsTab = 2, settingsProfilesTab
		case "narrow-en":
			width, height, a.tab = 760, 620, 2
			SetUILanguage("en")
		case "profiles-narrow-en":
			width, height, a.tab, a.settingsTab = 760, 620, 2, settingsProfilesTab
			SetUILanguage("en")
		case "compact-selector":
			width, height, a.sidebarCollapsed = 760, 620, true
		}
		if state == "create" {
			a.beginProfileEdit("new")
		} else if state == "delete" {
			a.profiles = profileUIState{deleteOpen: true, target: "default"}
		}
		tt := profileTester(a, width, height)
		if state == "dark" || state == "profiles-dark" {
			tt.SetDark(true)
		}
		if state == "selector" || state == "compact-selector" {
			if err := tt.Click("切换配置"); err != nil {
				t.Fatal(err)
			}
		}
		tt.Frame()
		tt.Advance(400 * time.Millisecond)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(dir, "profiles-"+state+".png"))
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
