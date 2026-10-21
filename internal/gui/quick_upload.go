package gui

import (
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/traydrop"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

type quickUploadState struct {
	window                  *mygo.Window
	visible, active, copied bool
	jobs                    []*job
	message                 string
	messageError            bool
}

const quickUploadWidth, quickUploadHeight = 360, 268

func (a *application) invalidateWindows() {
	if a.win != nil {
		a.win.Invalidate()
	}
	if a.quickUpload.window != nil {
		a.quickUpload.window.Invalidate()
	}
}

func (a *application) showQuickUpload() {
	if a.tray == nil || a.quitting {
		return
	}
	a.resumeLightweightUI()
	if a.quickUpload.window == nil {
		w := mygo.NewFlyout(mygo.FlyoutOptions{
			Tray: a.tray, Placement: mygo.PlacementBottom, Gap: 6,
			Width: quickUploadWidth, Height: quickUploadHeight, Focusable: true, Hidden: true, Shadow: true,
			Content: ui.View(a.quickUploadView),
		})
		a.quickUpload.window = w
		w.OnShow(func() {
			a.quickUpload.visible = true
			traydrop.SetPanelVisible(a.tray, true)
			a.updateLightweightTimer(time.Now())
		})
		w.OnHide(func() {
			a.quickUpload.visible = false
			traydrop.SetPanelVisible(a.tray, false)
			a.updateMenus()
			a.updateLightweightTimer(time.Now())
		})
		w.OnClosed(func() {
			if a.quickUpload.window == w {
				a.quickUpload.window = nil
				a.quickUpload.visible = false
				traydrop.SetPanelVisible(a.tray, false)
				a.updateLightweightTimer(time.Now())
			}
		})
		w.OnClose(func(e *mygo.CloseEvent) {
			if !a.quitting {
				e.PreventDefault()
				a.hideQuickUpload()
			}
		})
		w.OnFileDrop(func(e *mygo.FileDropEvent) { a.quickUploadFiles(e.Paths) })
	}
	a.quickUpload.visible = true
	a.updateMenus()
	a.quickUpload.window.Show()
	a.quickUpload.window.Focus()
	a.refreshUploadConfig()
}

func (a *application) hideQuickUpload() {
	if a.quickUpload.window != nil {
		a.quickUpload.window.Hide()
	}
	if a.quickUpload.visible {
		a.quickUpload.visible = false
		a.updateMenus()
	}
	a.updateLightweightTimer(time.Now())
}

func (a *application) toggleQuickUpload() {
	if a.quickUpload.visible {
		a.hideQuickUpload()
	} else {
		a.showQuickUpload()
	}
}

func (a *application) beginQuickUpload(jobs []*job) {
	a.quickUpload.jobs = append([]*job(nil), jobs...)
	a.quickUpload.active, a.quickUpload.copied = true, false
	a.quickUpload.message, a.quickUpload.messageError = "", false
}

func (a *application) canResetQuickUpload() bool {
	return !a.busy && !a.uploading && !a.fileDialogOpen &&
		(len(a.quickUpload.jobs) > 0 || a.quickUpload.message != "")
}

// Forget only the panel's last result. Shared jobs remain in the main queue.
func (a *application) resetQuickUpload() {
	if !a.canResetQuickUpload() {
		return
	}
	a.quickUpload.jobs = nil
	a.quickUpload.active, a.quickUpload.copied = false, false
	a.quickUpload.message, a.quickUpload.messageError = "", false
	a.invalidateWindows()
}

// Main-window retries update the same jobs. Discard the old batch's error and
// clipboard claim so reopening the panel derives its result from those jobs.
func (a *application) resetRetriedQuickUpload(selected []*job) {
	for _, selectedJob := range selected {
		for _, quickJob := range a.quickUpload.jobs {
			if selectedJob == quickJob {
				a.quickUpload.message, a.quickUpload.messageError = "", false
				a.quickUpload.copied = false
				return
			}
		}
	}
}

func (a *application) quickUploadFiles(paths []string) {
	if a.busy || len(paths) == 0 {
		return
	}
	if !a.quickUploadReady() {
		a.quickUpload.active = false
		return
	}
	start := len(a.jobs)
	a.addFiles(paths)
	a.beginQuickUpload(a.jobs[start:])
	a.showQuickUpload()
	a.uploadJobs(a.quickUpload.jobs)
}

func (a *application) quickUploadStatus() (message, detail string, failed bool) {
	if a.uploading {
		return tr("正在处理与上传"), tf("已处理 %d / %d", a.uploadBatchDone, a.uploadBatchTotal), false
	}
	if a.busy {
		return tr("正在准备图片…"), tr("当前任务完成后可继续上传"), false
	}
	if a.quickUpload.message != "" {
		detail = tr("打开 Picup 查看上传结果")
		if a.quickUpload.messageError {
			detail = tr("打开 Picup 查看详情并重试")
		} else if a.quickUpload.copied {
			detail = tr("结果已按输出偏好复制")
		}
		return a.quickUpload.message, detail, a.quickUpload.messageError
	}
	if len(a.quickUpload.jobs) > 0 {
		uploaded, errors := 0, 0
		for _, j := range a.quickUpload.jobs {
			if j.result != nil {
				uploaded++
			} else if j.status == "上传失败" {
				errors++
			}
		}
		if errors > 0 {
			return tf("已上传 %d 张，%d 张失败，可重试", uploaded, errors), tr("打开 Picup 查看详情并重试"), true
		}
		return tf("已上传 %d 张图片", uploaded), tr("打开 Picup 查看上传结果"), false
	}
	if a.uploadDisplayConfig().ValidateStorage() != nil {
		return tr("请先完成存储设置"), tr("打开 Picup 完成设置"), false
	}
	return "", "", false
}

// The quick panel owns no preferences or toast queue. Its result survives
// dismissal and main-window renders, and its jobs refer only to its last batch.
func (a *application) quickUploadView(c *ui.Context) {
	c.SetTheme(palette(c))
	// Keep the flyout's corners transparent outside the rounded panel below.
	c.Root().Background(ui.Transparent)
	a.onShortcut(c, "choose", func() { a.chooseFiles(true) })
	a.onShortcut(c, "paste", func() { a.pasteImage(true) })
	a.onShortcut(c, "quick-upload", func() { a.chooseFiles(true) })
	a.onShortcut(c, "quick-paste", func() { a.pasteImage(true) })
	a.onShortcut(c, "settings", func() { a.openSettings(0); a.showWindow() })
	a.onShortcut(c, "quit", mygo.App.Quit)
	t := c.Theme()
	root := ui.Column(c).Fill().Radius(12).Clip().Background(t.Background)
	if runtime.GOOS != "darwin" {
		root.Border(1, t.Border)
	}
	root.Children(func() {
		ui.Row(c).FillWidth().Height(48).Shrink(0).Padding(0, 16).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Image(c, brandIcon).Size(20, 20).Shrink(0)
			components.Text(c, "Picup").FontSize(13).FontWeight(650)
			components.Text(c, tr("快捷上传")).SingleLine().Ellipsis("…").MinWidth(0).FontSize(11).TextColor(t.TextMuted)
			ui.Spacer(c)
			ui.Row(c).Gap(4).Shrink(0).Children(func() {
				a.quickHeaderAction(c, "重置状态", lucide.RotateCcw(), !a.canResetQuickUpload(), a.resetQuickUpload)
				a.quickHeaderAction(c, "打开 Picup", lucide.AppWindow(), false, a.openFromQuickUpload)
				a.quickHeaderAction(c, "设置", lucide.Settings(), false, a.openQuickUploadSettings)
			})
		})
		ui.Column(c).FillWidth().Height(220).Shrink(0).Padding(0, 16, 16, 16).Gap(8).Children(func() {
			cfg := a.uploadDisplayConfig()
			unconfigured := cfg.ValidateStorage() != nil
			disabled := a.busy || unconfigured
			ui.Row(c).FillWidth().Height(24).Shrink(0).Gap(7).AlignItems(ui.Center).Children(func() {
				components.Icon(c, lucide.Cloud()).Size(14, 14).Shrink(0).TextColor(t.TextMuted)
				name := profileName(cfg.ActiveProfile, cfg.CurrentProfileName())
				components.Text(c, name).MinWidth(0).MaxWidth(141).Shrink(1).SingleLine().Ellipsis("…").FontSize(10.5).TextColor(t.TextMuted).Tooltip(name)
				components.Text(c, "/").FontSize(10).TextColor(t.TextMuted.Alpha(.5))
				destination := tr("尚未配置存储")
				if cfg.Bucket != "" {
					destination = strings.ToUpper(cfg.Provider) + " · " + cfg.Bucket
				}
				components.Text(c, destination).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").FontSize(10.5).TextColor(t.TextMuted).Tooltip(destination)
			})
			a.quickUploadCard(c, unconfigured)
			paste := ui.ButtonBase(c).Label(tr("上传剪贴板图片")).FillWidth().Height(36).Shrink(0).Radius(8).Border(1, t.Border).Padding(0, 11).Background(ui.Color{}).Disabled(disabled).AlignItems(ui.Center).Gap(8)
			if !disabled && (paste.Hovered() || paste.FocusVisible()) {
				paste.Background(t.SurfaceHover)
			}
			paste.Children(func() {
				components.Icon(c, lucide.Clipboard()).Size(14, 14).Shrink(0).TextColor(t.Accent)
				components.Text(c, tr("上传剪贴板图片")).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").FontSize(11)
				a.quickShortcutLabel(c, "quick-paste", t.TextMuted)
			})
			if paste.Clicked() {
				a.pasteImage(true)
			}
		})
	})
}

