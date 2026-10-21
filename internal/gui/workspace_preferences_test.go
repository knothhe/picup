package gui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
)

func TestSidebarPreferencesSurviveRestartFromButtonAndShortcut(t *testing.T) {
	for _, control := range []string{"button", "shortcut"} {
		t.Run(control, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "personal.json")
			for _, collapsed := range []bool{true, false} {
				cfg := config.Default()
				a := &application{path: path, config: cfg, quality: float64(cfg.Quality)}
				if err := a.restoreWorkspacePreferences(); err != nil {
					t.Fatal(err)
				}
				if a.sidebarCollapsed == collapsed {
					t.Fatal("restart did not restore the previous sidebar state")
				}
				tt := ui.NewTester(nativeTestView(a), 1040, 760)
				tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
				tt.Frame()
				if control == "shortcut" {
					tt.Key(ui.Cmd, ui.KeyB)
				} else {
					label := "收起侧边栏"
					if !collapsed {
						label = "展开侧边栏"
					}
					if err := tt.Click(label); err != nil {
						t.Fatal(err)
					}
				}
				// A restart immediately after the interaction must see the new state.
				b := &application{path: path}
				if err := b.restoreWorkspacePreferences(); err != nil || b.sidebarCollapsed != collapsed {
					t.Fatal("sidebar state was not persisted immediately", err)
				}
				tt.Advance(time.Second)
				if a.sidebarCollapsed != collapsed || tt.HasText("设置已保存") {
					t.Fatal("sidebar toggle changed state or produced settings feedback")
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("sidebar toggle wrote the storage configuration", err)
				}
				info, err := os.Stat(workspacePreferencesPath(path))
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("workspace preferences were not saved privately", info, err)
				}
			}
			c := &application{path: path}
			if err := c.restoreWorkspacePreferences(); err != nil || c.sidebarCollapsed {
				t.Fatal("expanded state was not restored on the final restart", err)
			}
		})
	}
}

func TestSidebarPreferencesRemainSharedAcrossProfilesAndWindowRecreation(t *testing.T) {
	cfg := config.Default()
	a := &application{path: filepath.Join(t.TempDir(), "config.json"), config: cfg, quality: float64(cfg.Quality)}
	a.initSettingsAutosave()
	a.config.SecretKey = "unsaved-secret"
	a.profiles.menuOpen = true
	a.toggleSidebar()
	if !a.sidebarCollapsed || a.profiles.menuOpen || a.config.SecretKey != "unsaved-secret" {
		t.Fatal("sidebar toggle lost pending edits or left the profile menu open")
	}
	if a.settingsSave.persisted.SecretKey != "" {
		t.Fatal("sidebar toggle committed pending storage edits")
	}
	other := config.Default()
	if _, err := other.AddProfile("Other", false); err != nil {
		t.Fatal(err)
	}
	a.applyProfileConfig(other)
	a.releaseUIPresentation()
	if !a.sidebarCollapsed {
		t.Fatal("profile switch or window recreation reset the sidebar")
	}
	b := &application{path: a.path, config: other}
	if err := b.restoreWorkspacePreferences(); err != nil || !b.sidebarCollapsed {
		t.Fatal("sidebar preference was scoped to the previous profile", err)
	}
}

func TestSidebarPreferencesHandleMissingCorruptAndUnwritableFiles(t *testing.T) {
	a := &application{path: filepath.Join(t.TempDir(), "config.json")}
	if err := a.restoreWorkspacePreferences(); err != nil || a.sidebarCollapsed {
		t.Fatal("missing preferences did not keep the expanded default", err)
	}
	path := workspacePreferencesPath(a.path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("restoring defaults wrote a preference file", err)
	}
	if err := os.WriteFile(path, []byte(`{"sidebar_collapsed":"invalid"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.restoreWorkspacePreferences(); err == nil || a.sidebarCollapsed {
		t.Fatal("corrupt preferences were accepted or changed the sidebar")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	a.toggleSidebar()
	if !a.sidebarCollapsed || !a.messageError || len(a.notifications) != 1 {
		t.Fatal("failed persistence prevented toggling or did not report the failure")
	}
}
