package gui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/service"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

func heading(c *ui.Context, title, subtitle string) {
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		components.Text(c, title).FontSize(25).Bold().ContentTransition(navigationContentTransition())
		components.Text(c, subtitle).FontSize(12).TextColor(c.Theme().TextMuted).Wrap().ContentTransition(navigationContentTransition())
	})
}
func card(c *ui.Context, key string, build func()) {
	ui.Column(c).Key(key).FillWidth().Background(c.Theme().Surface).Radius(12).Padding(20).Gap(16).Children(build)
}
func bytesLabel(n int) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	if n >= 1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%d B", n)
}

func uploadSizeSummary(r *service.Result) string {
	summary := fmt.Sprintf("%s → %s · %d × %d", bytesLabel(r.OriginalBytes), bytesLabel(r.Bytes), r.Width, r.Height)
	if r.OriginalBytes <= 0 {
		return summary
	}
	if r.Bytes == r.OriginalBytes {
		return summary + " · " + tr("体积未减少")
	}
	change := 100 * float64(r.OriginalBytes-r.Bytes) / float64(r.OriginalBytes)
	if change > 0 {
		return summary + " · " + tf("节省 %s", fmt.Sprintf("%.1f%%", change))
	}
	return summary + " · " + tf("体积增加 %s", fmt.Sprintf("%.1f%%", -change))
}

func (a *application) uploadView(c *ui.Context) {
	a.loadUploadPreviews(a.jobs)
	counts := a.uploadCounts()
	config := a.uploadDisplayConfig()
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		components.Text(c, tr("上传")).FontSize(22).Bold()
		ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(7).Children(func() {
			components.Icon(c, lucide.Cloud()).Size(14, 14).TextColor(t.TextMuted)
			destination := tr("尚未配置存储")
			if config.Bucket != "" {
				destination = strings.ToUpper(config.Provider) + " · " + config.Bucket
			}
			components.Text(c, destination).SingleLine().Ellipsis("…").FontSize(11).TextColor(t.TextMuted).MinWidth(0)
		})
		if components.Button(c, tr("存储设置"), components.ButtonGhost).Shrink(0).Clicked() {
			a.openSettings(0)
		}
	})
	if config.ValidateStorage() != nil {
		ui.Row(c).FillWidth().Shrink(0).Padding(8, 12).Radius(8).Border(1, t.Border).Background(t.Surface).AlignItems(ui.Center).Gap(8).Children(func() {
			components.Icon(c, lucide.Info()).Size(14, 14).TextColor(t.TextMuted)
			components.Text(c, tr("连接存储后，即可上传图片。")).Grow(1).MinWidth(0).Wrap().FontSize(11).TextColor(t.TextMuted)
			if components.Button(c, tr("配置存储"), components.ButtonGhost).Shrink(0).Clicked() {
				a.openSettings(0)
			}
		})
	}
	a.uploadDropZone(c, counts.total > 0)
	if counts.total > 0 {
		a.uploadQueueToolbar(c, counts)
		jobs := append([]*job(nil), a.jobs...)
		scrollView(c).Key("upload-files").FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				for _, j := range jobs {
					a.jobView(c, j)
				}
			})
		})
	} else {
		ui.Spacer(c)
	}
	ui.Row(c).FillWidth().Shrink(0).Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
		components.Icon(c, lucide.Sparkles()).Size(14, 14).TextColor(t.Accent)
		label := tr("保留原图")
		if config.Compress {
			label = tf("默认压缩 · 质量 %d", config.Quality)
		}
		components.Text(c, label).FontSize(11).TextColor(t.TextMuted)
		if components.Button(c, tr("图片设置"), components.ButtonGhost).Clicked() {
			a.openSettings(1)
		}
		ui.Spacer(c)
		components.Text(c, tr("添加后手动开始上传")).FontSize(11).TextColor(t.TextMuted)
	})
}

