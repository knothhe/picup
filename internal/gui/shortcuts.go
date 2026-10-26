package gui

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

type shortcutCommand struct {
	id, title, scope, defaults string
	macOnly                    bool
}

// Only application commands are remapped. Native text editing remains owned by
// the focused editor, including Select All and the arrow keys.
var shortcutCommands = []shortcutCommand{
	{id: "settings", title: "打开设置", scope: "通用", defaults: "CmdOrCtrl+,"},
	{id: "sidebar", title: "切换侧边栏", scope: "通用", defaults: "CmdOrCtrl+B"},
	{id: "choose", title: "选择图片", scope: "通用", defaults: "CmdOrCtrl+O"},
	{id: "quit", title: "退出 Picup", scope: "通用", defaults: "CmdOrCtrl+Q"},
	{id: "paste", title: "粘贴图片", scope: "上传", defaults: "CmdOrCtrl+V"},
	{id: "upload", title: "上传队列", scope: "上传", defaults: "CmdOrCtrl+Enter"},
	{id: "quick-upload", title: "选择并上传", scope: "快捷上传", defaults: "CmdOrCtrl+Shift+U"},
	{id: "quick-paste", title: "上传剪贴板图片", scope: "快捷上传", defaults: "CmdOrCtrl+Shift+V"},
	{id: "gallery-select-all", title: "全选当前页", scope: "图库", defaults: "CmdOrCtrl+A"},
	{id: "preview-close", title: "返回图库", scope: "图片预览", defaults: "Escape"},
	{id: "preview-previous", title: "上一张图片", scope: "图片预览", defaults: "Left"},
	{id: "preview-next", title: "下一张图片", scope: "图片预览", defaults: "Right"},
	{id: "window-close", title: "关闭窗口", scope: "窗口", defaults: "CmdOrCtrl+W"},
	{id: "window-minimize", title: "最小化窗口", scope: "窗口", defaults: "CmdOrCtrl+M"},
	{id: "window-hide", title: "隐藏 Picup", scope: "窗口", defaults: "CmdOrCtrl+H", macOnly: true},
	{id: "window-hide-others", title: "隐藏其他应用", scope: "窗口", defaults: "CmdOrCtrl+Alt+H", macOnly: true},
}

type shortcutBinding struct {
	mods ui.Modifiers
	key  ui.Key
}

var shortcutKeys = []struct {
	name string
	key  ui.Key
}{
	{"Enter", ui.KeyEnter}, {"Escape", ui.KeyEscape}, {"Backspace", ui.KeyBackspace},
	{"Tab", ui.KeyTab}, {"Space", ui.KeySpace}, {"Delete", ui.KeyDelete}, {"Insert", ui.KeyInsert},
	{"Home", ui.KeyHome}, {"End", ui.KeyEnd}, {"PageUp", ui.KeyPageUp}, {"PageDown", ui.KeyPageDown},
	{"Left", ui.KeyLeft}, {"Right", ui.KeyRight}, {"Up", ui.KeyUp}, {"Down", ui.KeyDown},
	{"-", ui.KeyMinus}, {"=", ui.KeyEqual}, {",", ui.KeyComma}, {".", ui.KeyPeriod},
	{"/", ui.KeySlash}, {";", ui.KeySemicolon}, {"'", ui.KeyQuote}, {"[", ui.KeyBracketLeft},
	{"]", ui.KeyBracketRight}, {"\\", ui.KeyBackslash}, {"`", ui.KeyBackquote},
}

func shortcutKeyName(key ui.Key) string {
	if key >= ui.KeyA && key <= ui.KeyZ {
		return string(rune('A' + key - ui.KeyA))
	}
	if key >= ui.Key0 && key <= ui.Key9 {
		return string(rune('0' + key - ui.Key0))
	}
	if key >= ui.KeyF1 && key <= ui.KeyF12 {
		return fmt.Sprintf("F%d", key-ui.KeyF1+1)
	}
	for _, item := range shortcutKeys {
		if item.key == key {
			return item.name
		}
	}
	return ""
}

