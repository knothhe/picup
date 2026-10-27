package gui

import (
	"fmt"
	"path/filepath"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/service"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

func (a *application) imageToolsView(c *ui.Context) {
	s := &a.imageTools
	s.init()
	var sources []*job
	for _, j := range s.jobs {
		sources = append(sources, j.source)
	}
	a.loadUploadPreviews(sources)
	// Keep the empty drop target in a bounded column so it takes the available
	// room. Editors, errors and queued files can scroll at smaller window sizes.
	var body ui.Element
	if len(s.jobs) > 0 || s.processing.expanded || s.preferenceErr != nil || s.preferenceLoadErr != nil {
		body = scrollView(c).Key("image-tools-scroll").FillWidth().Grow(1).MinHeight(0)
	} else {
		body = ui.Column(c).Key("image-tools-empty").FillWidth().Grow(1).MinHeight(0)
	}
	body.Children(func() {
		ui.Column(c).FillWidth().Grow(1).MinWidth(0).Padding(20, 24).Gap(14).Children(func() {
			a.processingEditorFocused = a.processingDisclosure(c, "local-processing", &s.processing, processingSummaryParts(s.compress, s.format, int(s.quality), int(s.maxWidth)), &s.compress, &s.format, &s.quality, &s.maxWidth, s.running)
			if s.preferenceLoadErr != nil {
				ui.Row(c).FillWidth().MinWidth(0).Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
					components.Text(c, tr("读取图片工具参数失败，原记录已保留")).Grow(1).MinWidth(0).Wrap().FontSize(12).TextColor(c.Theme().Danger)
					if s.hasUnsavedPreferences() && components.Button(c, tr("放弃未保存参数"), components.ButtonGhost).Clicked() {
						a.discardImageToolsPreferences()
					}
					if components.Button(c, tr("重置参数记录"), components.ButtonOutline).Tooltip(tr("备份原记录后，保存当前参数。")).Clicked() {
						a.resetImageToolsPreferenceRecord()
					}
				})
			}
			if s.preferenceErr != nil {
				ui.Row(c).FillWidth().MinWidth(0).Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
					components.Text(c, tr("保存图片工具参数失败，输入已保留")).Grow(1).MinWidth(0).Wrap().FontSize(12).TextColor(c.Theme().Danger)
					if s.hasUnsavedPreferences() && components.Button(c, tr("放弃未保存参数"), components.ButtonGhost).Clicked() {
						a.discardImageToolsPreferences()
					}
					if components.Button(c, tr("重试保存"), components.ButtonOutline).Clicked() {
						a.flushImageToolsPreferences()
					}
				})
			}
			a.imageToolsDropZone(c, len(s.jobs) > 0)
			if len(s.jobs) > 0 {
				ui.Row(c).FillWidth().MinWidth(0).Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
					components.Text(c, tf("%d 张图片", len(s.jobs))).FontSize(13).FontWeight(600)
					if s.running {
						components.Text(c, tf("已处理 %d / %d", s.done, s.total)).FontSize(12).TextColor(c.Theme().TextMuted)
					}
					ui.Spacer(c)
					if len(s.jobs) > a.imageToolPending() && components.Button(c, tr("重新导出全部"), components.ButtonGhost).Disabled(a.busy).Clicked() {
						a.exportImageTools(true)
					}
					if components.Button(c, tr("清空"), components.ButtonGhost).Disabled(a.busy).Clicked() {
						s.jobs = nil
					}
				})
				ui.Column(c).FillWidth().MinWidth(0).Radius(10).Border(1, c.Theme().Border).Clip().Children(func() {
					for i, j := range append([]*imageToolJob(nil), s.jobs...) {
						a.imageToolRow(c, j, i == 0)
					}
				})
			}
		})
	})
	a.imageToolsActionBar(c)
	a.queueImageToolsPreferences()
}

func (a *application) imageToolsDropZone(c *ui.Context, compact bool) {
	t := c.Theme()
	zone := ui.Column(c).Key("local-drop-zone").FillWidth().MinWidth(0).Padding(16).Gap(10).AlignItems(ui.Center).Justify(ui.Center).Radius(12).Border(1.5, controlBorder(t)).BorderStyle(ui.BorderDashed).Background(sunkenColor(t)).Disabled(a.busy)
	if compact {
		zone.MinHeight(68).Padding(10, 12)
	} else {
		zone.Grow(1).MinHeight(230)
	}
	if zone.FileDragOver() {
		zone.BorderColor(t.Accent).Background(accentSoft(t))
	}
	if paths := zone.DroppedFiles(); len(paths) > 0 {
		a.addImageToolFiles(paths)
	}
	zone.Children(func() {
		if compact {
			ui.Row(c).FillWidth().MinWidth(0).Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
				components.Icon(c, lucide.ImagePlus()).Size(18, 18).Shrink(0).TextColor(t.TextMuted)
				components.Text(c, tr("拖入图片，或")).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(0)
				if components.Button(c, tr("选择图片"), components.ButtonGhost).TextColor(t.Accent).Disabled(a.busy).Clicked() {
					a.chooseImageToolFiles()
				}
				ui.Spacer(c)
				if components.Button(c, tr("粘贴图片"), components.ButtonOutline).Disabled(a.busy).Clicked() {
					a.pasteImageTool()
				}
			})
			return
		}
		iconTile(c, lucide.Sparkles(), 56)
		components.Text(c, tr("拖入图片开始处理")).FontSize(18).FontWeight(600).Wrap()
		ui.Row(c).FillWidth().MinWidth(0).Wrap().AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
			if components.Button(c, tr("选择图片"), components.ButtonPrimary).Clicked() {
				a.chooseImageToolFiles()
			}
			if components.Button(c, tr("粘贴图片"), components.ButtonOutline).Clicked() {
				a.pasteImageTool()
			}
		})
		components.Text(c, "PNG · JPEG · WebP · GIF · HEIC / AVIF · TIFF").FontSize(11).TextColor(t.TextMuted).Wrap()
	})
}

