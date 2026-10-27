package gui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/service"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

func bytesLabel(n int) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	if n >= 1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%d B", n)
}

// uploadSizeParts separates the measured sizes from the size change, so the
// change can carry its own tone.
func uploadSizeParts(r *service.Result) (sizes, change string, saved bool) {
	sizes = fmt.Sprintf("%s → %s · %d × %d", bytesLabel(r.OriginalBytes), bytesLabel(r.Bytes), r.Width, r.Height)
	if r.OriginalBytes <= 0 {
		return sizes, "", false
	}
	if r.Bytes == r.OriginalBytes {
		return sizes, tr("体积未减少"), false
	}
	delta := 100 * float64(r.OriginalBytes-r.Bytes) / float64(r.OriginalBytes)
	if delta > 0 {
		return sizes, tf("节省 %s", fmt.Sprintf("%.1f%%", delta)), true
	}
	return sizes, tf("体积增加 %s", fmt.Sprintf("%.1f%%", -delta)), false
}

func uploadSizeSummary(r *service.Result) string {
	sizes, change, _ := uploadSizeParts(r)
	if change == "" {
		return sizes
	}
	return sizes + " · " + change
}

func storageDestination(cfg config.Config) string {
	if cfg.Bucket == "" {
		return tr("尚未配置存储")
	}
	return strings.ToUpper(cfg.Provider) + " · " + cfg.Bucket
}

type statusTone int

const (
	toneNeutral statusTone = iota
	toneAccent
	toneSuccess
	toneDanger
	toneWarning
)

// statusPill is the shared capsule for per-item states.
func statusPill(c *ui.Context, text string, tone statusTone) {
	t := c.Theme()
	background, color := sunkenColor(t), t.TextMuted
	switch tone {
	case toneAccent:
		background, color = accentSoft(t), t.Accent
	case toneSuccess:
		background, color = successSoft(t), t.Success
	case toneDanger:
		background, color = dangerSoft(t), t.Danger
	case toneWarning:
		background, color = t.Warning.Alpha(.12), t.Warning
	}
	ui.Row(c).Height(22).Shrink(0).Padding(0, 8).Gap(4).Radius(11).Background(background).AlignItems(ui.Center).Children(func() {
		switch tone {
		case toneAccent:
			components.Spinner(c, components.SpinnerRing).Size(11, 11).TextColor(color)
		case toneSuccess:
			components.Icon(c, lucide.Check()).Size(12, 12).TextColor(color)
		}
		components.Text(c, text).FontSize(11.5).TextColor(color).SingleLine()
	})
}

// iconTile frames a page-level symbol on a soft accent square.
func iconTile(c *ui.Context, icon *ui.SVG, size float32) {
	t := c.Theme()
	ui.Box(c).Size(size, size).Shrink(0).Radius(size / 4).Background(accentSoft(t)).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		components.Icon(c, icon).Size(size/2, size/2).TextColor(t.Accent)
	})
}

func (a *application) shortcutHint(c *ui.Context, id string, color ui.Color) {
	if binding := a.shortcutBinding(id); binding.key != ui.KeyUnknown {
		components.Text(c, binding.label()).FontSize(11).TextColor(color).SingleLine().Shrink(0)
	}
}

func (a *application) uploadHeaderContext(c *ui.Context) {
	headerChip(c, lucide.Cloud(), storageDestination(a.uploadDisplayConfig()))
}

func (a *application) uploadHeaderActions(c *ui.Context) {
	if components.Button(c, tr("存储设置"), components.ButtonGhost).TextColor(c.Theme().TextMuted).Clicked() {
		a.openSettings(0)
	}
}