func parseShortcut(value string) (shortcutBinding, bool) {
	var b shortcutBinding
	if value == "" {
		return b, true
	}
	parts := strings.Split(value, "+")
	for _, modifier := range parts[:len(parts)-1] {
		switch strings.ToLower(modifier) {
		case "cmdorctrl":
			b.mods |= ui.Cmd
		case "cmd", "super":
			b.mods |= ui.Super
		case "ctrl", "control":
			b.mods |= ui.Ctrl
		case "alt", "option":
			b.mods |= ui.Alt
		case "shift":
			b.mods |= ui.Shift
		default:
			return shortcutBinding{}, false
		}
	}
	name := parts[len(parts)-1]
	for key := ui.KeyEnter; key <= ui.KeyForward; key++ {
		if candidate := shortcutKeyName(key); candidate != "" && strings.EqualFold(candidate, name) {
			b.key = key
			return b, true
		}
	}
	return shortcutBinding{}, false
}

func (b shortcutBinding) value() string {
	if b.key == ui.KeyUnknown {
		return ""
	}
	var parts []string
	mods := b.mods
	if mods&ui.Cmd != 0 {
		parts = append(parts, "CmdOrCtrl")
		mods &^= ui.Cmd
	}
	for _, modifier := range []struct {
		mods ui.Modifiers
		name string
	}{{ui.Super, "Super"}, {ui.Ctrl, "Ctrl"}, {ui.Alt, "Alt"}, {ui.Shift, "Shift"}} {
		if mods&modifier.mods != 0 {
			parts = append(parts, modifier.name)
		}
	}
	return strings.Join(append(parts, shortcutKeyName(b.key)), "+")
}

func (b shortcutBinding) label() string {
	if b.key == ui.KeyUnknown {
		return tr("未设置")
	}
	name := shortcutKeyName(b.key)
	if symbol := map[ui.Key]string{ui.KeyLeft: "←", ui.KeyRight: "→", ui.KeyUp: "↑", ui.KeyDown: "↓", ui.KeyEscape: "Esc", ui.KeyEnter: "Enter", ui.KeySpace: "Space"}[b.key]; symbol != "" {
		name = symbol
	}
	if runtime.GOOS == "darwin" {
		var prefix string
		for _, modifier := range []struct {
			mods   ui.Modifiers
			symbol string
		}{{ui.Ctrl, "⌃"}, {ui.Alt, "⌥"}, {ui.Shift, "⇧"}, {ui.Super, "⌘"}} {
			if b.mods&modifier.mods != 0 {
				prefix += modifier.symbol
			}
		}
		return prefix + name
	}
	return strings.NewReplacer("CmdOrCtrl", "Ctrl", "Super", "Win").Replace(b.value())
}

func shortcutDefinition(id string) (shortcutCommand, bool) {
	for _, command := range shortcutCommands {
		if command.id == id {
			return command, true
		}
	}
	return shortcutCommand{}, false
}

func (a *application) shortcutBinding(id string) shortcutBinding {
	command, ok := shortcutDefinition(id)
	if !ok {
		return shortcutBinding{}
	}
	if value, exists := a.config.Shortcuts[id]; exists {
		if b, valid := parseShortcut(value); valid {
			return b
		}
	}
	b, _ := parseShortcut(command.defaults)
	return b
}

func globalShortcut(command shortcutCommand) bool {
	return command.scope == "通用" || command.scope == "快捷上传" || command.scope == "窗口"
}

func shortcutScopesOverlap(a, b shortcutCommand) bool {
	return globalShortcut(a) || globalShortcut(b) || a.scope == b.scope
}

func editorShortcut(b shortcutBinding) bool {
	mods := b.mods &^ ui.Shift
	if runtime.GOOS == "darwin" {
		if mods == ui.Alt && (b.key >= ui.KeyA && b.key <= ui.KeyZ || b.key >= ui.Key0 && b.key <= ui.Key9 || b.key == ui.KeySpace || b.key >= ui.KeyMinus && b.key <= ui.KeyBackquote) {
			return true
		}
		if mods == ui.Ctrl {
			switch b.key {
			case ui.KeyA, ui.KeyE, ui.KeyK, ui.KeyB, ui.KeyF, ui.KeyP, ui.KeyN, ui.KeyH, ui.KeyD:
				return true
			}
		}
	}
	switch b.key {
	case ui.KeyA, ui.KeyC, ui.KeyX, ui.KeyY:
		return mods == ui.Cmd
	case ui.KeyV:
		return mods == ui.Cmd || b.mods == ui.Cmd|ui.Alt|ui.Shift
	case ui.KeyZ:
		return mods == ui.Cmd
	case ui.KeyLeft, ui.KeyRight, ui.KeyUp, ui.KeyDown, ui.KeyHome, ui.KeyEnd,
		ui.KeyPageUp, ui.KeyPageDown, ui.KeyBackspace, ui.KeyDelete:
		return true
	}
	return false
}

