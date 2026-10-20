package gui

import (
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
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

const quickUploadWidth, quickUploadHeight = 360, 312

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
		w.OnShow(func() { a.quickUpload.visible = true; a.updateLightweightTimer(time.Now()) })
		w.OnHide(func() {
			a.quickUpload.visible = false
			a.updateMenus()
			a.updateLightweightTimer(time.Now())
		})
		w.OnClosed(func() {
			if a.quickUpload.window == w {
				a.quickUpload.window = nil
				a.quickUpload.visible = false
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
		ui.Row(c).FillWidth().Height(46).Shrink(0).Padding(0, 12).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Image(c, brandIcon).Size(19, 19)
			components.Text(c, "Picup").FontSize(13).Bold()
			components.Text(c, tr("快捷上传")).SingleLine().Ellipsis("…").MinWidth(0).FontSize(11).TextColor(t.TextMuted)
			ui.Spacer(c)
			a.quickHeaderAction(c, "打开 Picup", lucide.AppWindow(), a.openFromQuickUpload)
			a.quickHeaderAction(c, "设置", lucide.Settings(), func() { a.openSettings(0); a.showWindow() })
		})
		ui.Column(c).FillWidth().Height(266).Shrink(0).Padding(12).Gap(8).Children(func() {
			cfg := a.uploadDisplayConfig()
			disabled := a.busy || cfg.ValidateStorage() != nil
			ui.Row(c).FillWidth().Height(22).Shrink(0).Gap(6).AlignItems(ui.Center).Children(func() {
				components.Icon(c, lucide.Cloud()).Size(13, 13).Shrink(0).TextColor(t.TextMuted)
				name := profileName(cfg.ActiveProfile, cfg.CurrentProfileName())
				components.Text(c, name).MinWidth(0).MaxWidth(142).Shrink(1).SingleLine().Ellipsis("…").FontSize(10.5).TextColor(t.TextMuted).Tooltip(name)
				components.Text(c, "/").FontSize(10).TextColor(t.TextMuted)
				destination := tr("尚未配置存储")
				if cfg.Bucket != "" {
					destination = strings.ToUpper(cfg.Provider) + " · " + cfg.Bucket
				}
				components.Text(c, destination).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").FontSize(10.5).TextColor(t.TextMuted).Tooltip(destination)
			})
			zone := ui.Box(c).Key("quick-drop").FillWidth().Height(96).Shrink(0).Radius(10).Border(1, t.Border.Mix(t.TextMuted, .25)).BorderStyle(ui.BorderDashed).Background(t.Surface).Disabled(disabled)
			dragging := zone.FileDragOver()
			if paths := zone.DroppedFiles(); len(paths) > 0 {
				a.quickUploadFiles(paths)
			}
			zone.Children(func() {
				button := ui.ButtonBase(c).Label(tr("选择图片…")).Fill().Radius(10).Background(ui.Color{})
				if button.Hovered() || button.FocusVisible() || dragging {
					zone.BorderColor(t.Accent).Background(t.Surface.Mix(t.Accent, .035))
				}
				button.Children(func() {
					ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(5).Children(func() {
						components.Icon(c, lucide.ImageUp()).Size(22, 22).TextColor(t.Accent)
						label := tr("拖入图片，即刻上传")
						if dragging {
							label = tr("松开即可上传图片")
						}
						components.Text(c, label).FontSize(13).Bold()
						ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
							components.Text(c, tr("选择图片…")).FontSize(11).TextColor(t.Accent).Background(t.Background).Border(1, t.Border).Radius(6).Padding(3, 9)
							a.quickShortcutLabel(c, "quick-upload")
						})
					})
				})
				if button.Clicked() {
					a.chooseFiles(true)
				}
			})
			paste := ui.ButtonBase(c).Label(tr("上传剪贴板图片")).FillWidth().Height(30).Shrink(0).Radius(6).Background(ui.Color{}).Disabled(disabled).AlignItems(ui.Center).Justify(ui.Center).Gap(7)
			if paste.Hovered() || paste.FocusVisible() {
				paste.Background(t.SurfaceHover)
			}
			paste.Children(func() {
				components.Icon(c, lucide.Clipboard()).Size(13, 13).TextColor(t.Accent)
				components.Text(c, tr("上传剪贴板图片")).FontSize(11).TextColor(t.Accent)
				a.quickShortcutLabel(c, "quick-paste")
			})
			if paste.Clicked() {
				a.pasteImage(true)
			}
			a.quickUploadStatusView(c)
		})
	})
}

func (a *application) openFromQuickUpload() {
	if a.uploadDisplayConfig().ValidateStorage() != nil {
		a.openSettings(0)
	}
	a.showWindow()
}

func (a *application) quickShortcutLabel(c *ui.Context, id string) {
	if binding := a.shortcutBinding(id); binding.key != ui.KeyUnknown {
		components.Text(c, binding.label()).FontSize(10).TextColor(c.Theme().TextMuted).SingleLine()
	}
}

func (a *application) quickHeaderAction(c *ui.Context, label string, icon *ui.SVG, run func()) {
	t := c.Theme()
	button := ui.ButtonBase(c).Label(tr(label)).Tooltip(tr(label)).Size(28, 28).Shrink(0).Radius(6).AlignItems(ui.Center).Justify(ui.Center).Background(ui.Color{})
	if button.Hovered() || button.FocusVisible() {
		button.Background(t.SurfaceHover)
	}
	button.Children(func() {
		components.Icon(c, icon).Size(14, 14).TextColor(t.TextMuted)
	})
	if button.Clicked() {
		run()
	}
}

func (a *application) quickUploadStatusView(c *ui.Context) {
	message, detail, failed := a.quickUploadStatus()
	if message == "" && detail == "" {
		return
	}
	t := c.Theme()
	color, icon := t.TextMuted, lucide.Info()
	if failed {
		color, icon = t.Danger, lucide.CircleX()
	} else if !a.busy {
		for _, j := range a.quickUpload.jobs {
			if j.result != nil {
				color, icon = t.Success, lucide.CircleCheck()
				break
			}
		}
	}
	ui.Row(c).Role(ui.RoleStatus).FillWidth().Height(70).Shrink(0).Padding(7, 2).Gap(8).AlignItems(ui.Center).Children(func() {
		if a.busy {
			components.Spinner(c, components.SpinnerRing).Size(14, 14).Shrink(0).TextColor(t.Accent)
		} else {
			components.Icon(c, icon).Size(14, 14).Shrink(0).TextColor(color)
		}
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Gap(4).Justify(ui.Center).Children(func() {
			scrollView(c).Key("quick-status").FillWidth().MinHeight(0).MaxHeight(36).Children(func() {
				components.Text(c, message).FillWidth().MinWidth(0).Wrap().FontSize(11).TextColor(color).Tooltip(message)
			})
			if detail != "" {
				components.Text(c, detail).FillWidth().MinWidth(0).Wrap().FontSize(10).TextColor(t.TextMuted)
			}
		})
	})
}