func (a *application) uploadView(c *ui.Context) {
	a.loadUploadPreviews(a.jobs)
	counts := a.uploadCounts()
	config := a.uploadDisplayConfig()
	unconfigured := config.ValidateStorage() != nil
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Padding(20, 24).Gap(14).Children(func() {
		if a.uploadQueueBlocked() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				components.Text(c, tr("上传队列记录无法读取，原记录已保留。重置后才能添加图片。")).Grow(1).MinWidth(0).Wrap().FontSize(12)
				if components.Button(c, tr("重置上传队列记录"), components.ButtonOutline).Disabled(a.busy).Clicked() {
					a.clearUploadQueue()
				}
			})
		}
		if unconfigured {
			_, height := c.Size()
			a.uploadSetup(c, counts.total > 0 || height < 720)
		}
		saved := a.config
		if a.savedConfig != nil {
			saved = *a.savedConfig
		}
		a.processingEditorFocused = a.processingDisclosure(c, processingEditor{
			key: "upload-processing", state: &a.uploadProcessing,
			compress: &a.config.Compress, format: &a.config.Format, quality: &a.quality, maxWidth: &a.maxWidth,
			shown:  processingValues{config.Compress, config.Format, config.Quality, config.MaxWidth},
			saved:  processingValues{saved.Compress, saved.Format, saved.Quality, saved.MaxWidth},
			locked: a.uploading,
			commit: func() bool { a.save(); return !a.settingsSave.failed },
		})
		if a.settingsSave.failed {
			ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(8).Children(func() {
				components.Text(c, tr("保存失败，输入已保留")).Grow(1).MinWidth(0).Wrap().FontSize(12).TextColor(c.Theme().Danger)
				if components.Button(c, tr("重试保存"), components.ButtonGhost).Clicked() {
					a.save()
				}
			})
		}
		a.uploadDropZone(c, counts.total > 0)
		if counts.total > 0 {
			a.uploadQueueToolbar(c, counts)
			jobs := append([]*job(nil), a.jobs...)
			scrollView(c).Key("upload-files").FillWidth().Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).FillWidth().Radius(10).Border(1, c.Theme().Border).Clip().Children(func() {
					for i, j := range jobs {
						a.jobView(c, j, i == 0)
					}
				})
			})
		}
	})
	a.uploadActionBar(c, counts, config)
}

// uploadSetup walks a new profile through connecting storage. Once files are
// queued it shrinks to a single line so the queue keeps its room.
func (a *application) uploadSetup(c *ui.Context, compact bool) {
	t := c.Theme()
	if compact {
		ui.Row(c).FillWidth().Shrink(0).Padding(8, 12).Radius(8).Border(1, t.Border).Background(sunkenColor(t)).AlignItems(ui.Center).Gap(8).Children(func() {
			components.Icon(c, lucide.Info()).Size(14, 14).TextColor(t.TextMuted)
			components.Text(c, tr("连接存储后，即可上传图片。")).Grow(1).MinWidth(0).Wrap().FontSize(12).TextColor(t.TextMuted)
			if components.Button(c, tr("配置存储"), components.ButtonGhost).Shrink(0).TextColor(t.Accent).Clicked() {
				a.openSettings(0)
			}
		})
		return
	}
	ui.Column(c).Key("upload-setup").FillWidth().Shrink(0).Padding(16, 18).Radius(12).Border(1, t.Border).Background(t.Surface).Gap(4).Children(func() {
		components.Text(c, tr("连接存储，开始上传")).FontSize(15).FontWeight(600)
		steps := []string{"填写存储连接", "测试上传与清理", "上传第一张图片"}
		for i, step := range steps {
			ui.Row(c).Key(i).FillWidth().Padding(10, 0, 0, 0).Gap(12).AlignItems(ui.Center).Children(func() {
				number := ui.Box(c).Size(22, 22).Shrink(0).Radius(11).AlignItems(ui.Center).Justify(ui.Center)
				color := t.TextMuted
				if i == 0 {
					number.Background(t.Accent)
					color = t.AccentText
				} else {
					number.Border(1.5, controlBorder(t))
				}
				number.Children(func() { components.Text(c, fmt.Sprint(i+1)).FontSize(11).FontWeight(600).TextColor(color) })
				components.Text(c, tr(step)).Grow(1).MinWidth(0).FontSize(13).FontWeight(500)
				if i == 0 && components.Button(c, tr("配置存储"), components.ButtonPrimary).Shrink(0).Clicked() {
					a.openSettings(0)
				}
			})
		}
	})
}