func (a *application) uploadDropZone(c *ui.Context, compact bool) {
	t := c.Theme()
	height := float32(300)
	if compact {
		height = 132
	}
	zone := ui.Box(c).Key("drop-zone").FillWidth().Shrink(0).Height(height).Radius(12).Border(1, t.Border.Mix(t.TextMuted, .25)).BorderStyle(ui.BorderDashed).Background(t.Surface).Disabled(a.busy)
	if !compact {
		zone.Shrink(1).MinHeight(180)
	}
	dragging := zone.FileDragOver()
	if paths := zone.DroppedFiles(); len(paths) > 0 {
		a.addFiles(paths)
	}
	zone.Children(func() {
		selectFiles := ui.ButtonBase(c).Key("choose-files").Label(tr("选择图片")).Fill().Background(ui.Color{}).Radius(12)
		if selectFiles.Hovered() || selectFiles.FocusVisible() || dragging {
			zone.BorderColor(t.Accent).Background(t.Surface.Mix(t.Accent, .035))
		}
		selectFiles.Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Justify(ui.Center).Padding(0, 16, 34, 16).Gap(10).Children(func() {
				if compact {
					ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
						components.Icon(c, lucide.ImageUp()).Size(28, 28).TextColor(t.TextMuted)
						ui.Column(c).Gap(5).Children(func() { uploadDropCopy(c, true, dragging) })
					})
				} else {
					components.Icon(c, lucide.ImageUp()).Size(44, 44).TextColor(t.TextMuted)
					uploadDropCopy(c, false, dragging)
				}
			})
		})
		if selectFiles.Clicked() {
			a.choose()
		}
		bottom := float32(28)
		if compact {
			bottom = 10
		}
		ui.Row(c).Absolute().Left(0).Right(0).Bottom(bottom).Height(30).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			if components.Button(c, tr("从剪贴板粘贴"), components.ButtonGhost).TextColor(t.Accent).Clicked() {
				a.paste()
			}
			if binding := a.shortcutBinding("paste"); binding.key != ui.KeyUnknown {
				components.Text(c, binding.label()).FontSize(10).TextColor(t.TextMuted)
			}
		})
	})
}

func uploadDropCopy(c *ui.Context, compact, dragging bool) {
	size := float32(16)
	if compact {
		size = 13
	}
	ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
		label := tr("拖入图片，或")
		if dragging {
			label = tr("松开即可添加图片")
		}
		components.Text(c, label).FontSize(size).Bold()
		if !dragging {
			components.Text(c, tr("选择图片")).FontSize(size).Bold().TextColor(c.Theme().Accent)
		}
	})
	components.Text(c, "PNG · JPEG · WebP · GIF · HEIC / AVIF · TIFF").FontSize(11).TextColor(c.Theme().TextMuted)
}

func (a *application) uploadQueueToolbar(c *ui.Context, counts uploadQueueCounts) {
	ui.Row(c).FillWidth().Shrink(0).Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
		components.Text(c, tf("文件列表 (%d)", counts.total)).FontSize(12).Bold()
		if a.uploading {
			components.Spinner(c, components.SpinnerRing).Size(14, 14)
			components.Text(c, tf("已处理 %d / %d", a.uploadBatchDone, a.uploadBatchTotal)).FontSize(11).TextColor(c.Theme().TextMuted)
		} else if counts.failed > 0 {
			components.Text(c, tf("%d 张失败", counts.failed)).FontSize(11).TextColor(c.Theme().Danger)
		} else if counts.uploaded > 0 {
			components.Text(c, tf("已上传 %d 张", counts.uploaded)).FontSize(11).TextColor(c.Theme().Success)
		}
		ui.Spacer(c)
		if counts.uploaded > 0 {
			if components.Button(c, tr("清除已完成"), components.ButtonGhost).Disabled(a.busy).Clicked() {
				a.clearUploadedJobs()
			}
			if components.IconButton(c, lucide.Copy(), tr("复制全部结果"), components.IconButtonToolbar).Clicked() {
				c.WriteClipboard(service.Format(a.uploadedResults(), a.uploadDisplayConfig().Output))
				a.feedback("已复制全部结果", nil)
			}
		}
		if components.IconButton(c, lucide.ListX(), tr("清空"), components.IconButtonPlain).Disabled(a.busy).Clicked() {
			a.jobs = nil
		}
		if a.uploading {
			if components.Button(c, tr("取消上传"), components.ButtonOutline).Clicked() && a.cancel != nil {
				a.cancel()
			}
		} else if counts.pending > 0 {
			if components.Button(c, tf("上传 %d 张图片", counts.pending), components.ButtonPrimary).Disabled(a.busy).Clicked() {
				a.upload()
			}
		}
	})
}

