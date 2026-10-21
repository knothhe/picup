package gui

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

const settingsProfilesTab = 6
const navigationDuration = 240 * time.Millisecond

var settingsLabels = []string{"存储", "图片与输出", "通用", "CLI", "快捷键", "关于", "配置"}
var settingsOrder = []int{settingsProfilesTab, 0, 1, 2, 3, 4, 5}

// Navigation changes share one timing curve, including the content's new size.
func layoutTransition() ui.ElementTransition {
	return ui.ElementTransition{Duration: navigationDuration, Ease: ui.EaseInOut}
}

// Keep the old page visible beneath the new one until its entrance completes.
// Apply this to the keyed page whose direct parent survives the tab change.
func tabTransition() ui.ElementTransition {
	t := layoutTransition()
	t.Colors = true
	t.Enter = &ui.Motion{Y: 6}
	t.Exit = &ui.Motion{}
	return t
}

func navigationContentTransition() ui.ContentTransition {
	return ui.ContentTransition{Animation: &ui.Animation{Duration: navigationDuration, Ease: ui.EaseInOut}}
}

type navIdentity int

func (a *application) navigate(tab int) {
	if tab == 5 {
		a.settingsTab = 5
		tab = 2
	}
	if tab == 0 {
		a.refreshUploadConfig()
	}
	if tab == 1 {
		tab = 4
	}
	if a.tab == tab {
		return
	}
	if tab == 2 {
		a.previousTab = a.tab
	}
	a.tab = tab
	a.message = ""
	if tab == 4 {
		a.enterGallery()
	}
}

func (a *application) header(c *ui.Context) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		label, icon := "收起侧边栏", lucide.PanelLeftClose()
		if a.sidebarCollapsed {
			label, icon = "展开侧边栏", lucide.PanelLeftOpen()
		}
		if components.IconButton(c, icon, a.shortcutTooltip(label, "sidebar"), components.IconButtonPlain).Label(tr(label)).Clicked() {
			a.profiles.menuOpen = false
			a.sidebarCollapsed = !a.sidebarCollapsed
		}
		ui.Image(c, brandIcon).Size(24, 24).Label("Picup")
		components.Text(c, "picup").Bold().FontSize(14)
		if a.sidebarCollapsed {
			ui.Row(c).Key("header-navigation").Gap(4).Children(func() {
				a.navigationItem(c, 0, "上传", lucide.ImageUp(), false)
				a.navigationItem(c, 4, "图库", lucide.Images(), false)
			})
		} else {
			components.Text(c, "/").TextColor(c.Theme().Border)
			components.Text(c, tr(map[int]string{0: "上传", 2: "设置", 3: "指南", 4: "图库"}[a.tab])).FontSize(12).TextColor(c.Theme().TextMuted).ContentTransition(navigationContentTransition())
		}
	})
}

func headerAction(c *ui.Context, icon *ui.SVG, label string, selected bool) ui.Element {
	button := components.IconButton(c, icon, tr(label), components.IconButtonPlain).Selected(selected).TextColor(c.Theme().TextMuted)
	if selected {
		button.Background(c.Theme().Text.Alpha(.08))
	}
	return button
}

func (a *application) headerActions(c *ui.Context) {
	if !a.sidebarCollapsed {
		return
	}
	ui.Row(c).Key("workspace-actions").Shrink(0).AlignItems(ui.Center).Gap(2).Children(func() {
		a.storageIndicator(c, true)
		if headerAction(c, lucide.BookOpen(), tr("指南"), a.tab == 3).Transition(layoutTransitionFor(3)).Clicked() {
			a.navigate(3)
		}
		if headerAction(c, lucide.Settings(), tr("设置"), a.tab == 2).Transition(layoutTransitionFor(2)).Clicked() {
			if a.tab == 2 {
				a.navigate(a.previousTab)
			} else {
				a.navigate(2)
			}
		}
	})
}

func layoutTransitionFor(tab int) ui.ElementTransition {
	t := layoutTransition()
	t.Match = navIdentity(tab)
	return t
}