// The drop zone is a slim bar beside a queue and the page's centre when empty.
// Files dropped anywhere in the window are accepted as well.
func (a *application) uploadDropZone(c *ui.Context, compact bool) {
	t := c.Theme()
	zone := ui.Box(c).Key("drop-zone").FillWidth().Radius(12).Border(1.5, controlBorder(t)).BorderStyle(ui.BorderDashed).Background(sunkenColor(t)).Disabled(a.busy || a.uploadQueueBlocked())
	if compact {
		zone.Shrink(0).Height(68).Radius(10)
	} else {
		zone.Grow(1).MinHeight(120)
	}
	dragging := zone.FileDragOver()
	if paths := zone.DroppedFiles(); len(paths) > 0 {
		a.addFiles(paths)
	}
	if dragging {
		zone.BorderColor(t.Accent).BorderStyle(ui.BorderSolid).Background(accentSoft(t))
	}
	zone.Children(func() {
		if compact {
			ui.Row(c).Fill().Padding(0, 12, 0, 14).Gap(12).AlignItems(ui.Center).Children(func() {
				pick := ui.ButtonBase(c).Key("choose-files").Label(tr("选择图片")).Grow(1).MinWidth(0).FillHeight().Gap(14).AlignItems(ui.Center).Justify(ui.Start).Background(ui.Color{})
				if pick.Hovered() || pick.FocusVisible() {
					zone.BorderColor(t.Accent)
				}
				pick.Children(func() {
					iconTile(c, lucide.ImageUp(), 40)
					ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() { uploadDropCopy(c, true, dragging) })
				})
				if pick.Clicked() {
					a.choose()
				}
				a.pasteButton(c, components.ButtonOutline)
			})
			return
		}
		ui.Column(c).Fill().Padding(24).AlignItems(ui.Center).Justify(ui.Center).Gap(12).Children(func() {
			iconTile(c, lucide.ImageUp(), 64)
			uploadDropCopy(c, false, dragging)
			ui.Row(c).Gap(8).Padding(4, 0, 0, 0).AlignItems(ui.Center).Children(func() {
				choose := components.Button(c, "", components.ButtonPrimary).Key("choose-files").Label(tr("选择图片")).Gap(8)
				choose.Children(func() {
					components.Text(c, tr("选择图片")).FontSize(12.5).FontWeight(500).TextColor(t.AccentText)
					a.shortcutHint(c, "choose", t.AccentText.Alpha(.75))
				})
				if choose.Clicked() {
					a.choose()
				}
				a.pasteButton(c, components.ButtonOutline)
			})
			components.Text(c, "PNG · JPEG · WebP · GIF · HEIC / AVIF · TIFF").FontSize(11.5).TextColor(t.TextMuted)
		})
	})
}

func (a *application) pasteButton(c *ui.Context, variant components.ButtonVariant) {
	t := c.Theme()
	paste := components.Button(c, "", variant).Shrink(0).Gap(6).Label(tr("从剪贴板粘贴"))
	paste.Children(func() {
		components.Icon(c, lucide.Clipboard()).Size(14, 14).TextColor(t.TextMuted)
		components.Text(c, tr("从剪贴板粘贴")).FontSize(12.5)
		a.shortcutHint(c, "paste", t.TextMuted)
	})
	if paste.Clicked() {
		a.paste()
	}
}

func uploadDropCopy(c *ui.Context, compact, dragging bool) {
	t := c.Theme()
	if !compact {
		title := tr("拖入图片开始上传")
		if dragging {
			title = tr("松开即可添加图片")
		}
		components.Text(c, title).FontSize(18).FontWeight(600)
		return
	}
	ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
		label := tr("拖入图片，或")
		if dragging {
			label = tr("松开即可添加图片")
		}
		components.Text(c, label).FontSize(13).FontWeight(600).SingleLine()
		if !dragging {
			components.Text(c, tr("选择图片")).FontSize(13).FontWeight(600).TextColor(t.Accent).SingleLine()
		}
	})
	components.Text(c, "PNG · JPEG · WebP · GIF · HEIC / AVIF · TIFF").FontSize(11.5).TextColor(t.TextMuted).SingleLine().Ellipsis("…").MinWidth(0)
}

func summaryDot(c *ui.Context, text string, color ui.Color) {
	ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(color)
		components.Text(c, text).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
	})
}

