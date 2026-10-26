package install

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"testing"
)

func managedCLIFixture(t *testing.T) (string, string, *int) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	if err := os.WriteFile(source, []byte("fixture-cli-program"), 0755); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "bin")
	beforeInfo, beforeVersion := readCLIInfo, inspectCLIVersion
	t.Cleanup(func() { readCLIInfo = beforeInfo; inspectCLIVersion = beforeVersion })
	readCLIInfo = func(path string) (*debug.BuildInfo, error) {
		tags := ""
		if cliChannel(Filename()) == "development" {
			tags = "picup_dev"
		}
		return &debug.BuildInfo{Path: "github.com/knothhe/picup/cmd/picup", Main: debug.Module{Path: "github.com/knothhe/picup"}, Settings: []debug.BuildSetting{{Key: "-tags", Value: tags}, {Key: "GOOS", Value: runtime.GOOS}, {Key: "GOARCH", Value: runtime.GOARCH}}}, nil
	}
	calls := new(int)
	inspectCLIVersion = func(context.Context, string) (string, error) { *calls++; return "0.1.1", nil }
	return source, directory, calls
}

func TestCLIStatusInstallUninstallAndDataRetention(t *testing.T) {
	source, directory, calls := managedCLIFixture(t)
	status := InspectCLI(context.Background(), directory)
	if status.State != CLIMissing || !status.CanInstall || status.CanUninstall {
		t.Fatal(status)
	}
	if err := InstallCLI(context.Background(), source, directory); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	status = InspectCLI(context.Background(), directory)
	if status.State != CLIManaged || status.Version != "0.1.1" || !status.InPath || !status.CanUninstall || *calls != 1 {
		t.Fatal(status, *calls)
	}
	preserved := filepath.Join(filepath.Dir(directory), "data/config.json")
	os.MkdirAll(filepath.Dir(preserved), 0700)
	os.WriteFile(preserved, []byte("user-data"), 0600)
	if err := UninstallCLI(context.Background(), directory); err != nil {
		t.Fatal(err)
	}
	if err := UninstallCLI(context.Background(), directory); err != nil {
		t.Fatal("repeat uninstall", err)
	}
	if status := InspectCLI(context.Background(), directory); status.State != CLIMissing {
		t.Fatal(status)
	}
	if data, err := os.ReadFile(preserved); err != nil || string(data) != "user-data" {
		t.Fatal("changed user data", err)
	}
}

func TestCLIUnmanagedProgramIsNotExecutedAndCanOnlyAdoptIdenticalSource(t *testing.T) {
	source, directory, calls := managedCLIFixture(t)
	os.MkdirAll(directory, 0755)
	data, _ := os.ReadFile(source)
	os.WriteFile(filepath.Join(directory, Filename()), data, 0755)
	status := InspectCLI(context.Background(), directory)
	if status.State != CLIUnmanaged || status.CanUninstall || *calls != 0 {
		t.Fatal(status, *calls)
	}
	if err := UninstallCLI(context.Background(), directory); err == nil {
		t.Fatal("removed unowned program")
	}
	if err := InstallCLI(context.Background(), source, directory); err != nil {
		t.Fatal("could not adopt identical bundled program", err)
	}
	if status := InspectCLI(context.Background(), directory); status.State != CLIManaged {
		t.Fatal(status)
	}
	os.WriteFile(filepath.Join(directory, Filename()), []byte("modified"), 0755)
	if status := InspectCLI(context.Background(), directory); status.State != CLIChanged || status.CanUninstall || status.CanInstall {
		t.Fatal(status)
	}
	if err := UninstallCLI(context.Background(), directory); err == nil {
		t.Fatal("removed modified program")
	}
}

