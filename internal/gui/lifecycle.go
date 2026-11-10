package gui

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/egoist/mygo"
	"github.com/knothhe/picup/assets"
	"github.com/knothhe/picup/internal/buildidentity"
	"github.com/knothhe/picup/internal/config"
	"github.com/oneo-me/onevra/source/shells/plain"
)

const lightweightIdleDelay = time.Minute

// All lifecycle state belongs to the UI thread. Native integrations outlive
// expendable windows and image presentation caches.
type lifecycleState struct {
	nativeReady, mainVisible, suspended bool
	hiddenAt                            time.Time
	timer                               *time.Timer
	timerSerial                         uint64
	dockVisible, dockVisibilityApplied  bool
	openAtLogin                         bool
	setDockVisible                      func(bool)
	setOpenAtLogin                      func(bool) error
}

func (a *application) newMainWindow() {
	shell := plain.NewShell(plain.ShellOptions{Hidden: true, Title: buildidentity.Name(), Width: 1040, Height: 760, MinWidth: 760, MinHeight: 620, StateKey: "main", Theme: palette, Content: a.view, ContentBehindHeader: true, HeaderLeading: a.header, HeaderActions: a.headerActions})
	w := shell.Window()
	a.win = w
	if runtime.GOOS != "darwin" {
		_ = w.SetIcon(assets.IconPNG)
	}
	w.OnFocus(func() {
		a.refreshUploadConfig()
		w.Invalidate()
	})
	w.OnBlur(func() {
		// Resume at the native event, even if an occluded window draws later.
		a.resumeNotificationTimers(time.Now())
		w.Invalidate()
	})
	w.OnShow(func() { a.mainWindowVisibilityChanged(true) })
	w.OnHide(func() { a.mainWindowVisibilityChanged(false) })
	w.OnClose(a.handleMainWindowClose)
	w.OnClosed(func() {
		if a.win == w {
			a.win = nil
			a.mainWindowVisibilityChanged(false)
		}
	})
	w.OnFileDrop(func(e *mygo.FileDropEvent) { a.addPageFiles(e.Paths) })
}

func (a *application) handleMainWindowClose(e *mygo.CloseEvent) {
	if a.quitting {
		return
	}
	e.PreventDefault()
	if a.r2.working && !a.r2.active {
		return
	}
	a.cancelR2Connection()
	if !a.flushSettingsAutosave() {
		a.showWindow()
		return
	}
	if !a.flushImageToolsPreferences() {
		a.showWindow()
		return
	}
	a.mobileTransfer.open = false
	a.stopMobileTransfer()
	if a.config.CloseToTray && a.tray != nil {
		a.win.Hide()
		return
	}
	mygo.App.Quit()
}

func (a *application) startNativeSettings() {
	a.lifecycle.nativeReady = true
	if runtime.GOOS == "darwin" {
		// MyGo starts with a visible Dock icon; showing it again activates the app.
		a.lifecycle.dockVisible, a.lifecycle.dockVisibilityApplied = true, true
		a.lifecycle.setDockVisible = func(visible bool) {
			if visible {
				mygo.App.Dock.Show()
			} else {
				mygo.App.Dock.Hide()
			}
		}
	}
	a.lifecycle.setOpenAtLogin = mygo.App.SetOpenAtLogin
	a.lifecycle.openAtLogin = mygo.App.OpenAtLogin()
	// Respect changes made in System Settings instead of re-enabling login.
	if a.config.LaunchAtLogin != a.lifecycle.openAtLogin {
		a.config.LaunchAtLogin = a.lifecycle.openAtLogin
		a.save()
	}
}

func dockIconVisible(policy string, mainVisible bool) bool {
	return policy == config.DockIconShow || policy == config.DockIconFollowWindow && mainVisible
}

func (a *application) updateDockVisibility() {
	if a.lifecycle.setDockVisible == nil || a.quitting {
		return
	}
	visible := dockIconVisible(a.config.DockIcon, a.lifecycle.mainVisible)
	if a.lifecycle.dockVisibilityApplied && a.lifecycle.dockVisible == visible {
		return
	}
	a.lifecycle.dockVisible, a.lifecycle.dockVisibilityApplied = visible, true
	a.lifecycle.setDockVisible(visible)
}

func (a *application) mainWindowVisibilityChanged(visible bool) {
	a.lifecycle.mainVisible = visible
	if !visible {
		a.resumeNotificationTimers(time.Now())
	}
	a.applyNativeSettings()
}

func (a *application) applyNativeSettings() {
	a.updateDockVisibility()
	a.updateLightweightTimer(time.Now())
}

func (a *application) trayClicked() {
	if a.config.TrayClickAction == config.TrayClickMainWindow {
		a.showWindow()
	} else {
		a.toggleQuickUpload()
	}
}

