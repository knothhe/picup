package gui

import (
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/oneo-me/onevra/source/components"
)

var nativeQuickCommands = []string{"quick-upload", "quick-paste"}

// The controller owns only the hotkeys it registered. Preparing a new set
// acquires its new keys before releasing old ones; failed storage can therefore
// roll back without trying to reclaim a key another application might take.
type nativeHotkeys struct {
	register   func(string, func()) error
	unregister func(string)
	invoke     func(string)
	intercept  func(string) bool
	registered map[string]bool
	routes     map[string]string // accelerator -> command; callbacks read live routes
}

func (h *nativeHotkeys) prepare(desired map[string]string) (func(bool), error) {
	if h.registered == nil {
		h.registered = map[string]bool{}
	}
	if h.routes == nil {
		h.routes = map[string]string{}
	}
	var added []string
	rollback := func() {
		for _, key := range added {
			h.unregister(key)
			delete(h.registered, key)
		}
	}
	// Stable ordering makes partial failure and its compensation predictable.
	for _, command := range nativeQuickCommands {
		for key, target := range desired {
			if target != command || h.registered[key] {
				continue
			}
			if err := h.register(key, func() {
				if h.intercept != nil && h.intercept(key) {
					return
				}
				if action := h.routes[key]; action != "" && h.invoke != nil {
					h.invoke(action)
				}
			}); err != nil {
				rollback()
				definition, _ := shortcutDefinition(command)
				binding, _ := parseShortcut(key)
				return nil, fmt.Errorf("%s (%s): %w", tr(definition.title), binding.label(), err)
			}
			h.registered[key] = true
			added = append(added, key)
		}
	}
	finished := false
	return func(saved bool) {
		if finished {
			return
		}
		finished = true
		if !saved {
			rollback()
			return
		}
		h.routes = maps.Clone(desired)
		for key := range h.registered {
			if _, keep := desired[key]; !keep {
				h.unregister(key)
				delete(h.registered, key)
			}
		}
	}, nil
}

func (h *nativeHotkeys) stop() {
	h.intercept, h.invoke = nil, nil
	clear(h.routes)
	for key := range h.registered {
		h.unregister(key)
	}
	clear(h.registered)
}

type uploadNotificationSummary struct {
	done, failed int
	canceled     bool
}

type nativeActionsState struct {
	initialized         bool
	hotkeys             nativeHotkeys
	requestedHotkeys    map[string]string
	retryHotkeys        bool
	hotkeyErr           error
	notificationErr     error
	notificationDenied  bool
	notificationBusy    bool
	notificationSerial  uint64
	pendingNotification *uploadNotificationSummary
	offNotification     func()
	notificationSupport func() bool
	showNotification    func(mygo.NotificationOptions) error
	closeNotification   func()
	windowFocused       func() bool
	runWorker           func(func())
	runOnMain           func(func())
}

const uploadNotificationID = "picup/upload-result"

// Register before App.Run so a click from an earlier run can restore results.
func (a *application) bindNativeNotificationClicks() {
	if a.nativeActions.offNotification != nil {
		return
	}
	a.nativeActions.offNotification = mygo.App.OnNotificationClick(func(id string) {
		if id != uploadNotificationID || a.quitting {
			return
		}
		if a.nativeActions.closeNotification != nil {
			a.nativeActions.closeNotification()
		}
		a.showPage(0)
	})
}

func (a *application) initNativeActions() {
	s := &a.nativeActions
	if s.initialized {
		return
	}
	s.initialized = true
	if s.hotkeys.register == nil {
		s.hotkeys.register = mygo.GlobalShortcut.Register
		s.hotkeys.unregister = mygo.GlobalShortcut.Unregister
	}
	s.hotkeys.invoke = func(command string) {
		if a.quitting || a.busy || a.fileDialogOpen || a.shortcutEditing != "" {
			return
		}
		switch command {
		case "quick-upload":
			a.chooseFiles(true)
		case "quick-paste":
			a.pasteImage(true)
		}
	}
	// A registered OS hotkey can consume its key event before the recorder's
	// input handler sees it. Route that exact binding to the focused recorder
	// without releasing keys (and risking another app claiming them).
	s.hotkeys.intercept = func(key string) bool {
		if a.shortcutEditing == "" || a.quitting {
			return false
		}
		if s.windowFocused != nil && s.windowFocused() {
			if binding, valid := parseShortcut(key); valid {
				a.setShortcut(a.shortcutEditing, binding)
				a.invalidateWindows()
			}
		}
		return true
	}
	if s.notificationSupport == nil {
		s.notificationSupport = mygo.NotificationsSupported
		s.showNotification = func(options mygo.NotificationOptions) error { return mygo.NewNotification(options).Show() }
		s.closeNotification = func() { mygo.NewNotification(mygo.NotificationOptions{ID: uploadNotificationID}).Close() }
		s.windowFocused = func() bool {
			focused := mygo.FocusedWindow()
			return focused != nil && (focused == a.win || focused == a.quickUpload.window)
		}
		s.runWorker = func(fn func()) { go fn() }
		s.runOnMain = mygo.RunOnMain
	}
	// A conflicting binding does not disable the other quick command at startup.
	desired := nativeHotkeyBindings(a.config)
	s.requestedHotkeys = maps.Clone(desired)
	for _, command := range nativeQuickCommands {
		next := maps.Clone(s.hotkeys.routes)
		if next == nil {
			next = map[string]string{}
		}
		for key, target := range desired {
			if target == command {
				next[key] = target
			}
		}
		finish, err := s.hotkeys.prepare(next)
		if err != nil {
			s.hotkeyErr = errors.Join(s.hotkeyErr, err)
		} else {
			finish(true)
		}
	}
	if s.hotkeyErr != nil {
		a.feedbackWithAction("注册全局快捷键失败", s.hotkeyErr, "重试", a.retryNativeActions)
	}
	if a.win != nil || a.tray != nil {
		a.updateMenus()
	}
}

