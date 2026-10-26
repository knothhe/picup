package gui

import (
	"runtime"

	"github.com/egoist/mygo"
	"github.com/knothhe/picup/assets"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/traydrop"
)

func (a *application) showWindow() {
	if a.quitting {
		return
	}
	a.resumeLightweightUI()
	a.hideQuickUpload()
	if a.win == nil && a.lifecycle.nativeReady {
		a.newMainWindow()
	}
	if a.win != nil {
		a.win.Show()
		a.win.Focus()
		a.win.Invalidate()
		if a.tab == 4 && !a.gallery.loaded && !a.gallery.busy {
			a.enterGallery()
		}
	}
}

func (a *application) showPage(tab int) {
	a.navigate(tab)
	a.showWindow()
}

func (a *application) quickUploadReady() bool {
	c, err := a.effective()
	if err == nil {
		err = c.ValidateStorage()
	}
	if err != nil {
		a.openSettings(0)
		a.feedback("请先完成存储设置", err)
		a.showWindow()
		return false
	}
	return true
}

// Build separate menu items for each native menu, since an item has one parent.
func (a *application) quickUploadItems() []*mygo.MenuItem {
	return []*mygo.MenuItem{
		{ID: "quick-upload", Label: tr("上传图片…"), Accelerator: a.nativeShortcut("quick-upload"), Click: func(*mygo.MenuItem, *mygo.Window) { a.chooseFiles(true) }},
		{ID: "quick-paste", Label: tr("上传剪贴板图片"), Accelerator: a.nativeShortcut("quick-paste"), Click: func(*mygo.MenuItem, *mygo.Window) { a.pasteImage(true) }},
	}
}

func (a *application) trayMenu() *mygo.Menu {
	// Linux tray hosts expose menus; keep the configured destination reachable.
	if a.config.TrayClickAction == config.TrayClickMainWindow {
		return mygo.NewMenu([]*mygo.MenuItem{{ID: "main-window", Label: tr("打开主窗口"), Click: func(*mygo.MenuItem, *mygo.Window) { a.showWindow() }}})
	}
	return mygo.NewMenu([]*mygo.MenuItem{{ID: "quick-panel", Label: tr("快捷上传"), Click: func(*mygo.MenuItem, *mygo.Window) { a.showQuickUpload() }}})
}

func (a *application) applicationMenu() *mygo.Menu {
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Picup", Submenu: []*mygo.MenuItem{
			{Label: tr("关于 Picup"), Click: func(*mygo.MenuItem, *mygo.Window) { a.showPage(5) }},
			{ID: "check-updates", Label: tr("检查更新…"), Click: func(*mygo.MenuItem, *mygo.Window) { a.checkForUpdates(true) }},
			{ID: "settings", Label: tr("设置"), Accelerator: a.nativeShortcut("settings"), Click: func(*mygo.MenuItem, *mygo.Window) { a.showPage(2) }},
			mygo.Separator(), {ID: "window-hide", Role: mygo.RoleHide}, {ID: "window-hide-others", Role: mygo.RoleHideOthers}, {Role: mygo.RoleUnhide},
			mygo.Separator(), {ID: "quit", Label: tr("退出 Picup"), Accelerator: a.nativeShortcut("quit"), Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
		}},
		{Label: tr("文件"), Submenu: append(a.quickUploadItems(), mygo.Separator(), &mygo.MenuItem{ID: "window-close", Role: mygo.RoleClose})},
		a.editMenu(),
		{Role: mygo.RoleWindowMenu, Submenu: []*mygo.MenuItem{{ID: "window-minimize", Role: mygo.RoleMinimize}, {Role: mygo.RoleZoom}, mygo.Separator(), {Role: mygo.RoleFront}}},
	})
	// Roles fill empty accelerators with their defaults while the menu is built.
	// Clear them afterwards too, so disabling a binding really disables it.
	for _, id := range []string{"window-hide", "window-hide-others", "window-close", "window-minimize"} {
		menu.ItemByID(id).SetAccelerator(a.nativeShortcut(id))
	}
	if a.shortcutEditing != "" {
		var clearAccelerators func([]*mygo.MenuItem)
		clearAccelerators = func(items []*mygo.MenuItem) {
			for _, item := range items {
				item.SetAccelerator("")
				clearAccelerators(item.Submenu)
			}
		}
		clearAccelerators(menu.Items())
	}
	return menu
}