func (a *application) quickUploadCard(c *ui.Context, unconfigured bool) {
	t := c.Theme()
	message, detail, failed := a.quickUploadStatus()
	hasStatus := message != "" || detail != ""
	zone := ui.Box(c).Key("quick-drop").FillWidth().Height(128).Shrink(0).Radius(10).Border(1, t.Border).Background(t.Surface)
	if !hasStatus && !unconfigured {
		zone.BorderColor(t.Border.Mix(t.TextMuted, .25)).BorderStyle(ui.BorderDashed)
	}
	if !a.busy && (hasStatus || unconfigured) {
		// Buttons are accessibility leaves in MyGo. Keep the live status on
		// their parent so assistive technology can read the result and action.
		zone.Role(ui.RoleStatus)
	}
	// A busy card is a status, rather than a disabled button that still invites
	// file selection. Configuring storage remains an explicit available action.
	dragging := !a.busy && !unconfigured && zone.FileDragOver()
	if paths := zone.DroppedFiles(); len(paths) > 0 {
		a.quickUploadFiles(paths)
	}
	zone.Children(func() {
		if a.busy {
			ui.Column(c).Role(ui.RoleStatus).Fill().Padding(13, 16).AlignItems(ui.Center).Justify(ui.Center).Gap(9).Children(func() {
				components.Spinner(c, components.SpinnerRing).Size(24, 24).Shrink(0).TextColor(t.Accent)
				components.Text(c, message).FillWidth().TextAlign(ui.Center).MaxLines(2).FontSize(13).FontWeight(600).FixedLineHeight(18)
				components.Text(c, detail).FillWidth().TextAlign(ui.Center).MaxLines(2).FontSize(10.5).FixedLineHeight(15).TextColor(t.TextMuted)
			})
			return
		}
		label, run := tr("选择图片…"), func() { a.chooseFiles(true) }
		if unconfigured {
			label, run = tr("打开存储设置"), a.openQuickUploadSettings
		} else if failed {
			label, run = tr("打开 Picup 查看详情并重试"), a.openQuickUploadResults
		}
		button := ui.ButtonBase(c).Key("quick-card-action").Label(label).Fill().Radius(10).Background(ui.Color{})
		if button.Hovered() || button.FocusVisible() || dragging {
			zone.BorderColor(t.Accent).Background(t.Surface.Mix(t.Accent, .035))
		}
		if dragging {
			zone.Border(1.5, t.Accent).BorderStyle(ui.BorderSolid).Background(t.Surface.Mix(t.Accent, .08))
		}
		button.Children(func() {
			content := ui.Column(c).Fill().Padding(13, 16).AlignItems(ui.Center).Justify(ui.Center).Gap(7)
			if hasStatus || unconfigured {
				content.Gap(5)
			}
			content.Children(func() {
				switch {
				case dragging:
					components.Icon(c, lucide.ImageUp()).Size(24, 24).Shrink(0).TextColor(t.Accent)
					a.quickCardTitle(c, tr("松开即可上传图片"), 13)
					name := profileName(a.uploadDisplayConfig().ActiveProfile, a.uploadDisplayConfig().CurrentProfileName())
					components.Text(c, tf("上传至 %s", name)).FontSize(11).TextColor(t.Accent).SingleLine().MaxWidth(294).Ellipsis("…")
				case unconfigured:
					components.Icon(c, lucide.Cloud()).Size(24, 24).Shrink(0).TextColor(t.TextMuted)
					a.quickCardTitle(c, tr("请先完成存储设置"), 13)
					a.quickCardDetail(c, tr("连接 S3 或 R2 存储后即可上传"), 1)
					a.quickSelectLabel(c, label, "", true, 25)
				case failed:
					content.Padding(10, 16)
					components.Icon(c, lucide.CircleX()).Size(19, 19).Shrink(0).TextColor(t.Danger)
					title, reason := a.quickUploadFailure()
					a.quickCardTitle(c, title, 12)
					a.quickCardDetail(c, reason, 2)
					a.quickSelectLabel(c, label, "", true, 22)
				case hasStatus:
					icon, color := lucide.Info(), t.TextMuted
					for _, j := range a.quickUpload.jobs {
						if j.result != nil {
							icon, color = lucide.CircleCheck(), t.Success
							break
						}
					}
					components.Icon(c, icon).Size(21, 21).Shrink(0).TextColor(color)
					a.quickCardTitle(c, message, 13)
					a.quickCardDetail(c, detail, 1)
					a.quickSelectLabel(c, tr("选择图片…"), "quick-upload", true, 25)
				default:
					components.Icon(c, lucide.ImageUp()).Size(24, 24).Shrink(0).TextColor(t.Accent)
					a.quickCardTitle(c, tr("拖入图片，即刻上传"), 13)
					a.quickSelectLabel(c, tr("选择图片…"), "quick-upload", false, 27)
				}
			})
		})
		if button.Clicked() {
			run()
		}
	})
}

