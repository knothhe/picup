package gui

import (
	"errors"
	"reflect"
	"strings"

	"github.com/egoist/mygo"
	"github.com/knothhe/picup/internal/config"
)

type profileUIState struct {
	editOpen, deleteOpen    bool
	menuOpen                bool
	mode, name, target, err string
}

func (a *application) profileSwitchBlocked() bool {
	return a.busy || a.uploading || a.gallery.operation || a.r2.active || a.r2.working
}

// Save the current editor before changing profile. Persist the new selection
// before publishing it to the views, so a failed write leaves the old target.
func (a *application) changeProfile(change func(*config.Config) error) bool {
	if a.profileSwitchBlocked() {
		return false
	}
	if !a.flushSettingsAutosave() {
		a.profiles.err = tr("保存失败，输入已保留；请重试后切换配置。")
		return false
	}
	c, err := a.settingsForSave()
	if err != nil {
		a.profiles.err = profileError(err)
		a.feedback("保存配置失败，当前配置未更改", err)
		return false
	}
	c.EnsureProfiles()
	if err := change(&c); err != nil {
		a.profiles.err = profileError(err)
		return false
	}
	c.EnsureProfiles()
	if err := config.Save(a.path, c); err != nil {
		a.profiles.err = tr("配置未更改，请重试。") + " " + err.Error()
		a.feedback("保存配置失败，当前配置未更改", err)
		return false
	}
	a.applyProfileConfig(c)
	a.profiles.err = ""
	if a.tab == 4 && !a.gallery.loaded && !a.gallery.busy {
		a.enterGallery()
	}
	return true
}

func profileError(err error) string {
	// Configuration errors are translated without touching user supplied names.
	if err.Error() == "profile name is required" {
		return tr("配置名称不能为空。")
	}
	if strings.HasPrefix(err.Error(), "profile name ") && strings.HasSuffix(err.Error(), " already exists") {
		return tr("配置名称已存在，请使用其他名称。")
	}
	if err.Error() == "at least one profile is required" {
		return tr("至少保留一个配置。")
	}
	if err.Error() == "disconnect Cloudflare before deleting its last profile" {
		return tr("请先在存储设置中断开 Cloudflare，再删除此连接的最后一个配置。")
	}
	return tr(err.Error())
}

func (a *application) switchProfile(id string) bool {
	if id == a.config.ActiveProfile {
		return true
	}
	return a.changeProfile(func(c *config.Config) error { return c.SwitchProfile(id) })
}

func (a *application) applyProfileConfig(c config.Config) {
	changed := a.config.ActiveProfile != c.ActiveProfile
	storageChanged := !sameGalleryStorage(a.config, c)
	if changed {
		a.flushGalleryPreferences()
	}
	a.config = c.Clone()
	if a.r2.initialized {
		a.loadR2ConnectionInfo()
	}
	a.quality, a.maxWidth = float64(c.Quality), float64(c.MaxWidth)
	a.uploadConfigGeneration++
	saved := c.Clone()
	a.savedConfig = &saved
	a.settingsSave = settingsSaveState{initialized: true, observed: c.Clone(), persisted: c.Clone()}
	if changed {
		a.gallery.stop()
		a.gallery = galleryState{generation: a.gallery.generation + 1}
		if err := a.restoreGalleryPreferences(); err != nil {
			a.feedback("读取图库偏好失败", err)
		}
	} else if storageChanged {
		a.resetGallery()
	}
	a.applyLanguage(false)
	a.applyNativeSettings()
	if a.win != nil {
		mygo.Theme.SetSource(mygo.ThemeSource(c.Appearance))
		a.win.Invalidate()
	}
}

// Focus reloads may update the saved target, but never replace pending edits.
func (a *application) canRefreshProfileConfig() bool {
	return !a.profileSwitchBlocked() && (!a.settingsSave.initialized || reflect.DeepEqual(a.currentSettings(), a.settingsSave.persisted))
}

func (a *application) confirmProfileEdit() bool {
	mode, name, target := a.profiles.mode, a.profiles.name, a.profiles.target
	return a.changeProfile(func(c *config.Config) error {
		switch mode {
		case "new", "copy":
			if mode == "copy" && target != "" {
				if err := c.SwitchProfile(target); err != nil {
					return err
				}
			}
			_, err := c.AddProfile(name, mode == "copy")
			return err
		case "rename":
			return c.RenameProfile(target, name)
		}
		return errors.New("unknown profile action")
	})
}
