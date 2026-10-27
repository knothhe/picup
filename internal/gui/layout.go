package gui

import (
	"fmt"
	"runtime"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"github.com/oneo-me/onevra/source/shells/plain"
)

const settingsProfilesTab = 6
const navigationDuration = 240 * time.Millisecond

// The sidebar collapses to an icon rail, so navigation never leaves the window.
const sidebarExpandedWidth, sidebarRailWidth = float32(216), float32(56)

var settingsLabels = []string{"存储", "图片与输出", "通用", "CLI", "快捷键", "关于", "配置"}
var settingsOrder = []int{0, 1, 2, 4, 3, settingsProfilesTab, 5}

// Settings are grouped by scope: the current profile, the whole app, and the rest.
var settingsGroups = []struct {
	title string
	tabs  []int
}{{"", []int{0, 1}}, {"应用", []int{2, 4, 3}}, {"其他", []int{settingsProfilesTab, 5}}}

func settingsIcon(tab int) *ui.SVG {
	switch tab {
	case 0:
		return lucide.Cloud()
	case 1:
		return lucide.SlidersHorizontal()
	case 2:
		return lucide.SunMoon()
	case 3:
		return lucide.Terminal()
	case 4:
		return lucide.Keyboard()
	case 5:
		return lucide.Info()
	}
	return lucide.Layers()
}

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

// leaveSettings returns to the page that opened settings.
func (a *application) leaveSettings() {
	previous := a.previousTab
	if previous == 2 {
		previous = 0
	}
	a.navigate(previous)
}

func pageLabel(tab int) string {
	return map[int]string{0: "上传", 2: "设置", 3: "指南", 4: "图库", imageToolsTab: "图片工具"}[tab]
}

func (a *application) pageTitle() string {
	if a.tab == 2 {
		return tr(settingsLabels[a.settingsTab])
	}
	return tr(pageLabel(a.tab))
}

// headerInset mirrors the plain shell's leading padding, past the window controls.
func headerInset(c *ui.Context) float32 {
	bar := c.TitleBar()
	inset := max(8, bar.Left)
	if runtime.GOOS == "darwin" && bar.Left > 0 {
		inset += 16
	}
	return inset
}

// The titlebar aligns its toggle with the sidebar's edge and the page title
// with the content, following the sidebar while it animates.
func (a *application) header(c *ui.Context) {
	width := a.sidebarWidth
	if width <= 0 {
		width = sidebarExpandedWidth
	}
	inset := headerInset(c)
	toggleX := max(inset, width-40)
	titleX := max(toggleX+42, width+24)
	ui.Row(c).FillWidth().FillHeight().MinWidth(0).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Width(toggleX - inset).Shrink(0)
		label, icon := "收起侧边栏", lucide.PanelLeftClose()
		if a.sidebarCollapsed {
			label, icon = "展开侧边栏", lucide.PanelLeftOpen()
		}
		if components.IconButton(c, icon, a.shortcutTooltip(label, "sidebar"), components.IconButtonPlain).Size(30, 30).Label(tr(label)).TextColor(c.Theme().TextMuted).Clicked() {
			a.toggleSidebar()
		}
		ui.Box(c).Width(titleX - toggleX - 30).Shrink(0)
		ui.Row(c).Key(a.tab).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(10).Children(func() {
			components.Text(c, a.pageTitle()).FontSize(15).FontWeight(600).SingleLine().Shrink(0).ContentTransition(navigationContentTransition())
			switch a.tab {
			case 0:
				a.uploadHeaderContext(c)
			case 2:
				a.settingsHeaderContext(c)
			case 4:
				a.galleryHeaderContext(c)
			case imageToolsTab:
				headerChip(c, lucide.Sparkles(), tr("本地处理 · 无需连接存储"))
			}
		})
	})
}

func (a *application) headerActions(c *ui.Context) {
	ui.Row(c).Key(fmt.Sprintf("page-actions-%d", a.tab)).Shrink(0).AlignItems(ui.Center).Gap(4).Children(func() {
		switch a.tab {
		case 0:
			a.uploadHeaderActions(c)
		case 2:
			a.settingsHeaderActions(c)
		case 3:
			if components.Button(c, tr("打开存储设置"), components.ButtonGhost).Clicked() {
				a.openSettings(0)
			}
		case 4:
			a.galleryHeaderActions(c)
		}
	})
}

