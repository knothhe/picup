package install

import (
	"context"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/knothhe/picup/internal/buildidentity"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/devcontrol"
	"github.com/knothhe/picup/internal/devmaintenance"
)

const developmentAppName = "Picup Dev.app"
const developmentReceipt = ".picup-dev-install.json"

type developmentRecord struct {
	Schema            int    `json:"schema"`
	Protocol          int    `json:"maintenance_protocol"`
	Identifier        string `json:"identifier"`
	App               string `json:"app"`
	CLI               string `json:"cli"`
	GUIHash           string `json:"gui_sha256"`
	PlistHash         string `json:"plist_sha256"`
	CLIHash           string `json:"cli_sha256"`
	LoginUnregistered bool   `json:"login_item_unregistered,omitempty"`
	Platform          string `json:"platform,omitempty"`
	Desktop           string `json:"desktop_entry,omitempty"`
	DesktopHash       string `json:"desktop_entry_sha256,omitempty"`
}

var readDevelopmentBuildInfo = buildinfo.ReadFile
var stopDevelopment = devcontrol.Stop
var removeDevelopmentFile = os.Remove
var removeDevelopmentBundle = os.RemoveAll
var clearDevelopmentLogin = unregisterDevelopmentLogin
var copyDevelopmentBundle = func(source, destination string) error {
	return exec.Command("/usr/bin/ditto", source, destination).Run()
}

func checkedInstallDirectory(directory string) (string, error) {
	if !filepath.IsAbs(directory) {
		return "", errors.New("installation directories must be absolute paths")
	}
	directory = filepath.Clean(directory)
	ancestor := directory
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", err
		}
		next := filepath.Dir(ancestor)
		if next == ancestor {
			return "", errors.New("cannot resolve installation directory")
		}
		ancestor = next
	}
	real, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", err
	}
	// Resolve standard macOS /tmp and /var aliases, but reject user-created
	// aliases so the reported installation cannot redirect to another target.
	systemAlias := ancestor
	if ancestor == "/var" || strings.HasPrefix(ancestor, "/var/") || ancestor == "/tmp" || strings.HasPrefix(ancestor, "/tmp/") {
		systemAlias = "/private" + ancestor
	}
	if real != ancestor && real != systemAlias {
		return "", fmt.Errorf("installation directory resolves through a symlink; use its real path: %s", real)
	}
	relative, err := filepath.Rel(ancestor, directory)
	if err != nil {
		return "", err
	}
	return filepath.Join(real, relative), nil
}

func overlap(a, b string) bool {
	relative, err := filepath.Rel(a, b)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func developmentTargets(appDir, cliDir string) (string, string, error) {
	var err error
	if appDir, err = checkedInstallDirectory(appDir); err != nil {
		return "", "", err
	}
	if cliDir, err = checkedInstallDirectory(cliDir); err != nil {
		return "", "", err
	}
	app := filepath.Join(appDir, developmentAppName)
	if runtime.GOOS == "linux" {
		app = filepath.Join(appDir, linuxDevelopmentDirectory)
	}
	cli := filepath.Join(cliDir, "picup-dev")
	if overlap(app, cliDir) || overlap(app, cli) {
		return "", "", errors.New("CLI directory must not be inside the application bundle")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	dataRoot := filepath.Join(home, ".config")
	if override := os.Getenv("XDG_CONFIG_HOME"); override != "" {
		dataRoot = override
	}
	protected := []string{filepath.Join(dataRoot, "picup"), filepath.Join(dataRoot, "picup-dev")}
	if path := os.Getenv("PICUP_CONFIG"); path != "" {
		protected = append(protected, filepath.Dir(path))
	}
	for _, path := range protected {
		path, err = checkedInstallDirectory(path)
		if err != nil {
			return "", "", err
		}
		if overlap(app, path) || overlap(path, app) || overlap(cli, path) || overlap(path, cli) {
			return "", "", errors.New("development installation overlaps a configuration or data directory")
		}
	}
	return app, cli, nil
}

func regularData(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("expected an owned regular file: %s", path)
	}
	return os.ReadFile(path)
}

func inspectDevelopment(app string) (developmentRecord, error) {
	var record developmentRecord
	info, err := os.Lstat(app)
	if err != nil {
		return record, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return record, errors.New("development bundle is not a real directory")
	}
	realApp, err := filepath.EvalSymlinks(app)
	if err != nil {
		return record, err
	}
	for _, relative := range []string{"Contents/Info.plist", "Contents/MacOS/Picup Dev", "Contents/Resources/picup-dev"} {
		resolved, err := filepath.EvalSymlinks(filepath.Join(app, relative))
		if err != nil {
			return record, err
		}
		if resolved != filepath.Join(realApp, relative) {
			return record, errors.New("owned development files resolve outside this exact bundle through a symlink")
		}
	}
	output, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Print :CFBundleIdentifier", filepath.Join(app, "Contents/Info.plist")).Output()
	if err != nil || strings.TrimSpace(string(output)) != buildidentity.DevelopmentIdentifier {
		return record, errors.New("target is not the com.knothhe.picup.dev application")
	}
	output, err = exec.Command("/usr/libexec/PlistBuddy", "-c", "Print :CFBundleExecutable", filepath.Join(app, "Contents/Info.plist")).Output()
	if err != nil || strings.TrimSpace(string(output)) != "Picup Dev" {
		return record, errors.New("unexpected development executable identity")
	}
	paths := []string{filepath.Join(app, "Contents/MacOS/Picup Dev"), filepath.Join(app, "Contents/Resources/picup-dev")}
	for _, path := range paths {
		if _, err := regularData(path); err != nil {
			return record, err
		}
		metadata, err := readDevelopmentBuildInfo(path)
		if err != nil {
			return record, fmt.Errorf("cannot verify development build channel: %w", err)
		}
		development, osMatch, archMatch := false, false, false
		for _, setting := range metadata.Settings {
			switch setting.Key {
			case "-tags":
				for _, tag := range strings.FieldsFunc(setting.Value, func(r rune) bool { return r == ',' || r == ' ' }) {
					development = development || tag == "picup_dev"
				}
			case "GOOS":
				osMatch = setting.Value == "darwin"
			case "GOARCH":
				archMatch = setting.Value == runtime.GOARCH
			}
		}
		if !development || !osMatch || !archMatch {
			return record, errors.New("bundle channel or architecture does not match this Dev installation")
		}
	}
	gui, _ := regularData(paths[0])
	cli, _ := regularData(paths[1])
	plist, err := regularData(filepath.Join(app, "Contents/Info.plist"))
	if err != nil {
		return record, err
	}
	protocol := 0
	if output, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Print :PicupDevMaintenanceProtocol", filepath.Join(app, "Contents/Info.plist")).Output(); err == nil && strings.TrimSpace(string(output)) == "1" {
		protocol = 1
	}
	return developmentRecord{Schema: 1, Protocol: protocol, Identifier: buildidentity.DevelopmentIdentifier, App: app, GUIHash: hash(gui), PlistHash: hash(plist), CLIHash: hash(cli)}, nil
}

