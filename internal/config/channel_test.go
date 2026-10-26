package config

import (
	"path/filepath"
	"testing"
)

func TestBuildChannelOwnsDefaultConfigurationDirectory(t *testing.T) {
	switch BuildChannel {
	case "release":
		if DirectoryName != "picup" {
			t.Fatal("release path changed")
		}
	case "development":
		if DirectoryName != "picup-dev" {
			t.Fatal("development path shares release data")
		}
	default:
		t.Fatalf("unknown compiled channel %q", BuildChannel)
	}
	t.Setenv("PICUP_CONFIG", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	got, e := Path()
	if want := filepath.Join(home, ".config", DirectoryName, "config.json"); e != nil || got != want {
		t.Fatalf("home default %q %v; want %q", got, e, want)
	}
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	got, e = Path()
	if want := filepath.Join(xdg, DirectoryName, "config.json"); e != nil || got != want {
		t.Fatalf("XDG default %q %v; want %q", got, e, want)
	}
}
func TestExplicitConfigurationPathKeepsPriorityAcrossBuildChannels(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "shared", "configuration.json")
	t.Setenv("PICUP_CONFIG", explicit)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	got, e := Path()
	if e != nil || got != explicit {
		t.Fatalf("explicit path changed by channel %q: %q %v", BuildChannel, got, e)
	}
	relative := filepath.Join("isolated-test-data", "config.json")
	t.Setenv("PICUP_CONFIG", relative)
	want, e := filepath.Abs(relative)
	if e != nil {
		t.Fatal(e)
	}
	got, e = Path()
	if e != nil || got != want {
		t.Fatalf("relative explicit path changed by channel %q: %q %v", BuildChannel, got, e)
	}
}
