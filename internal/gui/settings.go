package gui

import (
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/install"
	"github.com/oneo-me/onevra/source/components"
)

type settingsScrollIdentity int

func settingsGroup(c *ui.Context, key, title string, build func()) {
	ui.Column(c).Key(key).FillWidth().MinWidth(0).Gap(9).Children(func() {
		components.Text(c, tr(title)).FontSize(11).Bold().TextColor(c.Theme().TextMuted).Padding(0, 2)
		ui.Column(c).FillWidth().MinWidth(0).Radius(10).Border(1, c.Theme().Border).Background(c.Theme().Surface).Clip().Children(build)
	})
}

func (a *application) preference(c *ui.Context, key, title, description string, build func(), activate ...func()) {
	width, _ := c.Size()
	if !a.sidebarCollapsed {
		width -= 188
	}
	controlWidth := min(float32(260), max(float32(200), (width-80)*.44))
	ui.Row(c).Key(key).FillWidth().MinWidth(0).Padding(13, 16).Gap(16).AlignItems(ui.Center).Children(func() {
		text := ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4)
		if len(activate) > 0 {
			text.OnClick(activate[0])
		}
		text.Children(func() {
			components.Text(c, tr(title)).FontSize(12).Wrap()
			if description != "" {
				components.Text(c, tr(description)).FontSize(11).TextColor(c.Theme().TextMuted).Wrap()
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
	width, _ := c.Size()
	if !a.sidebarCollapsed {
		width -= 188
	}
	row := ui.Row(c).Key(key).FillWidth().MinWidth(0).Padding(13, 16).Gap(16).AlignItems(ui.Center)
	if width < 850 {
		row.Column().Gap(9).AlignItems(ui.Stretch)
	}
	row.Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
			components.Text(c, tr(title)).FontSize(12).Wrap()
			components.Text(c, tr(description)).FontSize(11).TextColor(c.Theme().TextMuted).Wrap()
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

func (a *application) settingsView(c *ui.Context) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			if a.settingsTab == 5 {
				heading(c, tr("关于"), tr("Picup 的版本与本机信息。"))
			} else {
				heading(c, tr("设置"), tr("配置存储、图片处理和桌面偏好。"))
			}
		})
		if components.Button(c, tr("使用入门"), components.ButtonOutline).Shrink(0).Clicked() {
			a.openGuide(0)
		}
		if a.gallery.busy && a.gallery.cancel != nil {
			if components.Button(c, tr("取消图库请求"), components.ButtonOutline).Shrink(0).Clicked() {
				a.gallery.cancel()
			}
		}
	})
	if a.sidebarCollapsed {
		ui.Row(c).FillWidth().Wrap().Gap(4).Children(func() {
			for _, category := range settingsOrder {
				button := components.Button(c, tr(settingsLabels[category]), components.ButtonGhost).Selected(a.settingsTab == category).TextColor(c.Theme().Text)
				if a.settingsTab == category {
					button.Background(c.Theme().Text.Alpha(.08))
				}
				if button.Transition(layoutTransition()).Clicked() {
					a.settingsTab = category
				}
			}
		})
	}
	scrollView(c).Key(settingsScrollIdentity(a.settingsTab)).FillWidth().Grow(1).MinHeight(0).Transition(tabTransition()).Children(func() {
		ui.Column(c).Key(a.settingsTab).FillWidth().MinWidth(0).Gap(20).Disabled(a.busy || a.gallery.busy).Children(func() {
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
	ui.Row(c).FillWidth().Shrink(0).Padding(12, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(c.Theme().Border).AlignItems(ui.Center).Gap(8).Children(func() {
		components.Text(c, tr("更改会自动保存，桌面端与 CLI 共用设置。")).FontSize(11).TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0).Wrap()
		if a.settingsSave.failed && components.Button(c, tr("重试保存"), components.ButtonOutline).Clicked() {
			a.save()
		}
	})
}

func (a *application) imageSettings(c *ui.Context) {
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
		a.preference(c, "output", "链接格式", "上传完成后复制的内容格式。", func() {
			selectControl(c, "链接格式", &a.config.Output, []string{"url", "markdown", "json"}, []string{"URL", "Markdown", "JSON"})
		})
		settingsDivider(c)
		a.switchPreference(c, "copy", "上传后自动复制结果", "CLI 在交互终端中使用此偏好。", &a.config.Copy)
	})
}

func (a *application) appearanceSettings(c *ui.Context) {
	settingsGroup(c, "appearance", "界面", func() {
		a.preference(c, "language", "语言", "无匹配的系统语言时使用英文。", func() {
			before := a.config.Language
			selectControl(c, "语言", &a.config.Language, []string{"system", "zh-CN", "en"}, []string{"跟随系统", "简体中文", "English"})
			if before != a.config.Language {
				a.applyLanguage(true)
			}
		})
		settingsDivider(c)
		a.preference(c, "theme", "外观", "即时生效并自动保存。", func() {
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
	settingsGroup(c, "gallery-cache", "图库缓存", func() {
		a.preference(c, "cache-clear", "本地缓存", "保存列表与缩略图，最多 256 MiB，自动清理较早使用的内容。", func() {
			if components.Button(c, tr("清理图库缓存"), components.ButtonOutline).Clicked() {
				a.clearGalleryCache()
			}
		})
	})
	components.Text(c, tr("清理缓存后，下次打开图库会重新加载。")).FillWidth().MinWidth(0).Wrap().FontSize(11).TextColor(c.Theme().TextMuted)
}

func (a *application) windowSettings(c *ui.Context) {
	settingsGroup(c, "window-startup", "窗口与启动", func() {
		a.switchPreference(c, "close-to-tray", "关闭窗口后继续运行", "关闭主窗口后保留托盘和后台上传；关闭此选项后退出应用。", &a.config.CloseToTray)
		settingsDivider(c)
		ui.Column(c).FillWidth().Disabled(runtime.GOOS != "darwin").Children(func() {
			a.segmentedPreference(c, "dock-icon", "Dock 图标", "跟随窗口时，仅在主窗口打开时展示。", &a.config.DockIcon, []string{config.DockIconShow, config.DockIconHide, config.DockIconFollowWindow}, []string{"展示", "隐藏", "跟随窗口"})
		})
		settingsDivider(c)
		a.segmentedPreference(c, "tray-click", "点击托盘图标", "选择点击菜单栏或托盘图标时打开的位置。", &a.config.TrayClickAction, []string{config.TrayClickQuickPanel, config.TrayClickMainWindow}, []string{"打开快捷面板", "打开主窗口"})
		settingsDivider(c)
		a.switchPreference(c, "lightweight", "轻量模式", "窗口和面板全部关闭一分钟后释放界面内存，再次打开时重新加载。", &a.config.LightweightMode)
		settingsDivider(c)
		ui.Column(c).FillWidth().Disabled(a.lifecycle.setOpenAtLogin == nil).Children(func() {
			a.switchPreference(c, "launch-at-login", "开机自启", "登录系统后在后台启动 Picup。", &a.config.LaunchAtLogin)
		})
	})
}

func (a *application) cliSettings(c *ui.Context) {
	settingsGroup(c, "cli-install", "命令行工具", func() {
		a.textPreference(c, "install-dir", "安装目录", "默认安装到用户目录。", install.DisplayPath(install.DefaultDir()), &a.installDir, false)
		settingsDivider(c)
		a.preference(c, "install", "安装或更新", "与桌面端共用配置。", func() {
			if components.Button(c, tr("安装 / 更新 CLI"), components.ButtonPrimary).Clicked() {
				a.installCLI()
			}
		})
	})
	if a.installMessage != "" {
		components.Text(c, a.installMessage).Wrap().FontSize(12)
	}
	if !install.OnPath(a.installDir) {
		components.Text(c, tr("安装后请将此目录加入 PATH。")).Wrap().FontSize(11).TextColor(c.Theme().TextMuted)
	}
	settingsGroup(c, "cli-example", "快速开始", func() {
		ui.Column(c).FillWidth().Padding(16).Gap(8).Children(func() {
			for _, command := range []string{"picup photo.png", "picup --output markdown", "picup config"} {
				components.Text(c, command).FontSize(12).TextColor(c.Theme().Accent)
			}
		})
	})
}