// Change the OS registration before saving. A failed write restores it.
func (a *application) prepareLaunchAtLogin(enabled bool) (func(bool) error, error) {
	previous := a.lifecycle.openAtLogin
	if a.lifecycle.setOpenAtLogin == nil || enabled == previous {
		return func(bool) error { return nil }, nil
	}
	if err := a.lifecycle.setOpenAtLogin(enabled); err != nil {
		return nil, fmt.Errorf("%s: %w", tr("更改开机自启失败"), err)
	}
	a.lifecycle.openAtLogin = enabled
	return func(saved bool) error {
		if !saved {
			if err := a.lifecycle.setOpenAtLogin(previous); err != nil {
				return fmt.Errorf("%s: %w", tr("恢复开机自启失败"), err)
			}
			a.lifecycle.openAtLogin = previous
		}
		return nil
	}, nil
}

func (a *application) stopLightweightTimer() {
	a.lifecycle.timerSerial++
	if a.lifecycle.timer != nil {
		a.lifecycle.timer.Stop()
		a.lifecycle.timer = nil
	}
}

func (a *application) updateLightweightTimer(now time.Time) {
	if !a.lifecycle.nativeReady || a.quitting || !a.config.LightweightMode || a.lifecycle.mainVisible || a.quickUpload.visible || a.lifecycle.suspended || a.tray == nil {
		a.stopLightweightTimer()
		a.lifecycle.hiddenAt = time.Time{}
		return
	}
	if a.lifecycle.hiddenAt.IsZero() {
		a.lifecycle.hiddenAt = now
	}
	if a.lifecycle.timer == nil {
		a.scheduleLightweightCheck(max(time.Second, lightweightIdleDelay-now.Sub(a.lifecycle.hiddenAt)))
	}
}

func (a *application) scheduleLightweightCheck(delay time.Duration) {
	a.stopLightweightTimer()
	serial := a.lifecycle.timerSerial
	a.lifecycle.timer = time.AfterFunc(delay, func() {
		mygo.RunOnMain(func() {
			if serial != a.lifecycle.timerSerial {
				return
			}
			a.lifecycle.timer = nil
			if !a.suspendLightweightUI(time.Now()) {
				a.updateLightweightTimer(time.Now())
			}
		})
	})
}

func (a *application) lightweightSuspensionReady(now time.Time) bool {
	return a.config.LightweightMode && !a.quitting && !a.lifecycle.suspended &&
		!a.mobileReceiving() &&
		!a.lifecycle.mainVisible && !a.quickUpload.visible && !a.lifecycle.hiddenAt.IsZero() &&
		now.Sub(a.lifecycle.hiddenAt) >= lightweightIdleDelay &&
		!a.busy && !a.uploading && !a.fileDialogOpen && !a.quickUpload.active && !a.gallery.operation && !a.r2.active && !a.r2.working &&
		!a.settingsSave.pending && !a.settingsSave.failed && !a.profiles.editOpen && !a.profiles.deleteOpen &&
		!a.gallery.renameOpen && !a.gallery.deleteOpen && a.shortcutEditing == "" && !a.updates.confirming && a.updates.phase != "installing"
}

func (a *application) suspendLightweightUI(now time.Time) bool {
	if a.settingsSave.pending && !a.settingsSave.failed && !a.lifecycle.mainVisible && !a.quickUpload.visible &&
		!a.lifecycle.hiddenAt.IsZero() && now.Sub(a.lifecycle.hiddenAt) >= lightweightIdleDelay {
		a.flushSettingsAutosave()
	}
	if !a.lightweightSuspensionReady(now) {
		return false
	}
	a.stopLightweightTimer()
	a.lifecycle.suspended = true // Destroying the final window must retain the process.
	main, quick := a.win, a.quickUpload.window
	a.win, a.quickUpload.window = nil, nil
	if quick != nil {
		quick.Destroy()
	}
	if main != nil {
		main.Destroy()
	}
	a.releaseUIPresentation()
	go debug.FreeOSMemory()
	return true
}

func (a *application) releaseUIPresentation() {
	a.uploadConfigGeneration++
	a.flushGalleryPreferences()
	a.resetGallery() // Cancels reads and rejects their late results by generation.
	if a.uploadPreviewCancel != nil {
		a.uploadPreviewCancel()
		a.uploadPreviewCancel = nil
	}
	a.uploadPreviews = uploadPreviewState{generation: a.uploadPreviews.generation + 1, slots: a.uploadPreviews.slots}
	for _, j := range a.jobs {
		j.preview, j.previewLoading, j.previewDone = nil, false, false
	}
	for _, j := range a.imageTools.jobs {
		j.source.preview, j.source.previewLoading, j.source.previewDone = nil, false, false
	}
	a.profiles = profileUIState{}
	a.notifications = nil
	a.notificationDeadlines = nil
	a.notificationPausedAt = time.Time{}
}

func (a *application) resumeLightweightUI() {
	a.stopLightweightTimer()
	a.lifecycle.hiddenAt = time.Time{}
	a.lifecycle.suspended = false
}