func (a *application) setShortcut(id string, b shortcutBinding) bool {
	command, ok := shortcutDefinition(id)
	if !ok {
		return false
	}
	if b.key != ui.KeyUnknown {
		if shortcutKeyName(b.key) == "" {
			a.shortcutError = tr("不支持此按键，请使用其他快捷键。")
			return false
		}
		if command.scope != "图片预览" && b.mods&(ui.Cmd|ui.Ctrl|ui.Alt|ui.Super) == 0 && (b.key < ui.KeyF1 || b.key > ui.KeyF12) {
			a.shortcutError = tr("请组合 Command、Control 或 Option 等修饰键。")
			return false
		}
		defaults, _ := parseShortcut(command.defaults)
		if globalShortcut(command) && editorShortcut(b) && !(command.id == "quick-paste" && b == defaults) {
			a.shortcutError = tr("此快捷键用于文本编辑，请使用其他组合。")
			return false
		}
		for _, other := range shortcutCommands {
			if other.id != id && (!other.macOnly || runtime.GOOS == "darwin") && shortcutScopesOverlap(command, other) && a.shortcutBinding(other.id) == b {
				a.shortcutError = tf("此快捷键已用于「%s」。", tr(other.title))
				return false
			}
		}
	}
	if a.config.Shortcuts == nil {
		a.config.Shortcuts = map[string]string{}
	}
	defaultBinding, _ := parseShortcut(command.defaults)
	if b == defaultBinding {
		delete(a.config.Shortcuts, id)
	} else {
		a.config.Shortcuts[id] = b.value()
	}
	a.shortcutEditing, a.shortcutError = "", ""
	a.refreshShortcutMenus()
	return true
}

func (a *application) refreshShortcutMenus() {
	if a.win != nil || a.tray != nil {
		a.updateMenus()
	}
}

func (a *application) cancelShortcutRecording() {
	if a.shortcutEditing != "" {
		a.shortcutEditing, a.shortcutError = "", ""
		a.refreshShortcutMenus()
	}
}

func (a *application) shortcut(c *ui.Context, id string) bool {
	b := a.shortcutBinding(id)
	return a.shortcutEditing == "" && b.key != ui.KeyUnknown && c.Shortcut(b.mods, b.key)
}

func (a *application) overlayShortcut(layer ui.Element, id string) bool {
	b := a.shortcutBinding(id)
	return a.shortcutEditing == "" && b.key != ui.KeyUnknown && layer.OverlayShortcut(b.mods, b.key)
}

func (a *application) onShortcut(c *ui.Context, id string, run func()) {
	b := a.shortcutBinding(id)
	if a.shortcutEditing == "" && b.key != ui.KeyUnknown && c.Shortcut(b.mods, b.key) {
		run()
		c.After(0)
	}
}

func (a *application) registerShortcuts(c *ui.Context) {
	if a.tab != 2 || a.settingsTab != 4 {
		a.cancelShortcutRecording()
	}
	if a.nativeMenuState != a.nativeMenuStateKey() {
		a.refreshShortcutMenus()
	}
	a.onShortcut(c, "quit", mygo.App.Quit)
	a.onShortcut(c, "settings", func() { a.navigate(2) })
	a.onShortcut(c, "sidebar", a.toggleSidebar)
	a.onShortcut(c, "choose", a.choose)
	a.onShortcut(c, "quick-upload", func() { a.chooseFiles(true) })
	a.onShortcut(c, "quick-paste", func() { a.pasteImage(true) })
	a.onShortcut(c, "window-close", func() {
		if a.win != nil {
			a.win.Close()
		}
	})
	a.onShortcut(c, "window-minimize", func() {
		if a.win != nil {
			a.win.Minimize()
		}
	})
	if runtime.GOOS == "darwin" {
		a.onShortcut(c, "window-hide", mygo.App.Hide)
	}
	if a.tab == 0 {
		a.onShortcut(c, "paste", a.paste)
		a.onShortcut(c, "upload", a.upload)
	}
}