func nativeHotkeyBindings(c config.Config) map[string]string {
	result := map[string]string{}
	viewer := application{config: c}
	for _, command := range nativeQuickCommands {
		if binding := viewer.shortcutBinding(command); binding.key != ui.KeyUnknown {
			result[binding.value()] = command
		}
	}
	return result
}

// Called by the existing settings transaction before its config write.
func (a *application) prepareNativeActions(c config.Config) (func(bool), error) {
	s := &a.nativeActions
	if !s.initialized {
		return func(bool) {}, nil
	}
	desired := nativeHotkeyBindings(c)
	applyNotificationPreference := func() {
		if !c.NotifyOnComplete {
			s.pendingNotification = nil
			s.closeNotification()
		}
	}
	// A startup conflict must not prevent saving unrelated appearance, storage,
	// or notification preferences. Reattempt unchanged failed keys only through
	// the explicit retry action; changed bindings still use the transaction.
	if !s.retryHotkeys && maps.Equal(desired, s.requestedHotkeys) {
		return func(saved bool) {
			if saved {
				if maps.Equal(desired, s.hotkeys.routes) {
					s.hotkeyErr = nil
				}
				applyNotificationPreference()
			}
		}, nil
	}
	finish, err := s.hotkeys.prepare(desired)
	if err != nil {
		s.hotkeyErr = err
		return nil, fmt.Errorf("%s: %w", tr("全局快捷键未能生效，原有效绑定继续可用"), err)
	}
	return func(saved bool) {
		finish(saved)
		if saved {
			s.hotkeyErr = nil
			s.requestedHotkeys = maps.Clone(desired)
			applyNotificationPreference()
		}
	}, nil
}

func (a *application) retryNativeActions() {
	// The ordinary save transaction also commits pending edits, or preserves
	// the old effective bindings and the editor's recoverable draft on failure.
	a.nativeActions.retryHotkeys = true
	a.save()
	a.nativeActions.retryHotkeys = false
	a.invalidateWindows()
}

// A focus reload may adopt a CLI-edited configuration. It is already saved,
// so reconcile its hotkeys while preserving any previous effective bindings
// if the newly requested set is unavailable.
func (a *application) syncNativeActions() {
	finish, err := a.prepareNativeActions(a.config)
	if err != nil {
		a.feedbackWithAction("注册全局快捷键失败", err, "重试", a.retryNativeActions)
		return
	}
	finish(true)
}

func (a *application) globalQuickShortcutActive(command string) bool {
	if !a.nativeActions.initialized {
		return false
	}
	for _, target := range a.nativeActions.hotkeys.routes {
		if target == command {
			return true
		}
	}
	return false
}

func (a *application) stopNativeActions() {
	s := &a.nativeActions
	if s.initialized {
		s.hotkeys.stop()
	}
	s.initialized = false
	s.notificationSerial++
	s.pendingNotification = nil
	if s.offNotification != nil {
		s.offNotification()
		s.offNotification = nil
	}
}

