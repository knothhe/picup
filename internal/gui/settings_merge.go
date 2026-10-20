package gui

import (
	"errors"
	"fmt"
	"os"
	"reflect"

	"github.com/knothhe/picup/internal/config"
)

// Keep edits made in the current editor while retaining settings changed by
// another process since the editor's last saved snapshot.
func (a *application) settingsForSave() (config.Config, error) {
	current := a.currentSettings().Clone()
	current.EnsureProfiles()
	if _, err := os.Stat(a.path); errors.Is(err, os.ErrNotExist) {
		return current, nil
	} else if err != nil {
		return current, fmt.Errorf("read saved configuration: %w", err)
	}
	latest, err := config.Load(a.path)
	if err != nil {
		return current, err
	}
	if !a.settingsSave.initialized {
		return current, nil
	}
	baseline := a.settingsSave.persisted.Clone()
	baseline.EnsureProfiles()
	if err := latest.SwitchProfile(current.ActiveProfile); err != nil {
		return current, errors.New("the current profile was removed from the saved configuration; reload settings before saving")
	}
	if baseline.ActiveProfile != current.ActiveProfile {
		if err := baseline.SwitchProfile(current.ActiveProfile); err != nil {
			return current, errors.New("the current profile has no saved baseline; reload settings before saving")
		}
	}
	before, edited := reflect.ValueOf(baseline), reflect.ValueOf(current)
	destination := reflect.ValueOf(&latest).Elem()
	for i := 0; i < edited.NumField(); i++ {
		name := edited.Type().Field(i).Name
		if name == "Profiles" || name == "ActiveProfile" {
			continue
		}
		if !reflect.DeepEqual(before.Field(i).Interface(), edited.Field(i).Interface()) {
			destination.Field(i).Set(edited.Field(i))
		}
	}
	latest.EnsureProfiles()
	return latest, nil
}
