// Local installation of the macOS app and its companion CLI.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/knothhe/picup/internal/install"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "install:", err)
		os.Exit(1)
	}
}

func run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	appDir := flag.String("app-dir", filepath.Join(home, "Applications"), "desktop application directory")
	cliDir := flag.String("cli-dir", install.DefaultDir(), "CLI installation directory")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flag.Args())
	}
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("make install currently supports macOS; use make package on other platforms")
	}
	if !filepath.IsAbs(*appDir) || !filepath.IsAbs(*cliDir) {
		return fmt.Errorf("installation directories must be absolute paths")
	}
	data, err := os.ReadFile("mygo.json")
	if err != nil {
		return err
	}
	var config struct {
		Name, Identifier, Out string
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}
	if config.Name != "Picup" || config.Identifier != "com.knothhe.picup" || config.Out == "" {
		return fmt.Errorf("unexpected application configuration in mygo.json")
	}
	dst := filepath.Join(*appDir, config.Name+".app")
	if err := checkDestination(dst, config.Identifier); err != nil {
		return err
	}
	goCommand := os.Getenv("GO")
	if goCommand == "" {
		goCommand = "go"
	}
	if err := command("make", "build", "GO="+goCommand); err != nil {
		return err
	}
	if err := command(goCommand, "tool", "mygo", "build", "-skip-dmg"); err != nil {
		return err
	}
	src := filepath.Join(config.Out, "darwin-"+runtime.GOARCH, config.Name+".app")
	if err := checkBundle(src, config.Identifier); err != nil {
		return err
	}
	if err := os.MkdirAll(*appDir, 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(*appDir, ".picup-install-")
	if err != nil {
		return err
	}
	defer func() {
		if stage != "" {
			os.RemoveAll(stage)
		}
	}()
	staged := filepath.Join(stage, config.Name+".app")
	if err := command("/usr/bin/ditto", src, staged); err != nil {
		return err
	}
	// Reuse the GUI installer, including its receipt and overwrite protection.
	cli, err := install.Install(filepath.Join(staged, "Contents", "Resources", install.Filename()), *cliDir)
	if err != nil {
		return err
	}
	if err := checkDestination(dst, config.Identifier); err != nil {
		return fmt.Errorf("CLI installed at %s, desktop unchanged: %w", cli, err)
	}
	backup := filepath.Join(stage, "previous.app")
	hadPrevious := false
	if err := os.Rename(dst, backup); err == nil {
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("CLI installed at %s, desktop unchanged: %w", cli, err)
	}
	if err := os.Rename(staged, dst); err != nil {
		if hadPrevious {
			if restore := os.Rename(backup, dst); restore != nil {
				// Keep the backup if restoring fails.
				stage = ""
				return fmt.Errorf("CLI installed at %s; desktop install failed: %v; restore failed: %v; backup: %s", cli, err, restore, backup)
			}
		}
		return fmt.Errorf("CLI installed at %s, desktop install failed: %w", cli, err)
	}
	fmt.Println("Installed desktop:", dst)
	fmt.Println("Installed CLI:", cli)
	if !install.OnPath(*cliDir) {
		fmt.Printf("Add %s to PATH to use picup from your shell.\n", *cliDir)
	}
	return nil
}

func command(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	return cmd.Run()
}

func checkDestination(path, identifier string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to replace non-directory or symlink: %s", path)
	}
	return checkBundle(path, identifier)
}

func checkBundle(path, identifier string) error {
	output, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Print :CFBundleIdentifier", filepath.Join(path, "Contents", "Info.plist")).Output()
	if err != nil || strings.TrimSpace(string(output)) != identifier {
		return fmt.Errorf("%s is not a Picup application; choose another app directory", path)
	}
	return nil
}
