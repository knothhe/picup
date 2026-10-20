package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWindowSettingsDefaultsMigrationAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, raw := range []string{`{"provider":"r2"}`, `{"profiles":[{"id":"default","name":"Default","provider":"r2","region":"auto","path_template":"images/{sha256}.{extName}","quality":85,"format":"original","output":"url"}],"active_profile":"default"}`} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(path)
		if err != nil || !c.CloseToTray || c.LightweightMode || c.LaunchAtLogin || c.DockIcon != DockIconShow || c.TrayClickAction != TrayClickQuickPanel {
			t.Fatal("legacy configuration did not gain compatible window defaults", err)
		}
		c.CloseToTray, c.LightweightMode, c.LaunchAtLogin = false, true, true
		c.DockIcon, c.TrayClickAction = DockIconFollowWindow, TrayClickMainWindow
		if _, err := c.AddProfile("Other", false); err != nil {
			t.Fatal(err)
		}
		if err := Save(path, c); err != nil {
			t.Fatal(err)
		}
		got, err := Load(path)
		if err != nil || !reflect.DeepEqual(c, got) || got.CloseToTray || !got.LightweightMode || !got.LaunchAtLogin || got.DockIcon != DockIconFollowWindow || got.TrayClickAction != TrayClickMainWindow {
			t.Fatal("window preferences did not survive profile creation and save/load", err)
		}
	}
}

func TestWindowSettingsRejectInvalidValues(t *testing.T) {
	c := Default()
	for _, setting := range []struct{ key, value string }{
		{"dock_icon", "invisible"}, {"tray_click_action", "nothing"}, {"lightweight_mode", "maybe"},
		{"launch_at_login", "true"}, // Editing JSON alone cannot register a system login item.
	} {
		if err := c.Set(setting.key, setting.value); err == nil {
			t.Fatalf("accepted invalid setting %s", setting.key)
		}
	}
	for _, invalid := range []Config{
		func() Config { c := Default(); c.DockIcon = "unknown"; return c }(),
		func() Config { c := Default(); c.TrayClickAction = "unknown"; return c }(),
	} {
		if err := Save(filepath.Join(t.TempDir(), "config.json"), invalid); err == nil {
			t.Fatal("saved invalid window policy")
		}
	}
}
