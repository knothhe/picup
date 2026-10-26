package install

import (
	"debug/buildinfo"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func developmentFixture(t *testing.T) (string, string, string) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("macOS bundle installation")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PICUP_CONFIG", filepath.Join(root, "data/config.json"))
	beforeRead, beforeStop, beforeClear := readDevelopmentBuildInfo, stopDevelopment, clearDevelopmentLogin
	t.Cleanup(func() {
		readDevelopmentBuildInfo = beforeRead
		stopDevelopment = beforeStop
		clearDevelopmentLogin = beforeClear
	})
	readDevelopmentBuildInfo = func(string) (*debug.BuildInfo, error) {
		return &buildinfo.BuildInfo{Settings: []debug.BuildSetting{{Key: "-tags", Value: "picup_dev"}, {Key: "GOOS", Value: "darwin"}, {Key: "GOARCH", Value: runtime.GOARCH}}}, nil
	}
	stopDevelopment = func(string, time.Duration) error { return nil }
	clearDevelopmentLogin = func(string) error { return nil }
	source := filepath.Join(root, "source/Picup Dev.app")
	for path, value := range map[string]string{
		"Contents/Info.plist":      `<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>com.knothhe.picup.dev</string><key>CFBundleExecutable</key><string>Picup Dev</string><key>PicupDevMaintenanceProtocol</key><string>1</string></dict></plist>`,
		"Contents/MacOS/Picup Dev": "dev-gui-v1", "Contents/Resources/picup-dev": "dev-cli-v1",
	} {
		full := filepath.Join(source, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(value), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return source, filepath.Join(root, "Applications"), filepath.Join(root, "bin")
}

func TestDevelopmentInstallUninstallCoexistsAndPreservesUserData(t *testing.T) {
	source, apps, bin := developmentFixture(t)
	for path := range map[string]bool{filepath.Join(apps, "Picup.app/formal-data"): true, filepath.Join(bin, "picup"): true, os.Getenv("PICUP_CONFIG"): true} {
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, []byte("preserve"), 0600)
	}
	if err := InstallDevelopment(source, apps, bin); err != nil {
		t.Fatal(err)
	}
	if err := InstallDevelopment(source, apps, bin); err != nil {
		t.Fatal("repeat install", err)
	}
	if err := UninstallDevelopment(apps, bin); err != nil {
		t.Fatal(err)
	}
	if err := UninstallDevelopment(apps, bin); err != nil {
		t.Fatal("repeat uninstall", err)
	}
	for _, path := range []string{filepath.Join(apps, "Picup.app/formal-data"), filepath.Join(bin, "picup"), os.Getenv("PICUP_CONFIG")} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "preserve" {
			t.Fatalf("changed preserved path %s: %v", path, err)
		}
	}
}

func TestDevelopmentUninstallRejectsChangedAndFormalTargets(t *testing.T) {
	source, apps, bin := developmentFixture(t)
	if err := InstallDevelopment(source, apps, bin); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(bin, "picup-dev")
	os.WriteFile(cli, []byte("user-modified"), 0755)
	if err := UninstallDevelopment(apps, bin); err == nil {
		t.Fatal("removed modified CLI")
	}
	if _, err := os.Stat(filepath.Join(apps, "Picup Dev.app")); err != nil {
		t.Fatal("removed app before validating CLI")
	}
	os.WriteFile(cli, []byte("dev-cli-v1"), 0755)
	plist := filepath.Join(apps, "Picup Dev.app/Contents/Info.plist")
	data, _ := os.ReadFile(plist)
	os.WriteFile(plist, []byte(strings.ReplaceAll(string(data), "com.knothhe.picup.dev", "com.knothhe.picup")), 0644)
	if err := UninstallDevelopment(apps, bin); err == nil {
		t.Fatal("removed formal bundle")
	}
}

