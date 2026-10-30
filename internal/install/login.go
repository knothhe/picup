package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/knothhe/picup/internal/buildidentity"
	"github.com/knothhe/picup/internal/config"
)

// SetLinuxReleaseOpenAtLogin keeps the launch path stable after an update
// replaces the running executable. MyGo reads /proc/self/exe again when writing
// its Linux autostart entry, which can then name the deleted old application.
func SetLinuxReleaseOpenAtLogin(enabled bool) error {
	directory, err := xdgDirectory("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return err
	}
	path := filepath.Join(directory, "autostart", buildidentity.ReleaseIdentifier+".desktop")
	if !enabled {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if companionExecutableError != nil {
		return companionExecutableError
	}
	executable := companionExecutable
	if !filepath.IsAbs(executable) || filepath.Base(executable) != "picup-gui" || strings.ContainsFunc(executable, func(r rune) bool { return r < ' ' }) {
		return errors.New("cannot determine the installed Linux release executable")
	}
	info, err := os.Lstat(executable)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("Linux release executable is not a regular executable: %s", executable)
	}
	entry := "[Desktop Entry]\nType=Application\nName=Picup\nExec=" + desktopExec(executable) +
		" --mygo-opened-at-login\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"
	return config.AtomicWrite(path, []byte(entry), 0644)
}
