package gui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/knothhe/picup"
)

type updateCandidate struct {
	version, notes string
	install        func(context.Context, func(int64, int64)) error
}
type updateRuntime struct {
	check        func(context.Context) (*updateCandidate, error)
	dispatch     func(func())
	now          func() time.Time
	relaunch     func()
	quit         func()
	confirm      func(mygo.MessageOptions) (mygo.MessageResult, error)
	checkTimeout time.Duration
}
type updatesState struct {
	initialized, eligible       bool
	reason                      string
	preferences                 updatePreferences
	store                       *updatePreferenceStore
	runtime                     *updateRuntime
	phase                       string
	candidate                   *updateCandidate
	err                         error
	operationErr, preferenceErr error
	preferencesSaving           bool
	downloaded, total           int64
	cancel                      context.CancelFunc
	timer, quitTimer            *time.Timer
	timerSerial                 uint64
	quitAfterWork, confirming   bool
	quitAfterPreferences        bool
	offResume                   func()
	suppressAutomaticUntil      time.Time
	candidateAutoBlocked        bool
	preferencesReadBlocked      bool
}

func defaultUpdateRuntime() *updateRuntime {
	return &updateRuntime{
		check: func(ctx context.Context) (*updateCandidate, error) {
			u, e := mygo.Updater.Check(ctx)
			if e != nil || u == nil {
				return nil, e
			}
			return &updateCandidate{u.Version, u.Notes, u.Install}, nil
		},
		dispatch: mygo.RunOnMain, now: time.Now, relaunch: mygo.App.Relaunch, quit: mygo.App.Quit, confirm: mygo.Dialog.Message,
	}
}
func (a *application) initUpdates() {
	a.updates.runtime = defaultUpdateRuntime()
	a.updates.phase = "loading"
	go func() {
		exe, e := os.Executable()
		reason := ""
		cfg := picup.UpdateConfig()
		configured := cfg.PublicKey != "" && (cfg.GitHub != "" || cfg.URL != "")
		if e != nil {
			reason = "无法确定应用安装位置，更新已停用。"
		} else {
			if real, err := filepath.EvalSymlinks(exe); err == nil {
				exe = real
			}
			dev := updateDevelopmentBuild()
			reason = updateEligibilityReason(runtime.GOOS, exe, dev, configured, true)
			if reason == "" {
				reason = updateEligibilityReason(runtime.GOOS, exe, dev, configured, mygo.Updater.Enabled())
			}
		}
		store, loadErr := newUpdatePreferenceStore(filepath.Join(filepath.Dir(a.path), "updates-state.json"), updateScope(exe), time.Now())
		mygo.RunOnMain(func() {
			if a.quitting {
				return
			}
			u := &a.updates
			u.initialized = true
			u.eligible = reason == ""
			u.reason = reason
			u.store = store
			u.preferences = store.snapshot()
			a.setUpdateError(false, loadErr)
			u.preferencesReadBlocked = store.readFailed
			u.phase = "idle"
			if !u.eligible {
				u.phase = "unavailable"
			}
			u.offResume = mygo.Power.OnResume(func() { mygo.RunOnMain(func() { a.scheduleUpdateCheck() }) })
			a.scheduleUpdateCheck()
			a.invalidateWindows()
		})
	}()
}
func (a *application) stopUpdateTimer() {
	u := &a.updates
	u.timerSerial++
	if u.timer != nil {
		u.timer.Stop()
		u.timer = nil
	}
}
func (a *application) scheduleUpdateCheck() {
	a.stopUpdateTimer()
	u := &a.updates
	if a.quitting || !u.initialized || !u.eligible || !u.preferences.AutomaticChecks || u.preferencesReadBlocked || u.phase == "checking" || u.phase == "installing" || u.phase == "ready" {
		return
	}
	if u.candidate != nil && (!u.preferences.AutomaticDownloads || u.candidateAutoBlocked) {
		return
	}
	now := u.runtime.now()
	delay := max(updateStartupDelay, nextUpdateCheck(u.preferences, now).Sub(now))
	if u.candidate != nil {
		delay = max(time.Minute, u.preferences.RetryAfter.Sub(now))
	}
	delay = max(delay, u.suppressAutomaticUntil.Sub(now))
	serial := u.timerSerial
	// Re-evaluate wall time periodically; a clock jump must not leave a
	// monotonic day-long timer postponing a due check. This performs no HTTP.
	u.timer = time.AfterFunc(min(delay, time.Minute), func() {
		u.runtime.dispatch(func() {
			if serial != u.timerSerial || a.quitting {
				return
			}
			u.timer = nil
			now := u.runtime.now()
			retry := u.preferences.RetryAfter
			if retry.After(now.Add(updateRetryInterval + 5*time.Minute)) {
				retry = time.Time{}
			}
			if u.suppressAutomaticUntil.After(now.Add(updateInterval + 5*time.Minute)) {
				u.suppressAutomaticUntil = time.Time{}
			}
			if u.suppressAutomaticUntil.After(now) || (u.candidate == nil && nextUpdateCheck(u.preferences, now).After(now)) || (u.candidate != nil && retry.After(now)) {
				a.scheduleUpdateCheck()
				return
			}
			if !a.updateInstallReady(false) {
				a.deferAutomaticUpdate()
				return
			}
			if u.candidate != nil {
				a.installUpdate(false)
			} else {
				a.checkForUpdates(false)
			}
		})
	})
}
func (a *application) deferAutomaticUpdate() {
	a.stopUpdateTimer()
	u := &a.updates
	if !u.eligible || !u.preferences.AutomaticChecks || u.preferencesReadBlocked || u.candidateAutoBlocked || a.quitting {
		return
	}
	serial := u.timerSerial
	u.timer = time.AfterFunc(time.Minute, func() {
		u.runtime.dispatch(func() {
			if serial != u.timerSerial {
				return
			}
			if !a.updateInstallReady(false) {
				a.deferAutomaticUpdate()
				return
			}
			if u.candidate != nil {
				if u.preferences.AutomaticDownloads {
					a.installUpdate(false)
				}
			} else {
				a.checkForUpdates(false)
			}
		})
	})
}
func (a *application) applyUpdatePreferences(p updatePreferences) {
	if p.revision >= a.updates.preferences.revision {
		a.updates.preferences = p
	}
}
func (a *application) setUpdatePreference(checks, enabled bool) {
	u := &a.updates
	if !u.initialized || !u.eligible || u.preferencesSaving || u.phase == "installing" {
		return
	}
	u.preferencesSaving = true
	if !checks && enabled {
		u.candidateAutoBlocked = false
	}
	a.setUpdateError(true, nil)
	store, rt := u.store, u.runtime
	go func() {
		p, e := store.change(func(p *updatePreferences) {
			if checks {
				p.AutomaticChecks = enabled
			} else {
				p.AutomaticDownloads = enabled
			}
		})
		rt.dispatch(func() {
			if a.quitting {
				return
			}
			a.updates.preferencesSaving = false
			a.applyUpdatePreferences(p)
			a.setUpdateError(true, e)
			if e == nil {
				a.updates.preferencesReadBlocked = false
			}
			if a.updates.quitAfterPreferences {
				a.updates.quitAfterPreferences = false
				if a.updates.quitTimer != nil {
					a.updates.quitTimer.Stop()
					a.updates.quitTimer = nil
				}
				if e == nil {
					rt.quit()
					return
				}
				a.showWindow()
			}
			a.scheduleUpdateCheck()
			a.invalidateWindows()
		})
	}()
}
func (a *application) checkForUpdates(user bool) {
	u := &a.updates
	if user {
		a.showPage(5)
	}
	if !u.initialized {
		return
	}
	if !u.eligible {
		a.setUpdateError(false, errors.New(tr(u.reason)))
		a.invalidateWindows()
		return
	}
	if u.phase == "checking" || u.phase == "installing" || u.candidate != nil {
		return
	}
	a.stopUpdateTimer()
	u.phase = "checking"
	if user {
		u.suppressAutomaticUntil = time.Time{}
	}
	a.setUpdateError(false, nil)
	timeout := u.runtime.checkTimeout
	if timeout <= 0 {
		timeout = time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	u.cancel = cancel
	rt, store := u.runtime, u.store
	a.invalidateWindows()
	go func() {
		candidate, e := rt.check(ctx)
		if ctx.Err() != nil && !errors.Is(e, ctx.Err()) {
			e = errors.Join(ctx.Err(), e)
		}
		canceled := errors.Is(ctx.Err(), context.Canceled) && onlyUpdateCancellation(e)
		cancel()
		var p updatePreferences
		var saveErr error
		if !canceled {
			p, saveErr = store.change(func(p *updatePreferences) {
				if e == nil {
					p.LastSuccess = rt.now()
					p.RetryAfter = time.Time{}
				} else {
					p.RetryAfter = rt.now().Add(updateRetryInterval)
				}
			})
		}
		rt.dispatch(func() { a.finishUpdateCheck(candidate, e, saveErr, p, canceled, user) })
	}()
}
func (a *application) finishUpdateCheck(candidate *updateCandidate, checkErr, saveErr error, p updatePreferences, canceled, user bool) {
	u := &a.updates
	if a.quitting {
		return
	}
	u.cancel = nil
	u.phase = "idle"
	if canceled {
		u.suppressAutomaticUntil = u.runtime.now().Add(updateInterval)
		a.setUpdateError(false, nil)
		a.scheduleUpdateCheck()
		a.invalidateWindows()
		return
	}
	a.applyUpdatePreferences(p)
	a.setUpdateError(false, errors.Join(checkErr, saveErr))
	if saveErr == nil {
		u.preferencesReadBlocked = false
	}
	if saveErr != nil {
		a.deferUpdateRetry()
		u.candidateAutoBlocked = true
		u.preferences.RetryAfter = u.runtime.now().Add(updateRetryInterval)
	}
	if checkErr == nil && candidate != nil {
		u.candidate = candidate
		u.phase = "available"
		if u.preferences.AutomaticDownloads && saveErr == nil {
			if a.updateInstallReady(false) {
				a.installUpdate(false)
			} else {
				a.deferAutomaticUpdate()
			}
		} else if !user {
			a.feedbackWithAction("发现 Picup 新版本", nil, "查看更新", func() { a.showPage(5) })
		}
	} else if checkErr == nil {
		u.phase = "current"
	}
	a.scheduleUpdateCheck()
	a.invalidateWindows()
}
func (a *application) cancelUpdateOperation() {
	if a.updates.cancel != nil {
		a.updates.cancel()
	}
}
func (a *application) updateInstallReady(restart bool) bool {
	if a.quitting || a.updates.preferencesSaving || a.busy || a.uploading || a.fileDialogOpen || a.quickUpload.active || a.gallery.operation || a.gallery.busy || a.gallery.thumbLoading || a.gallery.fullCancel != nil || a.r2.active || a.r2.working || a.settingsSave.pending || a.settingsSave.failed || a.profiles.editOpen || a.profiles.deleteOpen || a.gallery.renameOpen || a.gallery.deleteOpen || a.shortcutEditing != "" {
		return false
	}
	if a.settingsSave.initialized && !reflect.DeepEqual(a.currentSettings(), a.settingsSave.persisted) {
		return false
	}
	if restart {
		for _, j := range a.jobs {
			if j.result == nil {
				return false
			}
		}
	}
	return true
}
func (a *application) installUpdate(user bool) {
	u := &a.updates
	if !u.eligible || u.candidate == nil || u.phase == "installing" || u.phase == "ready" {
		return
	}
	if !a.updateInstallReady(false) {
		if user {
			a.setUpdateError(false, errors.New(tr("请先完成上传、授权和编辑，再准备更新。")))
			a.invalidateWindows()
		} else {
			a.deferAutomaticUpdate()
		}
		return
	}
	a.stopUpdateTimer()
	u.phase = "installing"
	u.candidateAutoBlocked = false
	a.setUpdateError(false, nil)
	u.downloaded, u.total = 0, 0
	a.busy = true
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	u.cancel = cancel
	candidate, rt, store := u.candidate, u.runtime, u.store
	a.invalidateWindows()
	go func() {
		e := candidate.install(ctx, func(downloaded, total int64) {
			rt.dispatch(func() {
				if !a.quitting && a.updates.phase == "installing" {
					a.updates.downloaded, a.updates.total = downloaded, total
					a.invalidateWindows()
				}
			})
		})
		cancel()
		var p updatePreferences
		var saveErr error
		if e != nil && !onlyUpdateCancellation(e) {
			p, saveErr = store.change(func(p *updatePreferences) { p.RetryAfter = rt.now().Add(updateRetryInterval) })
		}
		rt.dispatch(func() {
			if saveErr == nil && p.Version != 0 {
				a.applyUpdatePreferences(p)
			}
			a.finishUpdateInstall(errors.Join(e, saveErr))
		})
	}()
}
func (a *application) finishUpdateInstall(err error) {
	u := &a.updates
	u.cancel = nil
	a.busy = false
	if u.quitTimer != nil {
		u.quitTimer.Stop()
		u.quitTimer = nil
	}
	if err == nil {
		u.phase = "ready"
		a.setUpdateError(false, nil)
	} else {
		u.phase = "available"
		u.candidateAutoBlocked = errors.Is(err, context.Canceled)
		if !onlyUpdateCancellation(err) {
			a.setUpdateError(false, err)
			a.deferUpdateRetry()
		}
		u.preferences.RetryAfter = u.runtime.now().Add(updateRetryInterval)
	}
	if a.quitting {
		return
	}
	quit := u.quitAfterWork
	u.quitAfterWork = false
	if quit {
		u.runtime.quit()
		return
	}
	a.scheduleUpdateCheck()
	a.invalidateWindows()
}
func (a *application) confirmUpdateRestart() {
	u := &a.updates
	if u.phase != "ready" || u.confirming {
		return
	}
	if !a.updateInstallReady(true) {
		a.setUpdateError(false, errors.New(tr("请先完成上传队列、授权和编辑，再重新启动。")))
		a.invalidateWindows()
		return
	}
	if !a.flushSettingsAutosave() {
		return
	}
	u.confirming = true
	a.busy = true
	rt := u.runtime
	parent := a.win
	go func() {
		r, e := rt.confirm(mygo.MessageOptions{Parent: parent, Type: mygo.MessageQuestion, Title: tr("重新启动 Picup"), Message: tr("更新已准备好，是否退出并重新启动？"), Detail: tr("当前配置会保留。新版本将在重新启动后生效。"), Buttons: []string{tr("稍后"), tr("重新启动")}, DefaultButton: 1, CancelButton: 0})
		rt.dispatch(func() {
			a.updates.confirming = false
			a.busy = false
			if a.quitting {
				return
			}
			if e != nil {
				a.setUpdateError(false, e)
			} else if r.Button == 1 {
				if a.updateInstallReady(true) && a.flushSettingsAutosave() {
					rt.relaunch()
				} else {
					a.setUpdateError(false, errors.New(tr("请先完成上传队列、授权和编辑，再重新启动。")))
				}
			}
			a.invalidateWindows()
		})
	}()
}
func (a *application) prepareUpdateQuit(e *mygo.QuitEvent) bool {
	u := &a.updates
	if u.preferencesSaving {
		e.PreventDefault()
		u.quitAfterPreferences = true
		if u.quitTimer == nil {
			u.quitTimer = time.AfterFunc(20*time.Second, func() {
				u.runtime.dispatch(func() {
					if u.preferencesSaving && u.quitAfterPreferences {
						u.quitAfterPreferences = false
						u.quitTimer = nil
						a.setUpdateError(false, errors.New(tr("更新偏好仍在保存，请稍后重试退出。")))
						a.showWindow()
						a.invalidateWindows()
					}
				})
			})
		}
		return false
	}
	if u.phase == "installing" {
		e.PreventDefault()
		u.quitAfterWork = true
		a.cancelUpdateOperation()
		if u.quitTimer == nil {
			u.quitTimer = time.AfterFunc(20*time.Second, func() {
				u.runtime.dispatch(func() {
					if u.phase == "installing" && u.quitAfterWork {
						u.quitAfterWork = false
						u.quitTimer = nil
						a.setUpdateError(false, errors.New(tr("更新仍在收尾，请稍后重试退出。")))
						a.showWindow()
						a.invalidateWindows()
					}
				})
			})
		}
		return false
	}
	if u.confirming {
		e.PreventDefault()
		return false
	}
	a.stopUpdateTimer()
	a.cancelUpdateOperation()
	return true
}
func (a *application) stopUpdates() {
	a.stopUpdateTimer()
	a.cancelUpdateOperation()
	if a.updates.quitTimer != nil {
		a.updates.quitTimer.Stop()
	}
	if a.updates.offResume != nil {
		a.updates.offResume()
	}
}
func updateReleaseURL(version string) string {
	cfg := picup.UpdateConfig()
	if cfg.GitHub == "" || strings.Count(cfg.GitHub, "/") != 1 {
		return ""
	}
	prefix := cfg.TagPrefix
	if prefix == "" {
		prefix = "v"
	}
	return "https://github.com/" + cfg.GitHub + "/releases/tag/" + url.PathEscape(prefix+version)
}
func updateLastCheckText(t time.Time) string {
	if t.IsZero() {
		return tr("尚未成功检查")
	}
	return tf("上次成功检查：%s", t.Local().Format("2006-01-02 15:04"))
}
func updateProgressText(downloaded, total int64) string {
	if total <= 0 {
		return tr("正在下载、校验并准备更新…")
	}
	return fmt.Sprintf("%.1f / %.1f MiB", float64(downloaded)/1048576, float64(total)/1048576)
}

func (a *application) deferUpdateCandidate() {
	u := &a.updates
	if u.phase != "available" || u.candidate == nil {
		return
	}
	u.candidate = nil
	u.candidateAutoBlocked = false
	u.suppressAutomaticUntil = u.runtime.now().Add(updateInterval)
	u.phase = "idle"
	a.setUpdateError(false, nil)
	a.scheduleUpdateCheck()
	a.invalidateWindows()
}

func (a *application) setUpdateError(preference bool, err error) {
	u := &a.updates
	if preference {
		u.preferenceErr = err
	} else {
		u.operationErr = err
	}
	u.err = errors.Join(u.operationErr, u.preferenceErr)
}
func (a *application) deferUpdateRetry() {
	u := &a.updates
	until := u.runtime.now().Add(updateRetryInterval)
	if u.suppressAutomaticUntil.Before(until) {
		u.suppressAutomaticUntil = until
	}
}
func onlyUpdateCancellation(err error) bool {
	if err == nil {
		return false
	}
	if err == context.Canceled {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !onlyUpdateCancellation(cause) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return onlyUpdateCancellation(wrapped.Unwrap())
	}
	return false
}