func TestDevelopmentQuitFailurePreventsReplacementAndDeletion(t *testing.T) {
	source, apps, bin := developmentFixture(t)
	if err := InstallDevelopment(source, apps, bin); err != nil {
		t.Fatal(err)
	}
	stopDevelopment = func(string, time.Duration) error { return errors.New("active upload blocked normal quit") }
	if err := UninstallDevelopment(apps, bin); err == nil {
		t.Fatal("uninstalled while running")
	}
	if err := InstallDevelopment(source, apps, bin); err == nil {
		t.Fatal("replaced while running")
	}
	for _, path := range []string{filepath.Join(apps, "Picup Dev.app/Contents/MacOS/Picup Dev"), filepath.Join(bin, "picup-dev"), filepath.Join(apps, developmentReceipt)} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("changed install after failed quit", err)
		}
	}
}

func TestDevelopmentPartialUninstallRetainsReceiptAndCanRetry(t *testing.T) {
	source, apps, bin := developmentFixture(t)
	if err := InstallDevelopment(source, apps, bin); err != nil {
		t.Fatal(err)
	}
	before := removeDevelopmentFile
	t.Cleanup(func() { removeDevelopmentFile = before })
	removeDevelopmentFile = func(path string) error {
		if path == filepath.Join(bin, "picup-dev") {
			return errors.New("fixture permission failure")
		}
		return os.Remove(path)
	}
	if err := UninstallDevelopment(apps, bin); err == nil {
		t.Fatal("ignored partial failure")
	}
	if _, err := os.Stat(filepath.Join(apps, developmentReceipt)); err != nil {
		t.Fatal("lost ownership needed for retry")
	}
	removeDevelopmentFile = before
	if err := UninstallDevelopment(apps, bin); err != nil {
		t.Fatal("could not retry partial removal", err)
	}
}

func TestDevelopmentRejectsSymlinkAndMissingOwnership(t *testing.T) {
	source, apps, bin := developmentFixture(t)
	os.MkdirAll(apps, 0755)
	alias := filepath.Join(filepath.Dir(apps), "apps-alias")
	os.Symlink(apps, alias)
	if err := InstallDevelopment(source, alias, bin); err == nil {
		t.Fatal("accepted symlink installation directory")
	}
	if err := InstallDevelopment(source, apps, bin); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(apps, developmentReceipt))
	if err := UninstallDevelopment(apps, bin); err == nil {
		t.Fatal("removed unowned installation")
	}
	if err := InstallDevelopment(source, apps, bin); err != nil {
		t.Fatal("could not repair ownership through verified reinstall", err)
	}
	if err := UninstallDevelopment(apps, bin); err != nil {
		t.Fatal(err)
	}
}

func TestDevelopmentLoginCleanupFailureAndMissingAppKeepRecoveryRecord(t *testing.T) {
	source, apps, bin := developmentFixture(t)
	if err := InstallDevelopment(source, apps, bin); err != nil {
		t.Fatal(err)
	}
	clearDevelopmentLogin = func(string) error { return errors.New("login item permission denied") }
	if err := UninstallDevelopment(apps, bin); err == nil {
		t.Fatal("ignored login item cleanup error")
	}
	if _, err := os.Stat(filepath.Join(apps, "Picup Dev.app")); err != nil {
		t.Fatal("removed app after login cleanup failure")
	}
	os.RemoveAll(filepath.Join(apps, "Picup Dev.app"))
	if err := UninstallDevelopment(apps, bin); err == nil {
		t.Fatal("reported full removal without verifying remaining login item")
	}
	if _, err := os.Stat(filepath.Join(apps, developmentReceipt)); err != nil {
		t.Fatal("removed recovery receipt")
	}
}

func TestDevelopmentCLIConflictPrecedesQuitAndOwnFilesCannotResolveThroughAnotherBundle(t *testing.T) {
	source, apps, bin := developmentFixture(t)
	if err := InstallDevelopment(source, apps, bin); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(bin, "picup-dev"), []byte("unmanaged replacement"), 0755)
	stopDevelopment = func(string, time.Duration) error {
		t.Fatal("requested quit before discovering CLI conflict")
		return nil
	}
	if err := InstallDevelopment(source, apps, bin); err == nil {
		t.Fatal("overwrote CLI conflict")
	}
	macOS := filepath.Join(source, "Contents/MacOS")
	foreign := filepath.Join(filepath.Dir(source), "foreign-code")
	if err := os.Rename(macOS, foreign); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, macOS); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectDevelopment(source); err == nil {
		t.Fatal("accepted code redirected through a directory symlink")
	}
}