func (a *application) jobView(c *ui.Context, j *job) {
	t := c.Theme()
	ui.Column(c).Key(fmt.Sprintf("job-%d", j.id)).FillWidth().Padding(11, 12).Gap(8).Radius(10).Border(1, t.Border).Background(t.Surface).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Box(c).Size(44, 44).Shrink(0).Radius(6).Background(t.Background).Clip().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				if j.preview != nil {
					components.Image(c, j.preview).Fill().Fit(ui.Cover).Label(j.name)
				} else {
					components.Icon(c, lucide.Image()).Size(20, 20).TextColor(t.TextMuted)
				}
			})
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(5).Children(func() {
				components.Text(c, j.name).SingleLine().Ellipsis("…").FontSize(12).Bold()
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
					if j.result != nil {
						r := j.result
						components.Text(c, uploadSizeSummary(r)).FontSize(11).TextColor(t.TextMuted).Wrap()
					} else if j.sourceBytes > 0 {
						components.Text(c, bytesLabel(j.sourceBytes)).FontSize(11).TextColor(t.TextMuted)
					}
					status, tone := tr("待上传"), components.BadgeNeutral
					if j.result != nil {
						status, tone = tr("已上传"), components.BadgeSuccess
					} else if j.status == "上传失败" {
						status, tone = tr("上传失败"), components.BadgeDanger
					} else if j.status == "正在处理与上传" {
						status, tone = tr("正在处理与上传"), components.BadgeAccent
					}
					components.Badge(c, status, components.BadgeOptions{Tone: tone}).FontSize(10).Radius(5).Padding(2, 5)
				})
			})
			ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(2).Children(func() {
				if j.result != nil {
					if components.IconButton(c, lucide.Link(), tr("复制链接"), components.IconButtonToolbar).Clicked() {
						c.WriteClipboard(j.result.URL)
						a.feedback("已复制链接", nil)
					}
					if components.IconButton(c, lucide.Code(), tr("复制 Markdown"), components.IconButtonToolbar).Clicked() {
						c.WriteClipboard(j.result.Markdown())
						a.feedback("已复制 Markdown", nil)
					}
				} else if j.status == "正在处理与上传" {
					components.Spinner(c, components.SpinnerRing).Size(18, 18).Label(tr("正在处理与上传"))
				} else {
					icon, label := lucide.Upload(), tf("上传 %s", j.name)
					if j.status == "上传失败" {
						icon, label = lucide.RotateCcw(), tf("重试上传 %s", j.name)
					}
					if components.IconButton(c, icon, label, components.IconButtonToolbar).Disabled(a.busy).Clicked() {
						a.uploadSingle(j)
					}
				}
				if components.IconButton(c, lucide.X(), tf("移除 %s", j.name), components.IconButtonPlain).Disabled(a.busy).Clicked() {
					a.removeUploadJob(j)
				}
			})
		})
		if j.status == "上传失败" && j.result == nil && j.detail != "" {
			components.Text(c, j.detail).FillWidth().MinWidth(0).Wrap().FontSize(11).TextColor(t.Danger)
		}
	})
}