func (a *application) nativeActionsSettings(c *ui.Context) {
	s := &a.nativeActions
	settingsGroup(c, "native-actions", "快捷上传与通知", func() {
		a.switchPreference(c, "notify-on-complete", "后台上传完成通知", "仅在 Picup 位于后台时显示；首次使用由系统询问权限。", &a.config.NotifyOnComplete)
		if s.initialized && !s.notificationSupport() {
			components.Text(c, tr("当前运行方式不支持系统通知；macOS 请使用打包的应用。")).Padding(0, 16, 14, 16).FillWidth().MinWidth(0).Wrap().FontSize(12).TextColor(c.Theme().TextMuted)
		}
		if s.notificationErr != nil {
			ui.Column(c).FillWidth().MinWidth(0).Padding(0, 16, 14, 16).Gap(8).Children(func() {
				message := "系统通知发送失败，可重试；上传结果仍保留。"
				if s.notificationDenied {
					message = "系统通知未获允许。请在系统设置中允许 Picup 通知，再点击重试。"
				}
				components.Text(c, tr(message)).FillWidth().MinWidth(0).Wrap().FontSize(12).TextColor(c.Theme().Danger)
				if components.Button(c, tr("重试通知"), components.ButtonOutline).AlignSelf(ui.Start).Disabled(!a.config.NotifyOnComplete || s.notificationBusy).Clicked() {
					a.retrySystemNotification()
				}
			})
		}
		settingsDivider(c)
		var active []string
		for _, command := range nativeQuickCommands {
			for key, target := range s.hotkeys.routes {
				if target == command {
					binding, _ := parseShortcut(key)
					definition, _ := shortcutDefinition(command)
					active = append(active, tr(definition.title)+" "+binding.label())
				}
			}
		}
		ui.Column(c).FillWidth().MinWidth(0).Padding(14, 16).Gap(8).Children(func() {
			components.Text(c, tr("快捷上传支持系统全局快捷键，在其他应用中也可使用。请在快捷键设置中修改或清除绑定。")).FillWidth().MinWidth(0).Wrap().FontSize(12).TextColor(c.Theme().TextMuted)
			if len(active) > 0 {
				components.Text(c, tf("当前全局快捷键：%s", strings.Join(active, " · "))).FillWidth().MinWidth(0).Wrap().FontSize(12)
			}
			if s.hotkeyErr != nil {
				components.Text(c, tr("全局快捷键未能生效。原有效绑定继续可用；请关闭冲突应用或修改快捷键后重试。")).FillWidth().MinWidth(0).Wrap().FontSize(12).TextColor(c.Theme().Danger)
				components.Text(c, s.hotkeyErr.Error()).FillWidth().MinWidth(0).Wrap().FontSize(11).TextColor(c.Theme().TextMuted)
				if components.Button(c, tr("重试全局快捷键"), components.ButtonOutline).AlignSelf(ui.Start).Clicked() {
					a.retryNativeActions()
				}
			}
		})
	})
}

func (a *application) notificationPreference() bool {
	if a.settingsSave.initialized {
		return a.settingsSave.persisted.NotifyOnComplete
	}
	return a.config.NotifyOnComplete
}

func (a *application) backgroundUploadFinished(done, failed int, canceled bool) {
	s := &a.nativeActions
	if !s.initialized || a.quitting || !a.notificationPreference() || s.notificationDenied || s.windowFocused() || done+failed == 0 {
		return
	}
	s.pendingNotification = &uploadNotificationSummary{done: done, failed: failed, canceled: canceled}
	a.deliverPendingUploadNotification()
}

func (a *application) deliverPendingUploadNotification() {
	s := &a.nativeActions
	if s.notificationBusy || s.pendingNotification == nil {
		return
	}
	result := s.pendingNotification
	s.pendingNotification = nil
	title := tf("已上传 %d 张图片", result.done)
	if result.failed > 0 {
		title = tf("已上传 %d 张，%d 张失败，可重试", result.done, result.failed)
	}
	if result.canceled {
		title = tr("上传已取消，已完成的链接仍可使用")
	}
	a.sendSystemNotification(title, tr("点击返回 Picup 查看上传结果。"))
}

func (a *application) retrySystemNotification() {
	s := &a.nativeActions
	if !s.initialized || a.quitting || !a.config.NotifyOnComplete || s.notificationBusy {
		return
	}
	s.notificationDenied, s.notificationErr = false, nil
	a.sendSystemNotification(tr("Picup 通知已开启"), tr("后台上传完成后将在这里提醒。点击可打开 Picup。"))
}

func (a *application) sendSystemNotification(title, body string) {
	s := &a.nativeActions
	if !s.notificationSupport() {
		return
	}
	s.notificationBusy = true
	s.notificationSerial++
	serial := s.notificationSerial
	options := mygo.NotificationOptions{ID: uploadNotificationID, Group: "picup-uploads", Title: title, Body: body, Silent: true}
	s.runWorker(func() {
		err := s.showNotification(options)
		s.runOnMain(func() {
			if !s.initialized || serial != s.notificationSerial || a.quitting {
				return
			}
			s.notificationBusy = false
			if !a.notificationPreference() {
				s.closeNotification()
				s.pendingNotification = nil
				return
			}
			s.notificationErr = err
			if errors.Is(err, mygo.ErrNotificationsDenied) {
				s.notificationDenied = true
				s.pendingNotification = nil
			} else if err == nil && s.pendingNotification != nil {
				a.deliverPendingUploadNotification()
			}
			a.invalidateWindows()
		})
	})
}
