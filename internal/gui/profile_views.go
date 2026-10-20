package gui

import (
	"errors"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

func profileName(id, name string) string {
	if id == "default" && name == "Default" {
		return tr("默认配置")
	}
	return name
}

// The workspace owns one profile switcher, in the sidebar or compact titlebar.
func (a *application) storageIndicator(c *ui.Context, compact bool) {
	cfg := a.uploadDisplayConfig().Clone()
	cfg.EnsureProfiles()
	name := profileName(cfg.ActiveProfile, cfg.CurrentProfileName())
	summary := profileDestination(cfg.Provider, cfg.Bucket)
	trigger := ui.ButtonBase(c).Key("current-storage").Label(tr("切换配置")).MinWidth(0).Height(32).Padding(0, 10).Gap(6).Radius(7).AlignItems(ui.Center).Background(ui.Color{}).Disabled(a.profileSwitchBlocked())
	if compact {
		trigger.MaxWidth(120).Shrink(0).Height(30).Padding(0, 6)
	} else {
		trigger.FillWidth()
	}
	if trigger.Hovered() || a.profiles.menuOpen {
		trigger.Background(c.Theme().Surface)
	}
	trigger.Children(func() {
		text := components.Text(c, name).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…").FontSize(12)
		if !compact {
			text.Grow(1)
		}
		components.Icon(c, lucide.ChevronDown()).Size(10, 10).Shrink(0).TextColor(c.Theme().TextMuted)
	})
	tooltip := name + "\n" + summary
	if a.profileSwitchBlocked() {
		tooltip = tr("当前任务完成后可切换配置。")
	}
	components.Tooltip(c, trigger, tooltip)
	if trigger.Clicked() {
		a.profiles.menuOpen = !a.profiles.menuOpen
	}
	if trigger.Shortcut(0, ui.KeyDown) || trigger.Shortcut(0, ui.KeyUp) {
		a.profiles.menuOpen = true
	}
	if a.profileSwitchBlocked() {
		a.profiles.menuOpen = false
	}
	if compact != a.sidebarCollapsed {
		return
	}
	a.profileMenu(c, trigger, compact, cfg)
}

func profileDestination(provider, bucket string) string {
	if bucket == "" {
		return tr("尚未配置存储")
	}
	return strings.ToUpper(provider) + " · " + bucket
}

func (a *application) profileMenu(c *ui.Context, anchor ui.Element, compact bool, cfg config.Config) {
	components.Popover(c, anchor, &a.profiles.menuOpen, func(panel ui.Element) {
		width, height := c.Size()
		panel.Label(tr("配置菜单")).Width(280).MaxWidth(width - 24).Padding(5).Gap(4)
		if !compact {
			panel.AttachTo(anchor, ui.AnchorTopStart, ui.AnchorBottomStart).Margin(0, 0, 4, 0)
		}
		components.Text(c, tr("切换配置")).FontSize(10).TextColor(c.Theme().TextMuted).Padding(6, 10)
		listHeight := min(float32(len(cfg.Profiles))*56, min(float32(320), max(float32(112), height-200)))
		scrollView(c).Key("profile-options").FillWidth().Height(listHeight).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				for _, profile := range cfg.Profiles {
					option := ui.ButtonBase(c).Key(profile.ID).Label(profileName(profile.ID, profile.Name)).FillWidth().Height(54).Padding(8, 10).Gap(8).Radius(6).Background(ui.Color{}).AlignItems(ui.Center)
					if profile.ID == cfg.ActiveProfile {
						option.Background(c.Theme().Text.Alpha(.08))
					} else if option.Hovered() {
						option.Background(c.Theme().Text.Alpha(.04))
					}
					option.Children(func() {
						ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
							components.Text(c, profileName(profile.ID, profile.Name)).FillWidth().MinWidth(0).SingleLine().Ellipsis("…").FontSize(12)
							components.Text(c, profileDestination(profile.Provider, profile.Bucket)).FillWidth().MinWidth(0).SingleLine().Ellipsis("…").FontSize(11).TextColor(c.Theme().TextMuted)
						})
						if profile.ID == cfg.ActiveProfile {
							components.Icon(c, lucide.Check()).Size(14, 14).TextColor(c.Theme().Accent)
						}
					})
					if option.Clicked() {
						a.profiles.menuOpen = false
						if !a.switchProfile(profile.ID) && a.profiles.err != "" {
							a.feedback("切换配置失败", errors.New(a.profiles.err))
						}
					}
				}
			})
		})
		settingsDivider(c)
		if components.Button(c, tr("管理配置…"), components.ButtonGhost).FillWidth().Clicked() {
			a.profiles.menuOpen = false
			a.openSettings(settingsProfilesTab)
		}
	})
}