func unregisterDevelopmentLogin(executable string) error {
	canonical, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	output, err := exec.CommandContext(ctx, executable, devmaintenance.InfoArgument).CombinedOutput()
	cancel()
	if err != nil {
		return fmt.Errorf("Dev maintenance capability check failed; reinstall the current Dev build before uninstalling: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	var information devmaintenance.Information
	if json.Unmarshal(output, &information) != nil || information.Protocol != devmaintenance.Protocol || information.Identifier != buildidentity.DevelopmentIdentifier || information.Executable != canonical {
		return errors.New("Dev maintenance reported a different protocol or installed identity; uninstall stopped")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err = exec.CommandContext(ctx, executable, devmaintenance.UnregisterArgument).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Dev login item removal failed; application preserved: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	var response struct {
		Protocol  int    `json:"protocol"`
		LoginItem string `json:"login_item"`
	}
	if json.Unmarshal(output, &response) != nil || response.Protocol != devmaintenance.Protocol || response.LoginItem != "unregistered" {
		return errors.New("Dev maintenance did not confirm login item removal; application preserved")
	}
	return nil
}

// InstallDevelopment consumes a precise, validated Dev bundle. It creates no
// login item, protocol registration or user configuration.
func InstallDevelopment(source, appDir, cliDir string) error {
	if runtime.GOOS == "linux" {
		return installLinuxDevelopment(source, appDir, cliDir)
	}
	app, cli, err := developmentTargets(appDir, cliDir)
	if err != nil {
		return err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return err
	}
	if source == app || overlap(app, source) {
		return errors.New("source and installed bundle must be separate")
	}
	if record, err := inspectDevelopment(source); err != nil {
		return err
	} else if record.Protocol != devmaintenance.Protocol {
		return errors.New("source Dev bundle lacks the current maintenance protocol; rebuild with make package-dev")
	}
	if _, err := os.Lstat(app); err == nil {
		if _, err := inspectDevelopment(app); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(app), 0755); err != nil {
		return err
	}
	if _, err := regularData(filepath.Join(filepath.Dir(app), developmentReceipt)); err != nil && !os.IsNotExist(err) {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(app), ".picup-dev-install-")
	if err != nil {
		return err
	}
	defer func() {
		if stage != "" {
			os.RemoveAll(stage)
		}
	}()
	staged := filepath.Join(stage, developmentAppName)
	if err := copyDevelopmentBundle(source, staged); err != nil {
		return err
	}
	record, err := inspectDevelopment(staged)
	if err != nil {
		return err
	}
	if _, _, _, _, err := prepareCLIInstall(filepath.Join(staged, "Contents/Resources/picup-dev"), filepath.Dir(cli), "picup-dev"); err != nil {
		return err
	}
	// Complete all preparation before asking the exact old instance to quit.
	if _, err := os.Lstat(app); err == nil {
		if err := stopDevelopment(filepath.Join(app, "Contents/MacOS/Picup Dev"), 30*time.Second); err != nil {
			return err
		}
		if _, err := inspectDevelopment(app); err != nil {
			return err
		}
	}
	installedCLI, err := InstallNamed(filepath.Join(staged, "Contents/Resources/picup-dev"), filepath.Dir(cli), "picup-dev")
	if err != nil {
		return err
	}
	backup := filepath.Join(stage, "previous.app")
	hadPrevious := false
	if err := os.Rename(app, backup); err == nil {
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("Dev CLI installed at %s; desktop unchanged: %w", installedCLI, err)
	}
	if err := os.Rename(staged, app); err != nil {
		if hadPrevious {
			if restore := os.Rename(backup, app); restore != nil {
				stage = ""
				return fmt.Errorf("Dev installation failed: %v; restore failed: %v; backup: %s", err, restore, backup)
			}
		}
		return err
	}
	record.App, record.CLI = app, cli
	encoded, _ := json.MarshalIndent(record, "", "  ")
	if err := config.AtomicWrite(filepath.Join(filepath.Dir(app), developmentReceipt), encoded, 0600); err != nil {
		return fmt.Errorf("Dev application and CLI installed, but ownership receipt could not be saved; reinstall to repair: %w", err)
	}
	return nil
}

// UninstallDevelopment preserves all configuration, user data and credentials.
// A missing bundle never authorizes deleting an unverified CLI or receipt.
func UninstallDevelopment(appDir, cliDir string) error {
	if runtime.GOOS == "linux" {
		return uninstallLinuxDevelopment(appDir, cliDir)
	}
	app, cli, err := developmentTargets(appDir, cliDir)
	if err != nil {
		return err
	}
	receiptPath := filepath.Join(filepath.Dir(app), developmentReceipt)
	data, err := regularData(receiptPath)
	if os.IsNotExist(err) {
		_, appErr := os.Lstat(app)
		_, cliErr := os.Lstat(cli)
		if os.IsNotExist(appErr) && os.IsNotExist(cliErr) {
			return nil
		}
		return errors.New("Dev ownership receipt is missing; reinstall this Dev application at the same paths before uninstalling")
	}
	if err != nil {
		return err
	}
	var record developmentRecord
	if json.Unmarshal(data, &record) != nil || record.Schema != 1 || record.Protocol != devmaintenance.Protocol || record.Identifier != buildidentity.DevelopmentIdentifier || record.App != app || record.CLI != cli {
		return errors.New("development ownership receipt does not match these exact installation paths")
	}
	appExists, cliExists := false, false
	if _, err := os.Lstat(app); err == nil {
		actual, err := inspectDevelopment(app)
		if err != nil {
			return err
		}
		if actual.GUIHash != record.GUIHash || actual.PlistHash != record.PlistHash || actual.CLIHash != record.CLIHash {
			return errors.New("Dev bundle changed since installation; reinstall to verify ownership")
		}
		appExists = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if bytes, err := regularData(cli); err == nil {
		if hash(bytes) != record.CLIHash {
			return errors.New("Dev CLI changed since installation; refusing to delete it")
		}
		cliExists = true
	} else if !os.IsNotExist(err) {
		return err
	}
	marker := filepath.Join(filepath.Dir(cli), ".picup-dev-cli.json")
	if bytes, err := regularData(marker); err == nil {
		var receipt receipt
		if json.Unmarshal(bytes, &receipt) != nil || receipt.Hash != record.CLIHash {
			return errors.New("Dev CLI receipt does not match installed ownership")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if appExists {
		if err := stopDevelopment(filepath.Join(app, "Contents/MacOS/Picup Dev"), 30*time.Second); err != nil {
			return err
		}
		actual, err := inspectDevelopment(app)
		if err != nil {
			return err
		}
		if actual.GUIHash != record.GUIHash || actual.PlistHash != record.PlistHash || actual.CLIHash != record.CLIHash {
			return errors.New("Dev bundle changed during shutdown; uninstall stopped")
		}
		if err := clearDevelopmentLogin(filepath.Join(app, "Contents/MacOS/Picup Dev")); err != nil {
			return err
		}
		record.LoginUnregistered = true
		updated, _ := json.MarshalIndent(record, "", "  ")
		if err := config.AtomicWrite(receiptPath, updated, 0600); err != nil {
			return fmt.Errorf("Dev login item removed, but recovery receipt could not be saved; application preserved: %w", err)
		}
		if err := removeDevelopmentBundle(app); err != nil {
			return err
		}
	} else if !record.LoginUnregistered {
		return errors.New("Dev application is missing and login item cleanup is unverified; reinstall at the same paths, then retry uninstall")
	}
	if cliExists {
		data, err := regularData(cli)
		if err != nil {
			return err
		}
		if hash(data) != record.CLIHash {
			return errors.New("Dev CLI changed during shutdown; refusing to delete it")
		}
		if err := removeDevelopmentFile(cli); err != nil {
			return err
		}
	}
	if err := removeDevelopmentFile(marker); err != nil && !os.IsNotExist(err) {
		return err
	}
	return removeDevelopmentFile(receiptPath)
}
