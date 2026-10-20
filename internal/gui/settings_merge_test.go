package gui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/knothhe/picup/internal/config"
)

func TestSettingsMergePreservesInactiveExternalProfileEdit(t *testing.T) {
	a, archiveID := profileFixture(t)
	a.quality = 93
	latest, err := config.Load(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := latest.SwitchProfile(archiveID); err != nil {
		t.Fatal(err)
	}
	latest.Bucket = "cli-archive-bucket"
	if err := latest.SwitchProfile("default"); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(a.path, latest); err != nil {
		t.Fatal(err)
	}
	merged, err := a.settingsForSave()
	if err != nil || merged.ActiveProfile != "default" || merged.Quality != 93 {
		t.Fatal("current editor changes were not merged", err)
	}
	if err := config.Save(a.path, merged); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(a.path)
	if err != nil || saved.Quality != 93 {
		t.Fatal("editor change was not saved", err)
	}
	if err := saved.SwitchProfile(archiveID); err != nil || saved.Bucket != "cli-archive-bucket" {
		t.Fatal("inactive CLI edit was overwritten", err)
	}
	if a.config.Quality != 85 || a.config.Profiles[1].Bucket != "archive-bucket" {
		t.Fatal("merge mutated editor or baseline collections")
	}
}

func TestSettingsMergePreservesIndependentActiveAndGlobalEdits(t *testing.T) {
	a, _ := profileFixture(t)
	a.quality = 92
	a.config.Language = "zh-CN"
	a.config.Shortcuts = map[string]string{"upload": "Mod+Shift+U"}
	latest, err := config.Load(a.path)
	if err != nil {
		t.Fatal(err)
	}
	latest.Bucket, latest.SecretKey = "cli-bucket", "cli-secret"
	latest.MaxWidth = 2048
	latest.Appearance = "dark"
	if err := config.Save(a.path, latest); err != nil {
		t.Fatal(err)
	}
	merged, err := a.settingsForSave()
	if err != nil || merged.Bucket != "cli-bucket" || merged.SecretKey != "cli-secret" || merged.Quality != 92 || merged.MaxWidth != 2048 || merged.Language != "zh-CN" || merged.Appearance != "dark" {
		t.Fatal("independent active or global changes were overwritten", err)
	}
	if merged.Shortcuts["upload"] != "Mod+Shift+U" {
		t.Fatal("shortcut edit was not merged")
	}
	merged.Shortcuts["upload"] = "changed"
	merged.Profiles[0].SecretKey = "changed"
	if a.config.Shortcuts["upload"] != "Mod+Shift+U" || a.config.Profiles[0].SecretKey != "original-secret" {
		t.Fatal("merged settings share editor collections")
	}
}

func TestSettingsMergePreservesExternalProfileCollectionAndGUISelection(t *testing.T) {
	a, archiveID := profileFixture(t)
	a.quality = 94
	latest, err := config.Load(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := latest.DeleteProfile(archiveID); err != nil {
		t.Fatal(err)
	}
	newID, err := latest.AddProfile("External", false)
	if err != nil {
		t.Fatal(err)
	}
	latest.Bucket = "external-bucket"
	if err := config.Save(a.path, latest); err != nil {
		t.Fatal(err)
	}
	merged, err := a.settingsForSave()
	if err != nil || merged.ActiveProfile != "default" || merged.Quality != 94 || len(merged.Profiles) != 2 {
		t.Fatal("GUI selection or external profile collection was lost", err)
	}
	if err := merged.SwitchProfile(newID); err != nil || merged.Bucket != "external-bucket" {
		t.Fatal("externally added profile did not survive", err)
	}
	if err := merged.SwitchProfile(archiveID); err == nil {
		t.Fatal("externally deleted inactive profile was restored")
	}
}

func TestSettingsMergeRejectsDeletedActiveProfileAndCorruptFiles(t *testing.T) {
	for _, scenario := range []string{"deleted-active", "corrupt", "unreadable", "uninitialized-corrupt"} {
		t.Run(scenario, func(t *testing.T) {
			a, _ := profileFixture(t)
			a.quality = 96
			if scenario == "deleted-active" {
				latest, err := config.Load(a.path)
				if err != nil {
					t.Fatal(err)
				}
				if err := latest.DeleteProfile("default"); err != nil {
					t.Fatal(err)
				}
				if err := config.Save(a.path, latest); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "unreadable" {
				a.path = t.TempDir()
			} else {
				if scenario == "uninitialized-corrupt" {
					a.settingsSave = settingsSaveState{}
				}
				if err := os.WriteFile(a.path, []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			beforeEditor := a.currentSettings().Clone()
			beforeDisk, _ := os.ReadFile(a.path)
			if _, err := a.settingsForSave(); err == nil {
				t.Fatal("merge accepted missing active profile or unreadable saved settings")
			}
			if !reflect.DeepEqual(beforeEditor, a.currentSettings()) {
				t.Fatal("failed merge changed user input")
			}
			afterDisk, _ := os.ReadFile(a.path)
			if string(beforeDisk) != string(afterDisk) {
				t.Fatal("failed merge rewrote disk settings")
			}
		})
	}
}

func TestSettingsMergeLegacyAndMissingFileFixtures(t *testing.T) {
	for _, scenario := range []string{"legacy-file", "missing-file", "uninitialized"} {
		t.Run(scenario, func(t *testing.T) {
			c := config.Default()
			c.Bucket = "original"
			path := filepath.Join(t.TempDir(), "config.json")
			a := &application{path: path, config: c, quality: float64(c.Quality)}
			if scenario != "uninitialized" {
				a.initSettingsAutosave()
			}
			if scenario == "legacy-file" {
				if err := os.WriteFile(path, []byte(`{"bucket":"cli-original"}`), 0600); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "uninitialized" {
				if err := config.Save(path, c); err != nil {
					t.Fatal(err)
				}
			}
			a.quality = 79
			merged, err := a.settingsForSave()
			if err != nil || merged.Quality != 79 || merged.ActiveProfile != "default" || len(merged.Profiles) != 1 {
				t.Fatal("legacy fixture did not initialize profiles", err)
			}
			if scenario == "legacy-file" && merged.Bucket != "cli-original" {
				t.Fatal("legacy file's independent edit was overwritten")
			}
			if a.config.Profiles != nil || a.settingsSave.persisted.Profiles != nil {
				t.Fatal("normalizing merge mutated legacy fixture snapshots")
			}
			if err := config.Save(path, merged); err != nil {
				t.Fatal(err)
			}
			raw, _ := os.ReadFile(path)
			if !strings.Contains(string(raw), `"profiles"`) {
				t.Fatal("merged legacy fixture did not use canonical storage")
			}
		})
	}
}