func (a *application) profileSettings(c *ui.Context) {
	cfg := a.currentSettings().Clone()
	cfg.EnsureProfiles()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(5).Children(func() {
			components.Text(c, tr("配置管理")).FontSize(16).Bold()
			components.Text(c, tr("为不同存储和使用场景分别保存设置。")).FillWidth().Wrap().FontSize(12).TextColor(c.Theme().TextMuted)
		})
		if components.Button(c, tr("新建配置"), components.ButtonPrimary).Shrink(0).Clicked() {
			a.beginProfileEdit("new")
		}
	})
	for _, profile := range cfg.Profiles {
		current := profile.ID == cfg.ActiveProfile
		ui.Column(c).Key(profile.ID).FillWidth().MinWidth(0).Padding(16).Gap(13).Radius(10).Border(1, c.Theme().Border).Background(c.Theme().Surface).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(9).Children(func() {
				components.Text(c, profileName(profile.ID, profile.Name)).Grow(1).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…").FontSize(13).Bold()
				if current {
					components.Text(c, tr("当前使用")).FontSize(10).TextColor(c.Theme().Accent).Background(c.Theme().Accent.Alpha(.1)).Padding(4, 8).Radius(5)
				}
			})
			components.Text(c, profileDestination(profile.Provider, profile.Bucket)).FillWidth().MinWidth(0).SingleLine().Ellipsis("…").FontSize(11).TextColor(c.Theme().TextMuted)
			ui.Row(c).FillWidth().Wrap().Gap(8).Disabled(a.profileSwitchBlocked()).Children(func() {
				if !current && components.Button(c, tr("切换"), components.ButtonPrimary).Label(tf("切换到 %s", profileName(profile.ID, profile.Name))).Clicked() {
					a.switchProfile(profile.ID)
				}
				if components.Button(c, tr("编辑存储"), components.ButtonOutline).Label(tf("编辑 %s 的存储", profileName(profile.ID, profile.Name))).Clicked() {
					if a.switchProfile(profile.ID) {
						a.openSettings(0)
					}
				}
				for _, action := range []struct{ mode, label string }{{"copy", "复制"}, {"rename", "重命名"}} {
					if components.Button(c, tr(action.label), components.ButtonOutline).Label(tf(action.label+"配置 %s", profileName(profile.ID, profile.Name))).Clicked() {
						a.beginProfileEditFor(action.mode, profile.ID)
					}
				}
				button := components.Button(c, tr("删除"), components.ButtonOutline).Label(tf("删除配置 %s", profileName(profile.ID, profile.Name))).TextColor(c.Theme().Danger).Disabled(len(cfg.Profiles) < 2)
				if button.Clicked() {
					a.profiles = profileUIState{deleteOpen: true, target: profile.ID}
				}
				if len(cfg.Profiles) < 2 {
					components.Tooltip(c, button, tr("至少保留一个配置。"))
				}
			})
		})
	}
	components.Text(c, tr("存储、图片与输出随配置切换。语言、外观和快捷键在所有配置间共用。")).FillWidth().MinWidth(0).Wrap().FontSize(11).TextColor(c.Theme().TextMuted)
}

func (a *application) beginProfileEdit(mode string) {
	a.beginProfileEditFor(mode, a.config.ActiveProfile)
}

