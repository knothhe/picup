package gui

import (
	"reflect"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
)

const settingsAutosaveDelay = 600 * time.Millisecond

type settingsSaveState struct {
	initialized         bool
	observed, persisted config.Config
	pending, failed     bool
	due                 time.Time
	err                 error
}

func cloneSettings(c config.Config) config.Config {
	return c.Clone()
}

func (a *application) currentSettings() config.Config {
	c := a.config
	c.Quality, c.MaxWidth = int(a.quality), int(a.maxWidth)
	if len(c.Profiles) > 0 {
		c = c.Clone()
		c.EnsureProfiles()
	}
	return c
}

// Initialize before controls are built so merely opening a page writes nothing.
func (a *application) initSettingsAutosave() {
	if a.settingsSave.initialized {
		return
	}
	c := cloneSettings(a.currentSettings())
	a.settingsSave = settingsSaveState{initialized: true, observed: c, persisted: cloneSettings(c)}
}

func (a *application) watchSettingsAutosave(c *ui.Context) {
	current := a.currentSettings()
	s := &a.settingsSave
	if !reflect.DeepEqual(current, s.observed) {
		s.observed = cloneSettings(current)
		s.pending = !reflect.DeepEqual(current, s.persisted)
		s.failed = false
		s.due = c.Now().Add(settingsAutosaveDelay)
	}
	if !s.pending {
		return
	}
	if remaining := s.due.Sub(c.Now()); remaining > 0 {
		c.After(remaining)
		return
	}
	a.save()
}

// Flush the last edit when quitting before the debounce deadline.
func (a *application) flushSettingsAutosave() bool {
	if a.settingsSave.initialized && !reflect.DeepEqual(a.currentSettings(), a.settingsSave.persisted) {
		a.save()
	}
	return !a.settingsSave.failed
}