func (a *application) imageToolRow(c *ui.Context, j *imageToolJob, first bool) {
	t, source := c.Theme(), j.source
	row := ui.Row(c).Key(fmt.Sprintf("local-image-%d", source.id)).FillWidth().MinWidth(0).MinHeight(66).Padding(10, 12).Gap(10).AlignItems(ui.Center)
	if !first {
		row.BorderWidth(1, 0, 0, 0).BorderColor(t.Border)
	}
	row.Children(func() {
		ui.Box(c).Size(40, 40).Shrink(0).Radius(7).Background(sunkenColor(t)).Clip().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			if source.preview != nil {
				components.Image(c, source.preview).Fill().Fit(ui.Cover).Label(source.name)
			} else {
				components.Icon(c, lucide.Image()).Size(18, 18).TextColor(t.TextMuted)
			}
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
			components.Text(c, source.name).FontSize(13).FontWeight(500).SingleLine().Ellipsis("…")
			if j.result != nil {
				r := j.result
				components.Text(c, uploadSizeSummary(&service.Result{OriginalBytes: r.OriginalBytes, Bytes: r.Bytes, Width: r.Width, Height: r.Height})).FontSize(11.5).TextColor(t.TextMuted).Wrap()
				components.Text(c, r.Name).FontSize(11).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Tooltip(r.Path)
			} else if source.sourceBytes > 0 {
				components.Text(c, bytesLabel(source.sourceBytes)).FontSize(11.5).TextColor(t.TextMuted)
			}
			if j.err != nil {
				components.Text(c, j.err.Error()).FontSize(12).TextColor(t.Danger).FillWidth().Wrap().MaxLines(3).Tooltip(j.err.Error())
			}
		})
		switch {
		case j.working:
			statusPill(c, tr("正在处理"), toneAccent)
		case j.result != nil:
			statusPill(c, tr("已导出"), toneSuccess)
		case j.err != nil:
			statusPill(c, tr("处理失败"), toneDanger)
		default:
			statusPill(c, tr("待处理"), toneNeutral)
		}
		if j.result != nil && components.IconButton(c, lucide.FolderOpen(), tr("打开导出目录"), components.IconButtonPlain).Clicked() {
			a.openImageToolOutput(filepath.Dir(j.result.Path))
		}
		if components.IconButton(c, lucide.X(), tf("移除 %s", source.name), components.IconButtonPlain).Disabled(a.busy).Clicked() {
			a.removeImageTool(j)
		}
	})
}

func (a *application) imageToolsActionBar(c *ui.Context) {
	s, t := &a.imageTools, c.Theme()
	width, _ := c.Size()
	ui.Column(c).Key("local-export-actions").FillWidth().Shrink(0).Padding(10, 16, 10, 24).Gap(8).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		folder := func() {
			components.Icon(c, lucide.FolderOpen()).Size(16, 16).Shrink(0).TextColor(t.TextMuted)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
				label := s.outputDir
				if label == "" {
					label = tr("选择导出目录")
				}
				components.Text(c, label).FontSize(12).SingleLine().Ellipsis("…").Tooltip(s.outputDir)
				components.Text(c, tr("保留原图，重名文件自动改名。")).FontSize(11).TextColor(t.TextMuted).Wrap()
			})
			label := "更改目录"
			if s.outputDir == "" {
				label = "选择导出目录"
			}
			if components.Button(c, tr(label), components.ButtonGhost).Shrink(0).Disabled(a.busy).Clicked() {
				a.chooseImageToolOutput()
			}
		}
		actions := func() {
			if s.running {
				if components.Button(c, tr("取消处理"), components.ButtonOutline).Clicked() && s.cancel != nil {
					s.cancel()
				}
			} else if pending := a.imageToolPending(); pending > 0 {
				if components.Button(c, tf("处理并导出 %d 张", pending), components.ButtonPrimary).Disabled(a.busy || s.outputDir == "").Clicked() {
					a.exportImageTools(false)
				}
			}
		}
		if width < 800 {
			ui.Row(c).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(folder)
			if s.running || a.imageToolPending() > 0 {
				ui.Row(c).FillWidth().Justify(ui.End).Children(actions)
			}
		} else {
			ui.Row(c).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() { folder(); actions() })
		}
	})
}