func (a *application) beginProfileEditFor(mode, id string) {
	c := a.config.Clone()
	c.EnsureProfiles()
	if id != "" {
		if err := c.SwitchProfile(id); err != nil {
			a.feedback("切换配置失败", err)
			return
		}
	}
	name := ""
	if mode == "rename" {
		name = c.CurrentProfileName()
	} else if mode == "copy" {
		name = tf("%s 副本", profileName(c.ActiveProfile, c.CurrentProfileName()))
	}
	a.profiles = profileUIState{editOpen: true, mode: mode, name: name, target: c.ActiveProfile}
}

func (a *application) profileDialogs(c *ui.Context) {
	ui.Box(c).Key("profile-dialogs").Children(func() {
		components.Dialog(c, &a.profiles.editOpen, components.DialogOptions{Label: tr("编辑配置")}, func(panel ui.Element) {
			title, help := "新建配置", "创建空白配置，填写存储后即可上传。"
			if a.profiles.mode == "copy" {
				title, help = "复制配置", "复制所选配置的存储凭据、图片与输出设置。"
			} else if a.profiles.mode == "rename" {
				title, help = "重命名配置", "修改名称，存储与上传设置保持不变。"
			}
			components.Text(c, tr(title)).Bold().FontSize(18)
			components.Text(c, tr(help)).FillWidth().Wrap().FontSize(12).TextColor(c.Theme().TextMuted)
			components.Text(c, tr("配置名称")).FontSize(12)
			input := components.TextBox(c, &a.profiles.name, components.TextBoxOptions{}).Label(tr("配置名称")).Placeholder(tr("例如：工作资料")).FillWidth().AutoFocus()
			if a.profiles.err != "" {
				components.Text(c, a.profiles.err).FillWidth().Wrap().FontSize(12).TextColor(c.Theme().Danger)
			}
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				if components.Button(c, tr("取消"), components.ButtonOutline).Clicked() {
					a.profiles.editOpen = false
				}
				label := "创建并切换"
				if a.profiles.mode == "rename" {
					label = "保存名称"
				}
				if components.Button(c, tr(label), components.ButtonPrimary).Disabled(a.profileSwitchBlocked()).Clicked() || input.Shortcut(0, ui.KeyEnter) {
					if a.confirmProfileEdit() {
						a.profiles.editOpen = false
					}
				}
			})
		})
		components.Dialog(c, &a.profiles.deleteOpen, components.DialogOptions{Label: tr("删除配置确认")}, func(panel ui.Element) {
			name := ""
			fallback := ""
			for _, profile := range a.config.Profiles {
				if profile.ID == a.profiles.target {
					name = profileName(profile.ID, profile.Name)
				} else if fallback == "" {
					fallback = profileName(profile.ID, profile.Name)
				}
			}
			components.Text(c, tf("删除配置「%s」？", name)).Bold().FontSize(18).Wrap()
			components.Text(c, tr("此配置的存储凭据、图片与输出设置将被移除，云端图片会保留。")).FillWidth().Wrap().FontSize(12).TextColor(c.Theme().TextMuted)
			if a.profiles.target == a.config.ActiveProfile {
				components.Text(c, tf("删除后自动切换到「%s」。", fallback)).FillWidth().Wrap().FontSize(12).TextColor(c.Theme().TextMuted)
			}
			if a.profiles.err != "" {
				components.Text(c, a.profiles.err).FillWidth().Wrap().FontSize(12).TextColor(c.Theme().Danger)
			}
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				if components.Button(c, tr("取消"), components.ButtonOutline).Clicked() {
					a.profiles.deleteOpen = false
				}
				if components.Button(c, tr("删除配置"), components.ButtonPrimary).Background(c.Theme().Danger).TextColor(ui.Hex("#ffffff")).Disabled(a.profileSwitchBlocked() || len(a.config.Profiles) < 2).Clicked() {
					if a.changeProfile(func(cfg *config.Config) error { return cfg.DeleteProfile(a.profiles.target) }) {
						a.profiles.deleteOpen = false
					}
				}
			})
		})
	})
}