// headerChip shows a page's context, such as its destination, in the titlebar.
func headerChip(c *ui.Context, icon *ui.SVG, text string) {
	t := c.Theme()
	ui.Row(c).Height(24).MinWidth(0).Shrink(1).Padding(0, 8).Gap(6).Radius(6).Background(sunkenColor(t)).AlignItems(ui.Center).Children(func() {
		if icon != nil {
			components.Icon(c, icon).Size(13, 13).Shrink(0).TextColor(t.TextMuted)
		}
		components.Text(c, text).FontSize(12).TextColor(t.TextMuted).SingleLine().Ellipsis("…").MinWidth(0)
	})
}

// countBadge marks pending work on a navigation item.
func countBadge(c *ui.Context, count int) ui.Element {
	t := c.Theme()
	return ui.Row(c).Height(18).MinWidth(18).Padding(0, 5).Radius(9).Background(t.Accent).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		components.Text(c, fmt.Sprint(count)).FontSize(10.5).FontWeight(600).TextColor(t.AccentText).SingleLine()
	})
}

func (a *application) navigationButton(c *ui.Context, key any, label string, icon *ui.SVG, selected, rail bool, trailing func()) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Key(key).Label(tr(label)).Selected(selected).Radius(7).AlignItems(ui.Center).Background(ui.Color{}).TextColor(t.Text)
	if rail {
		b.Size(36, 36).Justify(ui.Center).Tooltip(tr(label))
	} else {
		b.FillWidth().Height(32).Padding(0, 10).Gap(10).Justify(ui.Start)
	}
	if selected {
		b.Background(t.Surface).Border(1, t.Border)
	} else if b.Hovered() {
		b.Background(t.Text.Alpha(.05))
	}
	iconColor := t.TextMuted
	if selected {
		iconColor = t.Accent
	}
	b.Children(func() {
		components.Icon(c, icon).Size(16, 16).Shrink(0).TextColor(iconColor)
		if !rail {
			text := components.Text(c, tr(label)).FontSize(13).SingleLine().Ellipsis("…").MinWidth(0).Grow(1)
			if selected {
				text.FontWeight(500)
			}
		}
		if trailing != nil {
			trailing()
		}
	})
	return b
}

func (a *application) navigationItem(c *ui.Context, tab int, label string, icon *ui.SVG, rail bool) {
	t := c.Theme()
	var trailing func()
	switch {
	case tab == 0 && a.pendingUploads() > 0:
		trailing = func() {
			badge := countBadge(c, a.pendingUploads())
			if rail {
				badge.Absolute().Top(-3).Right(-4)
			}
		}
	case tab == 4 && a.gallery.loaded && !rail:
		trailing = func() {
			components.Text(c, fmt.Sprint(len(a.gallery.objects))).FontSize(11).TextColor(t.TextMuted).SingleLine()
		}
	case tab == 2 && !rail:
		if binding := a.shortcutBinding("settings"); binding.key != ui.KeyUnknown {
			trailing = func() { components.Text(c, binding.label()).FontSize(11).TextColor(t.TextMuted).SingleLine() }
		}
	}
	b := a.navigationButton(c, tab, label, icon, a.tab == tab, rail, trailing)
	if b.Clicked() {
		if tab == 2 && a.tab == 2 {
			a.leaveSettings()
		} else {
			a.navigate(tab)
		}
	}
}

func (a *application) pendingUploads() int {
	n := 0
	for _, j := range a.jobs {
		if j.result == nil {
			n++
		}
	}
	return n
}

func (a *application) sidebar(c *ui.Context) {
	t := c.Theme()
	panel := ui.Column(c).Key("sidebar").Shrink(0).FillHeight().Clip().Background(sidebarColor(t)).BorderWidth(0, 1, 0, 0).BorderColor(t.Border)
	target := sidebarExpandedWidth
	if a.sidebarCollapsed {
		target = sidebarRailWidth
	}
	width := panel.AnimateWith("width", target, navigationDuration, ui.EaseInOut)
	a.sidebarWidth = width
	rail := width < (sidebarExpandedWidth+sidebarRailWidth)/2
	panel.Width(width).Padding(plain.HeaderHeight(c)+2, 0, 10, 0)
	panel.Children(func() {
		key := "workspace-navigation"
		if a.tab == 2 {
			key = "settings-navigation"
		}
		nav := ui.Column(c).Key(key).Fill().MinHeight(0).Gap(2).Transition(tabTransition())
		if rail {
			nav.Padding(0, 10).AlignItems(ui.Center)
		} else {
			nav.Width(sidebarExpandedWidth).Padding(0, 10)
		}
		nav.Children(func() {
			if a.tab == 2 {
				a.settingsNavigation(c, rail)
			} else {
				a.workspaceNavigation(c, rail)
			}
		})
	})
}