func (a *application) uploadQueueToolbar(c *ui.Context, counts uploadQueueCounts) {
	t := c.Theme()
	ui.Row(c).FillWidth().Shrink(0).Wrap().MinHeight(30).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			components.Text(c, tr("队列")).FontSize(13).FontWeight(600)
			components.Text(c, fmt.Sprint(counts.total)).FontSize(13).TextColor(t.TextMuted)
		})
		ui.Row(c).Padding(0, 0, 0, 6).Gap(12).AlignItems(ui.Center).Children(func() {
			if a.uploading {
				components.Spinner(c, components.SpinnerRing).Size(14, 14)
				components.Text(c, tf("已处理 %d / %d", a.uploadBatchDone, a.uploadBatchTotal)).FontSize(12).TextColor(t.TextMuted)
				return
			}
			if counts.uploaded > 0 {
				summaryDot(c, tf("%d 已上传", counts.uploaded), t.Success)
			}
			if waiting := counts.pending - counts.failed; waiting > 0 {
				summaryDot(c, tf("%d 待上传", waiting), controlBorder(t))
			}
			if counts.failed > 0 {
				summaryDot(c, tf("%d 失败", counts.failed), t.Danger)
			}
			if counts.skipped > 0 {
				summaryDot(c, tf("%d 已跳过", counts.skipped), t.TextMuted)
			}
		})
		ui.Spacer(c)
		if counts.uploaded > 0 {
			if components.Button(c, tr("清除已完成"), components.ButtonGhost).TextColor(t.TextMuted).Disabled(a.busy).Clicked() {
				a.clearUploadedJobs()
			}
			copyAll := components.Button(c, "", components.ButtonGhost).Gap(6).Label(tr("复制全部结果")).TextColor(t.TextMuted)
			copyAll.Children(func() {
				components.Icon(c, lucide.Copy()).Size(14, 14)
				components.Text(c, tr("复制全部结果")).FontSize(12.5)
			})
			if copyAll.Clicked() {
				c.WriteClipboard(service.Format(a.uploadedResults(), a.uploadDisplayConfig().Output))
				a.feedback("已复制全部结果", nil)
			}
		}
		if components.IconButton(c, lucide.ListX(), tr("清空"), components.IconButtonPlain).Disabled(a.busy).Clicked() {
			a.clearUploadQueue()
		}
	})
}

var formatLabels = map[string]string{"original": "保留原格式", "webp": "WebP", "avif": "AVIF", "jpeg": "JPEG", "png": "PNG"}
var outputLabels = map[string]string{"url": "URL", "markdown": "Markdown", "json": "JSON"}

// uploadChip summarizes a setting the next upload will use and opens it.
func uploadChip(c *ui.Context, label string, icon *ui.SVG, text string) bool {
	t := c.Theme()
	chip := ui.ButtonBase(c).Label(label).Tooltip(label).Height(28).MinWidth(0).Shrink(1).Padding(0, 10).Gap(6).Radius(7).Border(1, t.Border).Background(t.Surface).AlignItems(ui.Center)
	if chip.Hovered() || chip.FocusVisible() {
		chip.Background(t.SurfaceHover)
	}
	chip.Children(func() {
		components.Icon(c, icon).Size(14, 14).Shrink(0).TextColor(t.Accent)
		components.Text(c, text).FontSize(12).TextColor(t.TextMuted).SingleLine().Ellipsis("…").MinWidth(0)
	})
	return chip.Clicked()
}

func (a *application) uploadActionBar(c *ui.Context, counts uploadQueueCounts, config config.Config) {
	t := c.Theme()
	ui.Row(c).Key("upload-actions").FillWidth().Height(56).Shrink(0).Padding(0, 16, 0, 24).Gap(8).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		output := outputLabels[config.Output]
		if output == "" {
			output = config.Output
		}
		copied := tf("复制为 %s", output)
		if config.Copy {
			copied = tf("自动复制 %s", output)
		}
		if uploadChip(c, tr("链接与剪贴板"), lucide.Clipboard(), copied) {
			a.openSettings(1)
		}
		ui.Spacer(c)
		if a.uploading {
			if components.Button(c, tr("取消上传"), components.ButtonOutline).Shrink(0).Clicked() && a.cancel != nil {
				a.cancel()
			}
			return
		}
		if counts.pending > 0 {
			if components.Button(c, tf("上传 %d 张图片", counts.pending), components.ButtonPrimary).Shrink(0).Disabled(a.busy).Clicked() {
				a.upload()
			}
		}
	})
}