func TestCLIForeignChannelAndSymlinkCannotBeRemoved(t *testing.T) {
	source, directory, calls := managedCLIFixture(t)
	if err := InstallCLI(context.Background(), source, directory); err != nil {
		t.Fatal(err)
	}
	before := readCLIInfo
	readCLIInfo = func(path string) (*debug.BuildInfo, error) {
		info, err := before(path)
		if err != nil {
			return info, err
		}
		for index := range info.Settings {
			if info.Settings[index].Key == "-tags" {
				if cliChannel(Filename()) == "development" {
					info.Settings[index].Value = ""
				} else {
					info.Settings[index].Value = "picup_dev"
				}
			}
		}
		return info, nil
	}
	if status := InspectCLI(context.Background(), directory); status.State != CLIForeign || status.CanUninstall || *calls != 0 {
		t.Fatal(status, *calls)
	}
	if err := UninstallCLI(context.Background(), directory); err == nil {
		t.Fatal("removed another channel")
	}
	readCLIInfo = before
	path := filepath.Join(directory, Filename())
	os.Remove(path)
	os.Symlink(source, path)
	if status := InspectCLI(context.Background(), directory); status.State != CLIBlocked || status.CanUninstall {
		t.Fatal(status)
	}
	if err := UninstallCLI(context.Background(), directory); err == nil {
		t.Fatal("followed CLI symlink")
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatal("changed symlink target")
	}
}

func TestCLIPartialUninstallPreservesRetryOwnership(t *testing.T) {
	source, directory, _ := managedCLIFixture(t)
	if err := InstallCLI(context.Background(), source, directory); err != nil {
		t.Fatal(err)
	}
	before := removeCLIFile
	t.Cleanup(func() { removeCLIFile = before })
	marker := filepath.Join(directory, cliMarker(Filename()))
	removeCLIFile = func(path string) error {
		if path == marker {
			return errors.New("receipt permission fixture")
		}
		return os.Remove(path)
	}
	if err := UninstallCLI(context.Background(), directory); err == nil {
		t.Fatal("ignored partial receipt failure")
	}
	if status := InspectCLI(context.Background(), directory); status.State != CLIRemovalPending || !status.CanUninstall {
		t.Fatal(status)
	}
	removeCLIFile = before
	if err := UninstallCLI(context.Background(), directory); err != nil {
		t.Fatal("retry failed", err)
	}
}

func TestCLIReceiptCannotClaimAnotherPathOrFutureSchema(t *testing.T) {
	source, directory, _ := managedCLIFixture(t)
	if err := InstallCLI(context.Background(), source, directory); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(directory, cliMarker(Filename()))
	data, _ := os.ReadFile(marker)
	var record receipt
	json.Unmarshal(data, &record)
	record.Path = filepath.Join(directory, "other")
	encoded, _ := json.Marshal(record)
	os.WriteFile(marker, encoded, 0600)
	if err := UninstallCLI(context.Background(), directory); err == nil {
		t.Fatal("accepted receipt for another path")
	}
	if status := InspectCLI(context.Background(), directory); status.State != CLIUnmanaged || status.CanUninstall {
		t.Fatal(status)
	}
	record.Path = filepath.Join(directory, Filename())
	record.Schema = 99
	encoded, _ = json.Marshal(record)
	os.WriteFile(marker, encoded, 0600)
	if err := UninstallCLI(context.Background(), directory); err == nil {
		t.Fatal("accepted future receipt schema")
	}
	if err := InstallCLI(context.Background(), source, directory); err == nil {
		t.Fatal("overwrote future receipt even though source bytes match")
	}
}

func TestCLIIncompleteReceiptCanOnlyBeRepairedWithExactBundledBytes(t *testing.T) {
	source, directory, calls := managedCLIFixture(t)
	if err := InstallCLI(context.Background(), source, directory); err != nil {
		t.Fatal(err)
	}
	// Simulate executable replacement succeeding before the receipt write fails.
	os.WriteFile(source, []byte("trusted-bundled-v2"), 0755)
	os.WriteFile(filepath.Join(directory, Filename()), []byte("trusted-bundled-v2"), 0755)
	if status := InspectCLI(context.Background(), directory); status.State != CLIChanged || status.CanInstall {
		t.Fatal(status)
	}
	if status := InspectCLIWithSource(context.Background(), directory, source); status.State != CLIChanged || !status.CanInstall || status.CanUninstall || *calls != 0 {
		t.Fatal(status, *calls)
	}
	if err := InstallCLI(context.Background(), source, directory); err != nil {
		t.Fatal("exact trusted program could not repair its receipt", err)
	}
	if status := InspectCLI(context.Background(), directory); status.State != CLIManaged {
		t.Fatal(status)
	}
}
