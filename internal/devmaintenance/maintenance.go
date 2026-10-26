// Package devmaintenance provides a windowless, identity-checked maintenance
// mode before the desktop loads configuration or obtains its instance lock.
package devmaintenance

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/knothhe/picup/internal/buildidentity"
)

const Protocol = 1
const InfoArgument = "--picup-dev-maintenance-info"
const UnregisterArgument = "--picup-dev-unregister-login"

type Information struct {
	Protocol   int    `json:"protocol"`
	Identifier string `json:"identifier"`
	Executable string `json:"executable"`
}

func clearLogin(status func() (int, error), unregister func() error) error {
	current, err := status()
	if err != nil {
		return err
	}
	switch current {
	case 0, 3:
		return nil // SMAppService NotRegistered / NotFound own no login item.
	case 1, 2:
		if err := unregister(); err != nil {
			return err
		}
		after, err := status()
		if err != nil {
			return err
		}
		if after != 0 && after != 3 {
			return fmt.Errorf("Dev login item remains registered (status %d)", after)
		}
		return nil
	default:
		return fmt.Errorf("unknown Dev login item status %d; removal stopped", current)
	}
}

func Run(arguments []string, output io.Writer) (bool, error) {
	if len(arguments) != 1 || (arguments[0] != InfoArgument && arguments[0] != UnregisterArgument) {
		return false, nil
	}
	if !buildidentity.Development() {
		return true, errors.New("maintenance is restricted to packaged development applications")
	}
	executable, err := os.Executable()
	if err != nil {
		return true, err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return true, err
	}
	if err := verifyOwnBundle(executable); err != nil {
		return true, err
	}
	if arguments[0] == InfoArgument {
		return true, json.NewEncoder(output).Encode(Information{Protocol, buildidentity.DevelopmentIdentifier, executable})
	}
	if err := clearLogin(loginStatus, unregisterLogin); err != nil {
		return true, err
	}
	return true, json.NewEncoder(output).Encode(map[string]any{"protocol": Protocol, "login_item": "unregistered"})
}