func (a *application) navigationItem(c *ui.Context, tab int, label string, icon *ui.SVG, full bool) {
	t := c.Theme()
	b := ui.ButtonBase(c).Key(tab).Label(tr(label)).Selected(a.tab == tab).Height(34).Padding(0, 10).Gap(9).Radius(7).AlignItems(ui.Center).Background(ui.Color{}).TextColor(t.Text)
	if full {
		b.FillWidth().Justify(ui.Start)
	}
	if a.tab == tab {
		b.Background(t.Surface)
	} else if b.Hovered() {
		b.Background(t.Text.Alpha(.04))
	}
	transition := layoutTransition()
	if !full || !a.sidebarCollapsed {
		transition.Match = navIdentity(tab)
	}
	b.Transition(transition).Children(func() {
		components.Icon(c, icon).Size(16, 16).TextColor(t.TextMuted)
		components.Text(c, tr(label)).FontSize(12)
	})
	if b.Clicked() {
		if tab == 2 && a.tab == 2 {
			a.navigate(a.previousTab)
		} else {
			a.navigate(tab)
		}
	}
}

func (a *application) sidebar(c *ui.Context) {
	t := c.Theme()
	panel := ui.Column(c).Key("sidebar").Shrink(0).FillHeight().Clip().BorderColor(t.Border)
	target := float32(188)
	if a.sidebarCollapsed {
		target = 0
	}
	width := panel.AnimateWith("width", target, navigationDuration, ui.EaseInOut)
	panel.Width(width).Disabled(a.sidebarCollapsed)
	if width <= 0 {
		return
	}
	panel.BorderWidth(0, 1, 0, 0).Children(func() {
		scrollView(c).FillWidth().Grow(1).Children(func() {
			ui.Column(c).FillWidth().MinWidth(0).Padding(18, 0, 18, 10).Gap(5).Children(func() {
				components.Text(c, tr("工作区")).FontSize(10).TextColor(t.TextMuted).Padding(0, 10, 8, 10)
				a.navigationItem(c, 0, "上传", lucide.ImageUp(), true)
				a.navigationItem(c, 4, "图库", lucide.Images(), true)
				if a.tab == 2 {
					ui.Column(c).Key("settings-navigation").FillWidth().Padding(22, 0, 0, 0).Gap(4).Transition(ui.ElementTransition{
						Duration: navigationDuration, Ease: ui.EaseInOut,
						Enter: &ui.Motion{Y: -6, Collapse: true}, Exit: &ui.Motion{Collapse: true},
					}).Children(func() {
						components.Text(c, tr("设置")).FontSize(10).TextColor(t.TextMuted).Padding(0, 10, 8, 10)
						icons := []*ui.SVG{lucide.Cloud(), lucide.SlidersHorizontal(), lucide.SunMoon(), lucide.Terminal(), lucide.Keyboard(), lucide.Info(), lucide.Layers()}
						for _, i := range settingsOrder {
							label := tr(settingsLabels[i])
							if components.SidebarItemWithLeading(c, label, a.settingsTab == i, func(c *ui.Context, size float32) {
								components.Icon(c, icons[i]).Size(size, size).TextColor(t.TextMuted)
							}).Padding(t.Space(1.75), 0, t.Space(1.75), t.Space(2)).Transition(layoutTransition()).Clicked() {
								a.settingsTab = i
							}
						}
					})
				}
			})
		})
		ui.Column(c).Width(187).Padding(10).Gap(4).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
			a.storageIndicator(c, false)
			a.navigationItem(c, 3, "指南", lucide.BookOpen(), true)
			a.navigationItem(c, 2, "设置", lucide.Settings(), true)
		})
	})
}

func (a *application) view(c *ui.Context) {
	a.initSettingsAutosave()
	if c.Preferences().ReduceMotion {
		c.DisableAnimations()
	}
	ui.Row(c).Key("workspace").Fill().Background(c.Theme().Background).Clip().Children(func() {
		a.sidebar(c)
		ui.Column(c).Key("workspace-content").Grow(1).FillHeight().MinWidth(0).Padding(24, 24, 20, 24).Gap(16).Children(func() {
			page := ui.Column(c).Key(a.tab).FillWidth().Grow(1).MinWidth(0).MinHeight(0).Gap(16)
			page.Transition(tabTransition()).Children(func() {
				switch a.tab {
				case 2:
					a.settingsView(c)
				case 3:
					a.guideView(c)
				case 4:
					a.galleryView(c)
				default:
					a.uploadView(c)
				}
			})
		})
	})
	a.galleryDialogs(c)
	a.profileDialogs(c)
	a.registerShortcuts(c)
	a.watchSettingsAutosave(c)
	a.notificationView(c)
}