func (a *application) nativeMenuStateKey() string {
	var key strings.Builder
	fmt.Fprintf(&key, "%t|%t|%t|%t", a.tab == 0, a.shortcutEditing != "", a.quickUpload.visible, a.fileDialogOpen)
	for _, command := range shortcutCommands {
		if globalShortcut(command) || command.id == "paste" {
			key.WriteByte('|')
			key.WriteString(a.shortcutBinding(command.id).value())
		}
	}
	return key.String()
}

// Only global commands get native menu accelerators. Scoped gallery commands
// stay in the UI so editors retain Select All and caret movement.
func (a *application) nativeShortcut(id string) string {
	command, ok := shortcutDefinition(id)
	if a.shortcutEditing != "" || !ok || !globalShortcut(command) {
		return ""
	}
	b := a.shortcutBinding(id)
	return b.value()
}

func (a *application) shortcutTooltip(label, id string) string {
	b := a.shortcutBinding(id)
	if b.key != ui.KeyUnknown {
		return tr(label) + " (" + b.label() + ")"
	}
	return tr(label)
}

func (a *application) shortcutsSettings(c *ui.Context) {
	ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
		components.Text(c, tr("点击键位后按下新的快捷键。")).Grow(1).MinWidth(0).Wrap().FontSize(11).TextColor(c.Theme().TextMuted)
		if components.Button(c, tr("恢复全部默认"), components.ButtonOutline).Shrink(0).Clicked() {
			a.config.Shortcuts = nil
			a.shortcutEditing, a.shortcutError = "", ""
			a.refreshShortcutMenus()
		}
	})
	for _, scope := range []string{"通用", "上传", "快捷上传", "图库", "图片预览", "窗口"} {
		settingsGroup(c, "shortcuts-"+scope, scope, func() {
			first := true
			for _, command := range shortcutCommands {
				if command.scope != scope || command.macOnly && runtime.GOOS != "darwin" {
					continue
				}
				if !first {
					settingsDivider(c)
				}
				first = false
				a.shortcutPreference(c, command)
			}
		})
	}
}

func (a *application) shortcutPreference(c *ui.Context, command shortcutCommand) {
	b := a.shortcutBinding(command.id)
	defaults, _ := parseShortcut(command.defaults)
	description := tf("默认：%s", defaults.label())
	if a.shortcutEditing == command.id {
		description = tr("请按下快捷键，Esc 取消。")
		if a.shortcutError != "" {
			description = a.shortcutError
		}
	}
	a.preference(c, "shortcut-"+command.id, command.title, description, func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			caption := b.label()
			if a.shortcutEditing == command.id {
				caption = tr("请按快捷键")
			}
			button := components.Button(c, caption, components.ButtonOutline).Grow(1).MinWidth(0).Label(tf("修改 %s 的快捷键", tr(command.title)))
			if a.shortcutEditing == command.id {
				button.BorderColor(c.Theme().Accent).HandleInput(func(event ui.InputEvent) bool {
					if event.Kind != ui.InputKeyDown || event.Repeat {
						return event.Kind == ui.InputText
					}
					if event.Key == ui.KeyEscape && event.Mods == 0 {
						a.cancelShortcutRecording()
						return true
					}
					key := event.Key
					if key == ui.KeyUnknown {
						key = event.PhysicalKey
					}
					if key != ui.KeyUnknown {
						a.setShortcut(command.id, shortcutBinding{mods: event.Mods, key: key})
					}
					return true
				})
				if !button.Focused() && !button.Clicked() {
					a.cancelShortcutRecording()
				}
			}
			if button.Clicked() {
				a.shortcutEditing, a.shortcutError = command.id, ""
				button.Focus()
				a.refreshShortcutMenus()
			}
			if components.IconButton(c, lucide.X(), tf("清除 %s 的快捷键", tr(command.title)), components.IconButtonPlain).Size(30, 30).Disabled(b.key == ui.KeyUnknown).Clicked() {
				a.setShortcut(command.id, shortcutBinding{})
			}
			if components.IconButton(c, lucide.RotateCcw(), tf("恢复 %s 的默认快捷键", tr(command.title)), components.IconButtonPlain).Size(30, 30).Disabled(b == defaults).Clicked() {
				if !a.setShortcut(command.id, defaults) {
					a.shortcutEditing = command.id
					button.Focus()
					a.refreshShortcutMenus()
				}
			}
		})
	})
}
