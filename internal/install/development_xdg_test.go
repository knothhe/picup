package install

import (
	"debug/buildinfo"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func linuxDevelopmentFixture(t *testing.T) (string, string, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Linux directory installation")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PICUP_CONFIG", filepath.Join(root, "data/config.json"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	beforeRead, beforeStop := readDevelopmentBuildInfo, stopDevelopment
	t.Cleanup(func() { readDevelopmentBuildInfo, stopDevelopment = beforeRead, beforeStop })
	readDevelopmentBuildInfo = func(string) (*debug.BuildInfo, error) {
		return &buildinfo.BuildInfo{Settings: []debug.BuildSetting{{Key: "-tags", Value: "picup_dev"}, {Key: "GOOS", Value: "linux"}, {Key: "GOARCH", Value: runtime.GOARCH}}}, nil
	}
	stopDevelopment = func(string, time.Duration) error { return nil }
	source := filepath.Join(root, "source/picup-dev")
	writeLinuxDevelopment(t, source, "v1")
	return source, filepath.Join(root, "lib"), filepath.Join(root, "bin")
}

func writeLinuxDevelopment(t *testing.T, directory, version string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range linuxDevelopmentFiles {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(name+"-"+version), 0755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLinuxDevelopmentInstallUpdateUninstallPreservesUserData(t *testing.T) {
	source, lib, bin := linuxDevelopmentFixture(t)
	preserved := []string{os.Getenv("PICUP_CONFIG"), filepath.Join(bin, "picup"), filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "autostart/other.desktop")}
	for _, path := range preserved {
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, []byte("[Desktop Entry]\nExec=\"/usr/bin/other\"\n"), 0600)
	}
	if err := InstallDevelopment(source, lib, bin); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(lib, "picup-dev")
	desktop := filepath.Join(os.Getenv("XDG_DATA_HOME"), "applications/com.knothhe.picup.dev.desktop")
	entry, err := os.ReadFile(desktop)
	if err != nil || !strings.Contains(string(entry), `Exec="`+filepath.Join(app, "picup-gui")+`"`) {
		t.Fatalf("desktop entry %q: %v", entry, err)
	}
	if data, err := os.ReadFile(filepath.Join(bin, "picup-dev")); err != nil || string(data) != "picup-dev-v1" {
		t.Fatalf("CLI %q: %v", data, err)
	}
	writeLinuxDevelopment(t, source, "v2")
	if err := InstallDevelopment(source, lib, bin); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(app, "picup-gui")); string(data) != "picup-gui-v2" {
		t.Fatal("update did not replace the GUI", string(data))
	}
	autostart := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "autostart/Picup-Dev.desktop")
	os.WriteFile(autostart, []byte("[Desktop Entry]\nExec="+desktopExec(filepath.Join(app, "picup-gui"))+" --login\n"), 0644)
	if err := UninstallDevelopment(lib, bin); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{app, filepath.Join(bin, "picup-dev"), filepath.Join(bin, ".picup-dev-cli.json"), desktop, autostart, filepath.Join(lib, developmentReceipt)} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("left installed file", path)
		}
	}
	for _, path := range preserved {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("removed user file", path)
		}
	}
}

func TestLinuxDevelopmentRejectsForeignTargets(t *testing.T) {
	source, lib, bin := linuxDevelopmentFixture(t)
	desktop := filepath.Join(os.Getenv("XDG_DATA_HOME"), "applications/com.knothhe.picup.dev.desktop")
	os.MkdirAll(filepath.Dir(desktop), 0755)
	os.WriteFile(desktop, []byte("foreign"), 0644)
	if err := InstallDevelopment(source, lib, bin); err == nil {
		t.Fatal("replaced a foreign desktop entry")
	}
	os.Remove(desktop)
	os.WriteFile(filepath.Join(source, "extra"), []byte("x"), 0644)
	if err := InstallDevelopment(source, lib, bin); err == nil {
		t.Fatal("accepted a source with unexpected files")
	}
	os.Remove(filepath.Join(source, "extra"))
	if err := InstallDevelopment(source, lib, bin); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(lib, "picup-dev/picup-gui"), []byte("changed"), 0755)
	if err := UninstallDevelopment(lib, bin); err == nil {
		t.Fatal("removed a changed application")
	}
	if _, err := os.Stat(filepath.Join(bin, "picup-dev")); err != nil {
		t.Fatal("removed the CLI after refusing")
	}
}

func TestLinuxDevelopmentRejectsReleaseBuilds(t *testing.T) {
	source, lib, bin := linuxDevelopmentFixture(t)
	readDevelopmentBuildInfo = func(string) (*debug.BuildInfo, error) {
		return &buildinfo.BuildInfo{Settings: []debug.BuildSetting{{Key: "GOOS", Value: "linux"}, {Key: "GOARCH", Value: runtime.GOARCH}}}, nil
	}
	if err := InstallDevelopment(source, lib, bin); err == nil {
		t.Fatal("installed a release build as Dev")
	}
}
