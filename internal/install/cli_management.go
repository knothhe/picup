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
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/knothhe/picup/internal/config"
)

type CLIInstallationState string

const (
	CLIMissing        CLIInstallationState = "missing"
	CLIManaged        CLIInstallationState = "managed"
	CLIUnmanaged      CLIInstallationState = "unmanaged"
	CLIForeign        CLIInstallationState = "foreign"
	CLIChanged        CLIInstallationState = "changed"
	CLIBlocked        CLIInstallationState = "blocked"
	CLIRemovalPending CLIInstallationState = "removal-pending"
)

type CLIInstallation struct {
	State                            CLIInstallationState
	Path, Version, CommandPath       string
	InPath, CanInstall, CanUninstall bool
	Err                              error
}

var cliVersionPattern = regexp.MustCompile(`^picup [0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
var receiptHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var readCLIInfo = buildinfo.ReadFile
var removeCLIFile = os.Remove
var inspectCLIVersion = func(ctx context.Context, executable string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "--version")
	// The known --version path has no need for cloud or user configuration.
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C"}
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(output))
	if !cliVersionPattern.MatchString(text) {
		return "", errors.New("installed CLI returned an invalid version")
	}
	return strings.TrimPrefix(text, "picup "), nil
}

func cliChannel(filename string) string {
	if filename == "picup-dev" || filename == "picup-dev.exe" {
		return "development"
	}
	return "release"
}
func cliMarker(filename string) string {
	if cliChannel(filename) == "development" {
		return ".picup-dev-cli.json"
	}
	return ".picup-cli.json"
}
func validCLIReceipt(record receipt, path, filename string) bool {
	if !receiptHashPattern.MatchString(record.Hash) {
		return false
	}
	if record.Schema == 0 {
		return record.Path == "" && record.Channel == "" && !record.UninstallPending
	}
	return record.Schema == 2 && record.Path == path && record.Channel == cliChannel(filename)
}

func cliPaths(directory, filename string) (string, string, error) {
	directory, err := expandHome(directory)
	if err != nil {
		return "", "", err
	}
	directory, err = checkedInstallDirectory(directory)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(directory, filename), filepath.Join(directory, cliMarker(filename)), nil
}

func isPicupCLIInfo(info *debug.BuildInfo, filename string) error {
	if info == nil || info.Path != "github.com/knothhe/picup/cmd/picup" || info.Main.Path != "github.com/knothhe/picup" {
		return errors.New("file is not the Picup command-line program")
	}
	development := false
	for _, setting := range info.Settings {
		if setting.Key == "-tags" {
			for _, tag := range strings.FieldsFunc(setting.Value, func(r rune) bool { return r == ',' || r == ' ' }) {
				development = development || tag == "picup_dev"
			}
		}
	}
	if development != (cliChannel(filename) == "development") {
		return errors.New("CLI belongs to a different Picup build channel")
	}
	return nil
}

// IsPicupCLI reads executable metadata without running the file.
func IsPicupCLI(path, filename string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("CLI is not a regular executable file")
	}
	if info.Mode().Perm()&0111 == 0 && runtime.GOOS != "windows" {
		return errors.New("CLI file is not executable")
	}
	metadata, err := readCLIInfo(path)
	if err != nil {
		return fmt.Errorf("cannot verify Picup CLI code identity: %w", err)
	}
	return isPicupCLIInfo(metadata, filename)
}

// InspectCLI never executes an unowned or modified command. Only a matching
// receipt plus Picup module/channel identity permits querying --version.
func InspectCLI(ctx context.Context, directory string) CLIInstallation {
	filename := Filename()
	status := CLIInstallation{State: CLIBlocked}
	path, marker, err := cliPaths(directory, filename)
	status.Path = path
	if err != nil {
		status.Err = err
		return status
	}
	status.InPath = OnPath(filepath.Dir(path))
	if command, err := exec.LookPath(filename); err == nil {
		if real, err := filepath.EvalSymlinks(command); err == nil {
			status.CommandPath = real
		} else {
			status.CommandPath = command
		}
	}
	data, err := regularData(path)
	if os.IsNotExist(err) {
		status.State = CLIMissing
		status.CanInstall = true
		if encoded, readErr := regularData(marker); readErr == nil {
			var record receipt
			if json.Unmarshal(encoded, &record) == nil && validCLIReceipt(record, path, filename) && record.Schema == 2 && record.UninstallPending {
				status.State = CLIRemovalPending
				status.CanUninstall = true
			} else {
				status.State = CLIUnmanaged
				status.Err = errors.New("CLI is absent but its installation receipt has unknown ownership")
			}
		} else if !os.IsNotExist(readErr) {
			status.State = CLIBlocked
			status.CanInstall = false
			status.Err = readErr
		}
		return status
	}
	if err != nil {
		status.Err = err
		return status
	}
	if err := IsPicupCLI(path, filename); err != nil {
		status.State = CLIForeign
		status.Err = err
		return status
	}
	encoded, err := regularData(marker)
	if err != nil {
		status.State = CLIUnmanaged
		status.CanInstall = os.IsNotExist(err)
		if !os.IsNotExist(err) {
			status.Err = err
		}
		return status
	}
	var record receipt
	if json.Unmarshal(encoded, &record) != nil || !validCLIReceipt(record, path, filename) {
		status.State = CLIUnmanaged
		status.Err = errors.New("CLI receipt has unknown schema, channel or installation path")
		return status
	}
	if record.Hash != hash(data) {
		status.State = CLIChanged
		status.Err = errors.New("CLI changed since installation")
		return status
	}
	status.State = CLIManaged
	status.CanInstall = true
	status.CanUninstall = true
	if version, err := inspectCLIVersion(ctx, path); err == nil {
		status.Version = version
	} else {
		status.Err = err
	}
	return status
}

// InspectCLIWithSource recognizes an incomplete receipt only when installed
// bytes exactly match the code-verified bundled companion. No unknown file runs.
func InspectCLIWithSource(ctx context.Context, directory, source string) CLIInstallation {
	status := InspectCLI(ctx, directory)
	if status.State != CLIChanged {
		return status
	}
	if IsPicupCLI(source, Filename()) != nil {
		return status
	}
	bundled, err := regularData(source)
	if err != nil {
		return status
	}
	installed, err := regularData(status.Path)
	if err != nil {
		return status
	}
	if hash(bundled) == hash(installed) {
		status.CanInstall = true
	}
	return status
}

// InstallCLI validates the bundled source before using the shared installer.
func InstallCLI(ctx context.Context, source, directory string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := IsPicupCLI(source, Filename()); err != nil {
		return err
	}
	metadata, err := readCLIInfo(source)
	if err != nil {
		return err
	}
	osMatches, archMatches := false, false
	for _, setting := range metadata.Settings {
		if setting.Key == "GOOS" && setting.Value != runtime.GOOS {
			return errors.New("bundled CLI targets another operating system")
		}
		if setting.Key == "GOARCH" && setting.Value != runtime.GOARCH {
			return errors.New("bundled CLI targets another CPU architecture")
		}
		osMatches = osMatches || setting.Key == "GOOS" && setting.Value == runtime.GOOS
		archMatches = archMatches || setting.Key == "GOARCH" && setting.Value == runtime.GOARCH
	}
	if !osMatches || !archMatches {
		return errors.New("bundled CLI is missing operating system or CPU build metadata")
	}
	path, _, err := cliPaths(directory, Filename())
	if err != nil {
		return err
	}
	_, err = InstallNamed(source, filepath.Dir(path), Filename())
	return err
}

// UninstallCLI removes only a code-verified, unchanged installation for the
// current channel. Data, credentials, history and the desktop app are untouched.
func UninstallCLI(ctx context.Context, directory string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, marker, err := cliPaths(directory, Filename())
	if err != nil {
		return err
	}
	encoded, err := regularData(marker)
	if os.IsNotExist(err) {
		if _, statErr := os.Lstat(path); os.IsNotExist(statErr) {
			return nil
		}
		return errors.New("CLI ownership receipt is missing; reinstall the same bundled version or choose another directory")
	}
	if err != nil {
		return err
	}
	var record receipt
	if json.Unmarshal(encoded, &record) != nil || !validCLIReceipt(record, path, Filename()) {
		return errors.New("CLI ownership receipt does not match this exact installation and build channel")
	}
	data, err := regularData(path)
	if os.IsNotExist(err) {
		if record.Schema == 2 && record.UninstallPending {
			return removeCLIFile(marker)
		}
		return errors.New("CLI is missing and removal ownership is unverified; reinstall to repair the receipt")
	}
	if err != nil {
		return err
	}
	if err := IsPicupCLI(path, Filename()); err != nil {
		return err
	}
	if record.Hash != hash(data) {
		return errors.New("CLI changed since installation; refusing to remove it")
	}
	// Persist retry ownership before removing the executable. If receipt removal
	// then fails, a retry can clean it up without guessing from a missing file.
	record.Schema = 2
	record.Path = path
	record.Channel = cliChannel(Filename())
	record.UninstallPending = true
	encoded, _ = json.Marshal(record)
	if err := config.AtomicWrite(marker, encoded, 0600); err != nil {
		return err
	}
	if err := removeCLIFile(path); err != nil {
		return err
	}
	if err := removeCLIFile(marker); err != nil {
		return fmt.Errorf("CLI removed but its ownership receipt remains; retry uninstall: %w", err)
	}
	return nil
}