func (a *application) quickCardTitle(c *ui.Context, text string, size float32) {
	components.Text(c, text).FillWidth().MinWidth(0).SingleLine().Ellipsis("…").TextAlign(ui.Center).FontSize(size).FontWeight(600).FixedLineHeight(18).Tooltip(text)
}

func (a *application) quickCardDetail(c *ui.Context, text string, lines int) {
	components.Text(c, text).FillWidth().MinWidth(0).Wrap().MaxLines(lines).TextAlign(ui.Center).FontSize(10.5).FixedLineHeight(15).TextColor(c.Theme().TextMuted).Tooltip(text)
}

func (a *application) quickSelectLabel(c *ui.Context, label, shortcut string, subtle bool, height float32) {
	t := c.Theme()
	background, color := t.Accent, t.AccentText
	if subtle {
		background, color = t.Surface.Mix(t.Accent, .09), t.Accent
	}
	ui.Row(c).Height(height).Shrink(0).Padding(2, 10).Radius(6).Background(background).Gap(14).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		components.Text(c, label).FontSize(11).FixedLineHeight(17).TextColor(color).SingleLine()
		if shortcut != "" {
			a.quickShortcutLabel(c, shortcut, color.Alpha(.85))
		}
	})
}

// Keep the summary separate from a potentially long provider error, so the
// recovery action stays visible. The full reason remains available as a tooltip.
func (a *application) quickUploadFailure() (title, reason string) {
	uploaded, failed := 0, 0
	for _, j := range a.quickUpload.jobs {
		if j.result != nil {
			uploaded++
		} else if j.status == "上传失败" {
			failed++
			if reason == "" {
				reason = j.detail
			}
		}
	}
	title = tr("上传失败")
	if failed > 0 {
		title = tf("已上传 %d 张，%d 张失败，可重试", uploaded, failed)
	}
	if reason == "" {
		reason = a.quickUpload.message
	}
	return title, reason
}

