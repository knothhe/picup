package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/knothhe/picup/internal/buildidentity"
	"github.com/knothhe/picup/internal/config"
)

// On Linux the Dev application is a directory holding the GUI next to its
// companion CLI, the layout Source finds, registered with an XDG desktop entry.
const linuxDevelopmentDirectory = "picup-dev"
const linuxDevelopmentGUI = "picup-gui"
const linuxDevelopmentIcon = "icon.png"

var linuxDevelopmentFiles = []string{linuxDevelopmentGUI, "picup-dev", linuxDevelopmentIcon, "THIRD_PARTY_NOTICES.txt"}

func xdgDirectory(variable, fallback string) (string, error) {
	if value := os.Getenv(variable); filepath.IsAbs(value) {
		return filepath.Clean(value), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fallback), nil
}

func linuxDesktopEntryPath() (string, error) {
	directory, err := xdgDirectory("XDG_DATA_HOME", ".local/share")
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "applications", buildidentity.DevelopmentIdentifier+".desktop"), nil
}

// desktopExec quotes a path for an Exec key: the desktop entry specification's
// argument quoting, then its string escaping, as MyGo's autostart entries do.
func desktopExec(path string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range path {
		switch r {
		case '"', '`', '$':
			b.WriteString(`\\`)
			b.WriteRune(r)
		case '\\':
			b.WriteString(`\\\\`)
		case '%':
			b.WriteString("%%")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func linuxDesktopEntry(app string) ([]byte, error) {
	if strings.ContainsFunc(app, func(r rune) bool { return r < ' ' }) {
		return nil, errors.New("installation path contains control characters")
	}
	return []byte("[Desktop Entry]\nType=Application\nName=Picup Dev\n" +
		"Exec=" + desktopExec(filepath.Join(app, linuxDevelopmentGUI)) + "\n" +
		"Icon=" + filepath.Join(app, linuxDevelopmentIcon) + "\n" +
		"Categories=Utility;\nTerminal=false\n"), nil
}

func inspectLinuxDevelopment(app string) (developmentRecord, error) {
	var record developmentRecord
	info, err := os.Lstat(app)
	if err != nil {
		return record, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return record, errors.New("development directory is not a real directory")
	}
	entries, err := os.ReadDir(app)
	if err != nil {
		return record, err
	}
	for _, entry := range entries {
		if !slices.Contains(linuxDevelopmentFiles, entry.Name()) {
			return record, fmt.Errorf("development directory holds an unexpected file: %s", filepath.Join(app, entry.Name()))
		}
	}
	data := map[string][]byte{}
	for _, name := range linuxDevelopmentFiles {
		if data[name], err = regularData(filepath.Join(app, name)); err != nil {
			return record, err
		}
	}
	for _, name := range []string{linuxDevelopmentGUI, "picup-dev"} {
		metadata, err := readDevelopmentBuildInfo(filepath.Join(app, name))
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
				osMatch = setting.Value == "linux"
			case "GOARCH":
				archMatch = setting.Value == runtime.GOARCH
			}
		}
		if !development || !osMatch || !archMatch {
			return record, errors.New("directory channel or architecture does not match this Dev installation")
		}
	}
	return developmentRecord{Schema: 1, Platform: "linux", Identifier: buildidentity.DevelopmentIdentifier, App: app,
		GUIHash: hash(data[linuxDevelopmentGUI]), CLIHash: hash(data["picup-dev"])}, nil
}

func copyLinuxDevelopment(source, destination string) error {
	if err := os.Mkdir(destination, 0755); err != nil {
		return err
	}
	for _, name := range linuxDevelopmentFiles {
		data, err := regularData(filepath.Join(source, name))
		if err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if name == linuxDevelopmentGUI || name == "picup-dev" {
			mode = 0755
		}
		if err := os.WriteFile(filepath.Join(destination, name), data, mode); err != nil {
			return err
		}
	}
	return nil
}

// checkLinuxDesktopEntry accepts a missing entry, the exact entry this
// installation writes, or the entry a receipt recorded.
func checkLinuxDesktopEntry(path string, expected []byte, recorded string) error {
	data, err := regularData(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if string(data) == string(expected) || (recorded != "" && hash(data) == recorded) {
		return nil
	}
	return fmt.Errorf("%s is not managed by this Picup Dev installation; move it away and retry", path)
}

// removeLinuxAutostart removes the XDG autostart entries that start this exact
// GUI, which the application's open-at-login setting creates.
func removeLinuxAutostart(gui string) error {
	directory, err := xdgDirectory("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return err
	}
	directory = filepath.Join(directory, "autostart")
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	command := desktopExec(gui)
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		if !strings.HasSuffix(entry.Name(), ".desktop") {
			continue
		}
		data, err := regularData(path)
		if err != nil {
			continue // Never follow or remove what this installation did not write.
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			value, ok := strings.CutPrefix(strings.TrimSpace(line), "Exec=")
			if ok && (value == command || strings.HasPrefix(value, command+" ")) {
				if err := os.Remove(path); err != nil {
					return fmt.Errorf("Dev login item removal failed; application preserved: %w", err)
				}
				break
			}
		}
	}
	return nil
}

func installLinuxDevelopment(source, appDir, cliDir string) error {
	app, cli, err := developmentTargets(appDir, cliDir)
	if err != nil {
		return err
	}
	desktop, err := linuxDesktopEntryPath()
	if err != nil {
		return err
	}
	entry, err := linuxDesktopEntry(app)
	if err != nil {
		return err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return err
	}
	if source == app || overlap(app, source) {
		return errors.New("source and installed directory must be separate")
	}
	if _, err := inspectLinuxDevelopment(source); err != nil {
		return err
	}
	if _, err := os.Lstat(app); err == nil {
		if _, err := inspectLinuxDevelopment(app); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	receiptPath := filepath.Join(filepath.Dir(app), developmentReceipt)
	recorded := ""
	if data, err := regularData(receiptPath); err == nil {
		var previous developmentRecord
		if json.Unmarshal(data, &previous) == nil && previous.Platform == "linux" && previous.Desktop == desktop {
			recorded = previous.DesktopHash
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := checkLinuxDesktopEntry(desktop, entry, recorded); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(app), 0755); err != nil {
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
	staged := filepath.Join(stage, linuxDevelopmentDirectory)
	if err := copyLinuxDevelopment(source, staged); err != nil {
		return err
	}
	record, err := inspectLinuxDevelopment(staged)
	if err != nil {
		return err
	}
	if _, _, _, _, err := prepareCLIInstall(filepath.Join(staged, "picup-dev"), filepath.Dir(cli), "picup-dev"); err != nil {
		return err
	}
	// Complete all preparation before asking the exact old instance to quit.
	if _, err := os.Lstat(app); err == nil {
		if err := stopDevelopment(filepath.Join(app, linuxDevelopmentGUI), 30*time.Second); err != nil {
			return err
		}
		if _, err := inspectLinuxDevelopment(app); err != nil {
			return err
		}
	}
	installedCLI, err := InstallNamed(filepath.Join(staged, "picup-dev"), filepath.Dir(cli), "picup-dev")
	if err != nil {
		return err
	}
	backup := filepath.Join(stage, "previous")
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
	record.App, record.CLI, record.Desktop, record.DesktopHash = app, cli, desktop, hash(entry)
	if err := os.MkdirAll(filepath.Dir(desktop), 0755); err != nil {
		return fmt.Errorf("Dev application and CLI installed, but the desktop entry could not be created: %w", err)
	}
	if err := config.AtomicWrite(desktop, entry, 0644); err != nil {
		return fmt.Errorf("Dev application and CLI installed, but the desktop entry could not be saved: %w", err)
	}
	encoded, _ := json.MarshalIndent(record, "", "  ")
	if err := config.AtomicWrite(receiptPath, encoded, 0600); err != nil {
		return fmt.Errorf("Dev application and CLI installed, but ownership receipt could not be saved; reinstall to repair: %w", err)
	}
	return nil
}

func uninstallLinuxDevelopment(appDir, cliDir string) error {
	app, cli, err := developmentTargets(appDir, cliDir)
	if err != nil {
		return err
	}
	desktop, err := linuxDesktopEntryPath()
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
	if json.Unmarshal(data, &record) != nil || record.Schema != 1 || record.Platform != "linux" || record.Identifier != buildidentity.DevelopmentIdentifier ||
		record.App != app || record.CLI != cli || record.Desktop != desktop {
		return errors.New("development ownership receipt does not match these exact installation paths")
	}
	appExists, cliExists, desktopExists := false, false, false
	if _, err := os.Lstat(app); err == nil {
		actual, err := inspectLinuxDevelopment(app)
		if err != nil {
			return err
		}
		if actual.GUIHash != record.GUIHash || actual.CLIHash != record.CLIHash {
			return errors.New("Dev application changed since installation; reinstall to verify ownership")
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
	if bytes, err := regularData(desktop); err == nil {
		if hash(bytes) != record.DesktopHash {
			return errors.New("Dev desktop entry changed since installation; refusing to delete it")
		}
		desktopExists = true
	} else if !os.IsNotExist(err) {
		return err
	}
	gui := filepath.Join(app, linuxDevelopmentGUI)
	if appExists {
		if err := stopDevelopment(gui, 30*time.Second); err != nil {
			return err
		}
		actual, err := inspectLinuxDevelopment(app)
		if err != nil {
			return err
		}
		if actual.GUIHash != record.GUIHash || actual.CLIHash != record.CLIHash {
			return errors.New("Dev application changed during shutdown; uninstall stopped")
		}
	}
	if err := removeLinuxAutostart(gui); err != nil {
		return err
	}
	if desktopExists {
		if err := removeDevelopmentFile(desktop); err != nil {
			return err
		}
	}
	if appExists {
		if err := removeDevelopmentBundle(app); err != nil {
			return err
		}
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