func (a *application) editMenu() *mygo.MenuItem {
	// On macOS RolePaste uses the native paste: selector, bypassing Click
	// and key shortcuts. The upload page has no editor to receive that
	// command, so give its image-paste action the native accelerator instead.
	// Other pages retain RolePaste for their text fields.
	standard := mygo.NewMenu([]*mygo.MenuItem{{Role: mygo.RoleEditMenu}}).Items()[0]
	var clone func(*mygo.MenuItem) *mygo.MenuItem
	clone = func(item *mygo.MenuItem) *mygo.MenuItem {
		copy := &mygo.MenuItem{Label: item.Label, Type: item.Type, Role: item.Role, Accelerator: item.Accelerator, Hidden: item.Hidden}
		for _, child := range item.Submenu {
			copy.Submenu = append(copy.Submenu, clone(child))
		}
		if item.Role == mygo.RolePaste {
			copy.ID = "paste"
			if !a.fileDialogOpen && (a.tab == 0 || a.quickUpload.visible) {
				copy.Role = ""
				copy.Accelerator = a.shortcutBinding("paste").value()
				copy.Click = func(*mygo.MenuItem, *mygo.Window) {
					if a.shortcutEditing == "" {
						if a.quickUpload.visible {
							a.pasteImage(true)
						} else if a.tab == 0 {
							a.paste()
						}
					}
				}
			}
		}
		return copy
	}
	return clone(standard)
}

func (a *application) updateMenus() {
	a.nativeMenuState = a.nativeMenuStateKey()
	mygo.App.SetMenu(a.applicationMenu())
	if a.tray != nil {
		if runtime.GOOS == "linux" {
			a.tray.SetMenu(a.trayMenu())
		}
		a.tray.SetToolTip(tr("Picup · 快捷上传图片"))
	}
	a.invalidateWindows()
}

func (a *application) setupMenus() {
	if runtime.GOOS == "darwin" {
		_ = mygo.App.Dock.SetIcon(assets.IconPNG)
	} else if a.win != nil {
		_ = a.win.SetIcon(assets.IconPNG)
	}
	options := mygo.TrayOptions{Icon: menuBarIcon(), IconIsTemplate: runtime.GOOS == "darwin", ToolTip: tr("Picup · 快捷上传图片")}
	if runtime.GOOS == "linux" {
		options.Menu = a.trayMenu()
	}
	tray, err := mygo.NewTray(options)
	if err != nil {
		a.feedback("菜单栏不可用，请使用窗口上传", err)
	} else {
		a.tray = tray
		tray.OnClick(a.trayClicked)
		tray.OnRightClick(a.toggleQuickUpload)
		cleanup, dropErr := traydrop.Register(tray, tr("Picup · 快捷上传图片"), func() bool {
			return !a.quitting && !a.busy
		}, a.quickUploadFiles, a.toggleQuickUpload)
		if dropErr != nil {
			a.feedback("启用菜单栏拖拽上传失败", dropErr)
		} else {
			mygo.App.OnWillQuit(func(*mygo.QuitEvent) { cleanup() })
		}
	}
	a.updateMenus()
}

// Generated from the same photograph and upload arrow as the app icon. The
// template follows the system menu bar's light and dark appearance on macOS.
func menuBarIcon() []byte {
	if runtime.GOOS == "darwin" {
		return assets.MenuBarTemplatePNG
	}
	return assets.MenuBarPNG
}
