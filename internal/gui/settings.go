package gui

import (
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

type settingsScrollIdentity int

func settingsGroup(c *ui.Context, key, title string, build func()) {
	ui.Column(c).Key(key).FillWidth().MinWidth(0).Gap(8).Children(func() {
		components.Text(c, tr(title)).FontSize(13).FontWeight(600)
		ui.Column(c).FillWidth().MinWidth(0).Radius(10).Border(1, c.Theme().Border).Background(c.Theme().Surface).Clip().Children(build)
	})
}

func (a *application) preference(c *ui.Context, key, title, description string, build func(), activate ...func()) {
	width := a.settingsContentWidth(c)
	controlWidth := min(float32(260), max(float32(200), (width-80)*.44))
	ui.Row(c).Key(key).FillWidth().MinWidth(0).Padding(13, 16).Gap(16).AlignItems(ui.Center).Children(func() {
		text := ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4)
		if len(activate) > 0 {
			text.OnClick(activate[0])
		}
		text.Children(func() {
			components.Text(c, tr(title)).FontSize(13).FontWeight(500).Wrap()
			if description != "" {
				components.Text(c, tr(description)).FontSize(12).TextColor(c.Theme().TextMuted).Wrap()
			}
		})
		ui.Column(c).Width(controlWidth).Shrink(0).AlignItems(ui.End).Children(build)
	})
}

func settingsDivider(c *ui.Context) {
	ui.Box(c).FillWidth().Height(1).Background(c.Theme().Border.Alpha(.65))
}

func (a *application) textPreference(c *ui.Context, key, label, description, placeholder string, value *string, secret bool) {
	a.preference(c, key, label, description, func() {
		input := components.TextBox(c, value, components.TextBoxOptions{}).FillWidth().Label(tr(label)).Placeholder(tr(placeholder))
		if secret {
			input.Password()
		}
	})
}

func selectControl(c *ui.Context, label string, value *string, choices, labels []string) {
	s := components.Select(c, value)
	caption := *value
	for i, choice := range choices {
		if choice == *value {
			caption = tr(labels[i])
		}
	}
	s.Trigger.FillWidth().Label(tr(label)).Children(func() { components.Text(c, caption).FontSize(12) })
	s.Popup(func(ui.Element) {
		for i, choice := range choices {
			s.Item(choice).Label(tr(labels[i])).Children(func() { components.Text(c, tr(labels[i])).FontSize(12) })
		}
	})
}

func (a *application) switchPreference(c *ui.Context, key, title, description string, value *bool) {
	a.preference(c, key, title, description, func() {
		components.Switch(c, value, tr(title), components.SwitchOptions{HideLabel: true})
	}, func() { *value = !*value })
}

func (a *application) segmentedPreference(c *ui.Context, key, title, description string, value *string, choices, labels []string) {
	width := a.settingsContentWidth(c)
	row := ui.Row(c).Key(key).FillWidth().MinWidth(0).Padding(13, 16).Gap(16).AlignItems(ui.Center)
	if width < 640 {
		row.Column().Gap(9).AlignItems(ui.Stretch)
	}
	row.Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
			components.Text(c, tr(title)).FontSize(13).FontWeight(500).Wrap()
			if description != "" {
				components.Text(c, tr(description)).FontSize(12).TextColor(c.Theme().TextMuted).Wrap()
			}
		})
		selected := 0
		for i, choice := range choices {
			if choice == *value {
				selected = i
				break
			}
		}
		if ui.Segmented(c, &selected, trList(labels)...).Label(tr(title)).FontSize(12).AlignSelf(ui.Start).Changed() {
			*value = choices[selected]
		}
	})
}

// settingsContentWidth is the width of the settings column inside its padding.
func (a *application) settingsContentWidth(c *ui.Context) float32 {
	width, _ := c.Size()
	return min(width-a.sidebarWidth-64, settingsMaxWidth)
}

const settingsMaxWidth = 720

func (a *application) settingsHeaderContext(c *ui.Context) {
	switch a.settingsTab {
	case 0, 1:
		cfg := a.currentSettings().Clone()
		cfg.EnsureProfiles()
		headerChip(c, nil, tf("仅作用于「%s」", profileName(cfg.ActiveProfile, cfg.CurrentProfileName())))
	case 2, 3, 4:
		headerChip(c, nil, tr("所有配置共用"))
	}
}