func (a *application) openFromQuickUpload() {
	if a.uploadDisplayConfig().ValidateStorage() != nil {
		a.openSettings(0)
	}
	a.showWindow()
}

func (a *application) openQuickUploadSettings() {
	a.openSettings(0)
	a.showWindow()
}

func (a *application) openQuickUploadResults() {
	a.navigate(0)
	a.showWindow()
}

func (a *application) quickShortcutLabel(c *ui.Context, id string, color ui.Color) {
	if binding := a.shortcutBinding(id); binding.key != ui.KeyUnknown {
		components.Text(c, binding.label()).FontSize(10).TextColor(color).SingleLine().Shrink(0)
	}
}

func (a *application) quickHeaderAction(c *ui.Context, label string, icon *ui.SVG, disabled bool, run func()) {
	t := c.Theme()
	button := ui.ButtonBase(c).Label(tr(label)).Tooltip(tr(label)).Disabled(disabled).Size(28, 28).Shrink(0).Radius(6).AlignItems(ui.Center).Justify(ui.Center).Background(ui.Color{})
	if !disabled && (button.Hovered() || button.FocusVisible()) {
		button.Background(t.SurfaceHover)
	}
	button.Children(func() {
		components.Icon(c, icon).Size(15, 15).TextColor(t.TextMuted)
	})
	if button.Clicked() {
		run()
	}
}