func (a *application) workspaceNavigation(c *ui.Context, rail bool) {
	a.storageIndicator(c, rail)
	ui.Box(c).Height(12).Shrink(0)
	a.navigationItem(c, 0, "上传", lucide.ImageUp(), rail)
	a.navigationItem(c, 4, "图库", lucide.Images(), rail)
	a.navigationItem(c, imageToolsTab, "图片工具", lucide.Sparkles(), rail)
	ui.Spacer(c)
	a.navigationItem(c, 3, "指南", lucide.BookOpen(), rail)
	a.navigationItem(c, 2, "设置", lucide.Settings(), rail)
}

func (a *application) settingsNavigation(c *ui.Context, rail bool) {
	t := c.Theme()
	back := tf("返回%s", tr(pageLabel(a.previousTab)))
	if a.previousTab == 2 {
		back = tf("返回%s", tr("上传"))
	}
	b := ui.ButtonBase(c).Key("settings-back").Label(back).Radius(7).AlignItems(ui.Center).Background(ui.Color{}).TextColor(t.TextMuted).Tooltip(back)
	if rail {
		b.Size(36, 36).Justify(ui.Center)
	} else {
		b.FillWidth().Height(30).Padding(0, 8).Gap(6).Justify(ui.Start)
	}
	if b.Hovered() {
		b.Background(t.Text.Alpha(.05))
	}
	b.Children(func() {
		components.Icon(c, lucide.ChevronLeft()).Size(15, 15).Shrink(0)
		if !rail {
			components.Text(c, back).FontSize(12.5).SingleLine().Ellipsis("…").MinWidth(0)
		}
	})
	if b.Clicked() {
		a.leaveSettings()
	}
	// The profile switcher heads the profile's own settings.
	ui.Box(c).Height(6).Shrink(0)
	a.storageIndicator(c, rail)
	ui.Box(c).Height(4).Shrink(0)
	for i, group := range settingsGroups {
		switch {
		case group.title == "":
		case rail:
			ui.Box(c).Width(24).Height(1).Margin(8, 0).Background(t.Border)
		default:
			components.Text(c, tr(group.title)).Key(fmt.Sprintf("group-%d", i)).FontSize(11).FontWeight(500).TextColor(t.TextMuted).Padding(12, 10, 6, 10)
		}
		for _, tab := range group.tabs {
			if a.navigationButton(c, fmt.Sprintf("settings-%d", tab), settingsLabels[tab], settingsIcon(tab), a.settingsTab == tab, rail, nil).Clicked() {
				a.settingsTab = tab
			}
		}
	}
}

func (a *application) view(c *ui.Context) {
	a.processingEditorFocused = false
	a.initSettingsAutosave()
	if c.Preferences().ReduceMotion {
		c.DisableAnimations()
	}
	t := c.Theme()
	ui.Row(c).Key("workspace").Fill().Background(t.Background).Clip().Children(func() {
		a.sidebar(c)
		ui.Column(c).Key("workspace-content").Grow(1).FillHeight().MinWidth(0).Children(func() {
			// The titlebar draws over this strip; its rule closes the page header.
			ui.Box(c).Key("titlebar-rule").FillWidth().Height(plain.HeaderHeight(c)).Shrink(0).BorderWidth(0, 0, 1, 0).BorderColor(t.Border)
			page := ui.Column(c).Key(a.tab).FillWidth().Grow(1).MinWidth(0).MinHeight(0)
			page.Transition(tabTransition()).Children(func() {
				switch a.tab {
				case 2:
					a.settingsView(c)
				case 3:
					a.guideView(c)
				case 4:
					a.galleryView(c)
				case imageToolsTab:
					a.imageToolsView(c)
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