// The save state lives in the titlebar, so every settings page reports it.
func (a *application) settingsHeaderActions(c *ui.Context) {
	t := c.Theme()
	if a.gallery.busy && a.gallery.cancel != nil {
		if components.Button(c, tr("取消图库请求"), components.ButtonGhost).Shrink(0).Clicked() {
			a.gallery.cancel()
		}
	}
	defer func() {
		if components.IconButton(c, lucide.BookOpen(), tr("使用入门"), components.IconButtonPlain).TextColor(t.TextMuted).Clicked() {
			a.openGuide(0)
		}
	}()
	switch {
	case a.settingsSave.failed:
		ui.Row(c).Role(ui.RoleStatus).Height(30).Padding(0, 4, 0, 10).Gap(8).Radius(7).Background(dangerSoft(t)).AlignItems(ui.Center).Children(func() {
			components.Text(c, tr("保存失败，输入已保留")).FontSize(12).TextColor(t.Danger).SingleLine()
			if components.Button(c, tr("重试保存"), components.ButtonGhost).Height(24).TextColor(t.Danger).Clicked() {
				a.save()
			}
		})
	case a.settingsSave.pending:
		ui.Row(c).Role(ui.RoleStatus).Gap(6).AlignItems(ui.Center).Children(func() {
			components.Spinner(c, components.SpinnerRing).Size(12, 12)
			components.Text(c, tr("正在保存…")).FontSize(12).TextColor(t.TextMuted)
		})
	default:
		ui.Row(c).Role(ui.RoleStatus).Gap(6).AlignItems(ui.Center).Tooltip(tr("更改会自动保存，桌面端与 CLI 共用设置。")).Children(func() {
			components.Icon(c, lucide.Check()).Size(14, 14).TextColor(t.Success)
			components.Text(c, tr("已自动保存")).FontSize(12).TextColor(t.TextMuted)
		})
	}
}

func (a *application) settingsView(c *ui.Context) {
	scrollView(c).Key(settingsScrollIdentity(a.settingsTab)).FillWidth().Grow(1).MinHeight(0).Transition(tabTransition()).Children(func() {
		busy := a.busy && !(a.settingsTab == 5 && a.updates.phase == "installing") && !(a.settingsTab == 3 && a.cliManagement.operationActive())
		ui.Column(c).Key(a.settingsTab).FillWidth().MaxWidth(settingsMaxWidth+64).MinWidth(0).Padding(24, 32).Gap(22).Disabled(busy || a.gallery.busy || a.r2.saving && a.settingsTab != 0 || a.r2.working && !a.r2.active).Children(func() {
			switch a.settingsTab {
			case 0:
				a.storageSettings(c)
			case 1:
				a.imageSettings(c)
			case 2:
				a.appearanceSettings(c)
			case 3:
				a.cliSettings(c)
			case 5:
				a.aboutContent(c)
			case 4:
				a.shortcutsSettings(c)
			case settingsProfilesTab:
				a.profileSettings(c)
			}
		})
	})
}