func (a *application) jobView(c *ui.Context, j *job, first bool) {
	t := c.Theme()
	row := ui.Row(c).Key(fmt.Sprintf("job-%d", j.id)).FillWidth().MinHeight(62).Padding(10, 10, 10, 12).Gap(12).AlignItems(ui.Center)
	if !first {
		row.BorderWidth(1, 0, 0, 0).BorderColor(t.Border)
	}
	failed := j.status == "上传失败" && j.result == nil
	processing := j.status == "正在处理与上传"
	if processing {
		row.Background(accentSoft(t).Alpha(.5))
	}
	row.Children(func() {
		ui.Box(c).Size(40, 40).Shrink(0).Radius(7).Background(sunkenColor(t)).Clip().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			if j.preview != nil {
				components.Image(c, j.preview).Fill().Fit(ui.Cover).Label(j.name)
			} else {
				components.Icon(c, lucide.Image()).Size(18, 18).TextColor(t.TextMuted)
			}
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			components.Text(c, j.name).SingleLine().Ellipsis("…").FontSize(13).FontWeight(500)
			ui.Row(c).FillWidth().MinWidth(0).Wrap().AlignItems(ui.Center).Gap(10).Children(func() {
				if j.result != nil {
					sizes, change, saved := uploadSizeParts(j.result)
					if j.result.Reused {
						sizes = tr("已复用云端同路径图片")
						change = ""
					}
					components.Text(c, sizes).FontSize(12).TextColor(t.TextMuted)
					if change != "" {
						color := t.TextMuted
						if saved {
							color = t.Success
						}
						components.Text(c, change).FontSize(12).FontWeight(500).TextColor(color)
					}
				} else if processing && j.totalBytes > 0 {
					components.Text(c, tf("%s / %s · %d%%", bytesLabel(int(j.sentBytes)), bytesLabel(int(j.totalBytes)), min(100, int(j.sentBytes*100/j.totalBytes)))).FontSize(12).TextColor(t.TextMuted)
				} else if j.sourceBytes > 0 {
					components.Text(c, bytesLabel(j.sourceBytes)).FontSize(12).TextColor(t.TextMuted)
				}
			})
			if failed && j.detail != "" {
				components.Text(c, j.detail).FillWidth().MinWidth(0).Wrap().MaxLines(2).Tooltip(j.detail).FontSize(12).TextColor(t.Danger)
			}
		})
		switch {
		case j.result != nil:
			label := tr("已上传")
			if j.result.Reused {
				label = tr("已复用")
			}
			statusPill(c, label, toneSuccess)
		case failed:
			statusPill(c, tr("上传失败"), toneDanger)
		case j.status == "已跳过":
			statusPill(c, tr("已跳过"), toneNeutral)
		case processing:
			statusPill(c, tr("正在处理与上传"), toneAccent)
		default:
			statusPill(c, tr("待上传"), toneNeutral)
		}
		ui.Row(c).Width(96).Shrink(0).Justify(ui.End).AlignItems(ui.Center).Gap(2).Children(func() {
			if j.result != nil {
				if components.IconButton(c, lucide.Link(), tr("复制链接"), components.IconButtonPlain).Clicked() {
					c.WriteClipboard(j.result.URL)
					a.feedback("已复制链接", nil)
				}
				if components.IconButton(c, lucide.Code(), tr("复制 Markdown"), components.IconButtonPlain).Clicked() {
					c.WriteClipboard(j.result.Markdown())
					a.feedback("已复制 Markdown", nil)
				}
			} else if !processing {
				icon, label := lucide.Upload(), tf("上传 %s", j.name)
				if failed {
					icon, label = lucide.RotateCcw(), tf("重试上传 %s", j.name)
				}
				if components.IconButton(c, icon, label, components.IconButtonPlain).TextColor(t.Accent).Disabled(a.busy).Clicked() {
					a.uploadSingle(j)
				}
			}
			if !processing && components.IconButton(c, lucide.X(), tf("移除 %s", j.name), components.IconButtonPlain).Disabled(a.busy).Clicked() {
				a.removeUploadJob(j)
			}
		})
	})
}