func (a *application) imageSettings(c *ui.Context) {
	settingsGroup(c, "upload-task-preferences", "上传任务", func() {
		a.preference(c, "concurrency", "同时上传数量", "限制同时处理与上传的图片，范围 1 至 6。", func() {
			value := float64(a.config.UploadConcurrency)
			if components.NumberInput(c, &value, 1, 6, 1).Label(tr("同时上传数量")).Width(140).Changed() {
				a.config.UploadConcurrency = int(value)
			}
		})
		settingsDivider(c)
		a.preference(c, "collision-policy", "同路径图片", "复用会返回已有链接；覆盖会替换内容。复用、改名和跳过需要读取权限。", func() {
			selectControl(c, "同路径图片", &a.config.CollisionPolicy, []string{"reuse", "overwrite", "rename", "skip"}, []string{"复用已有链接", "覆盖", "自动改名", "跳过并提示"})
		})
	})
	settingsGroup(c, "image-processing", "图片处理", func() {
		a.switchPreference(c, "compress", "默认压缩图片", "PNG 无损优化，JPEG / WebP / AVIF 按质量压缩。", &a.config.Compress)
		settingsDivider(c)
		a.preference(c, "format", "图片格式", "动画保持原样；HEIC 默认转为 WebP。", func() {
			selectControl(c, "图片格式", &a.config.Format, []string{"original", "webp", "avif", "jpeg", "png"}, []string{"保留原格式", "WebP", "AVIF", "JPEG", "PNG"})
		})
		settingsDivider(c)
		a.preference(c, "quality", "压缩质量", "适用于 JPEG / WebP / AVIF，范围 1 至 100。", func() {
			components.NumberInput(c, &a.quality, 1, 100, 1).Label(tr("压缩质量")).Width(140)
		})
		settingsDivider(c)
		a.preference(c, "max-width", "最大宽度", "单位为像素，0 为不限；只缩小，不放大。", func() {
			components.NumberInput(c, &a.maxWidth, 0, 32768, 1).Label(tr("最大宽度")).Width(140)
		})
	})
	settingsGroup(c, "image-output", "链接与剪贴板", func() {
		a.preference(c, "output", "链接格式", "", func() {
			selectControl(c, "链接格式", &a.config.Output, []string{"url", "markdown", "json"}, []string{"URL", "Markdown", "JSON"})
		})
		settingsDivider(c)
		a.switchPreference(c, "copy", "上传后自动复制结果", "CLI 在交互终端中使用此偏好。", &a.config.Copy)
	})
}

func (a *application) appearanceSettings(c *ui.Context) {
	settingsGroup(c, "appearance", "界面", func() {
		a.preference(c, "language", "语言", "", func() {
			before := a.config.Language
			selectControl(c, "语言", &a.config.Language, []string{"system", "zh-CN", "en"}, []string{"跟随系统", "简体中文", "English"})
			if before != a.config.Language {
				a.applyLanguage(true)
			}
		})
		settingsDivider(c)
		a.preference(c, "theme", "外观", "", func() {
			values := []string{"system", "light", "dark"}
			selected := 0
			for i, value := range values {
				if a.config.Appearance == value {
					selected = i
				}
			}
			before := selected
			ui.Segmented(c, &selected, trList([]string{"自动", "浅色", "深色"})...).Label(tr("外观")).FontSize(12)
			if before != selected {
				a.config.Appearance = values[selected]
				if a.win != nil {
					mygo.Theme.SetSource(mygo.ThemeSource(a.config.Appearance))
				}
			}
		})
	})
	a.windowSettings(c)
	a.nativeActionsSettings(c)
	settingsGroup(c, "gallery-cache", "图库缓存", func() {
		a.preference(c, "cache-clear", "本地缓存", "最多 256 MiB，不影响云端图片。", func() {
			if components.Button(c, tr("清理图库缓存"), components.ButtonOutline).Clicked() {
				a.clearGalleryCache()
			}
		})
	})
}

func (a *application) windowSettings(c *ui.Context) {
	settingsGroup(c, "window-startup", "窗口与启动", func() {
		a.switchPreference(c, "close-to-tray", "关闭窗口后继续运行", "关闭后保留菜单栏图标和后台上传。", &a.config.CloseToTray)
		settingsDivider(c)
		ui.Column(c).FillWidth().Disabled(runtime.GOOS != "darwin").Children(func() {
			a.segmentedPreference(c, "dock-icon", "Dock 图标", "跟随窗口时，仅在主窗口打开时展示。", &a.config.DockIcon, []string{config.DockIconShow, config.DockIconHide, config.DockIconFollowWindow}, []string{"展示", "隐藏", "跟随窗口"})
		})
		settingsDivider(c)
		a.segmentedPreference(c, "tray-click", "点击托盘图标", "", &a.config.TrayClickAction, []string{config.TrayClickQuickPanel, config.TrayClickMainWindow}, []string{"打开快捷面板", "打开主窗口"})
		settingsDivider(c)
		a.switchPreference(c, "lightweight", "轻量模式", "窗口和面板全部关闭一分钟后释放界面内存，再次打开时重新加载。", &a.config.LightweightMode)
		settingsDivider(c)
		ui.Column(c).FillWidth().Disabled(a.lifecycle.setOpenAtLogin == nil).Children(func() {
			a.switchPreference(c, "launch-at-login", "开机自启", "", &a.config.LaunchAtLogin)
		})
	})
}

func (a *application) cliSettings(c *ui.Context) { a.cliManagementSettings(c) }
