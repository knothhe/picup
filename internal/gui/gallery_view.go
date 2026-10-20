package gui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/storage"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

func galleryMetadata(o storage.Object) string {
	date := tr("时间未知")
	if !o.Modified.IsZero() {
		date = o.Modified.Local().Format("2006-01-02 15:04")
	}
	return date + " · " + bytesLabel(int(o.Size))
}
func galleryMarkdown(objects []storage.Object) string {
	var lines []string
	for _, o := range objects {
		name := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "\n", " ", "\r", " ").Replace(o.Key)
		u := strings.NewReplacer("(", "%28", ")", "%29").Replace(o.URL)
		lines = append(lines, "!["+name+"]("+u+")")
	}
	return strings.Join(lines, "\n")
}
func galleryLinks(objects []storage.Object) string {
	var lines []string
	for _, o := range objects {
		lines = append(lines, o.URL)
	}
	return strings.Join(lines, "\n")
}
func galleryIconButton(c *ui.Context, icon *ui.SVG, label string) ui.Element {
	return components.IconButton(c, icon, label, components.IconButtonToolbar).Size(34, 34).Radius(7)
}
func galleryCountButton(c *ui.Context, icon *ui.SVG, label string, count int, danger bool) ui.Element {
	color := c.Theme().Text
	if danger {
		color = c.Theme().Danger
	}
	b := ui.ButtonBase(c).Label(label).Height(34).Padding(0, 10).Gap(6).Radius(7).Background(c.Theme().Surface).Border(1, c.Theme().Border).TextColor(color)
	b.Children(func() { components.Icon(c, icon).Size(18, 18); components.Text(c, fmt.Sprint(count)).FontSize(12) })
	return b
}
func (a *application) galleryView(c *ui.Context) {
	defer a.queueGalleryPreferences()
	g := &a.gallery
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		components.Text(c, tr("图库")).Bold().FontSize(22)
		ui.Spacer(c)
		label := tr("尚未配置存储")
		current := a.uploadDisplayConfig()
		if current.Bucket != "" {
			label = strings.ToUpper(current.Provider) + " · " + current.Bucket
		}
		if g.loaded {
			label += tf(" · %d 张图片", len(g.objects))
		}
		components.Text(c, label).FontSize(11).TextColor(c.Theme().TextMuted).SingleLine().Ellipsis("…").MinWidth(0)
		if galleryIconButton(c, lucide.Settings(), tr("存储设置")).Clicked() {
			a.navigate(2)
			a.settingsTab = 0
		}
	})
	a.galleryToolbar(c)
	key := g.viewFilterKey()
	if g.filterKey != key {
		g.filterKey = key
		g.page = 0
	}
	rows, filterErr := g.filtered(time.Now())
	page, pages := rows, 1
	if g.loaded {
		page, pages = g.pageRows(rows)
	}
	if filterErr != nil {
		components.Text(c, filterErr.Error()).FontSize(12).Wrap().TextColor(c.Theme().Danger)
	}
	a.galleryThumbs(page)
	scroll := scrollView(c).Key(fmt.Sprintf("gallery-scroll-%d-%s", g.page, g.filterKey)).FillWidth().Grow(1).MinHeight(0)
	scroll.Children(func() {
		if !g.loaded || len(page) == 0 {
			a.galleryEmpty(c)
		} else {
			width, _ := c.Size()
			width -= 48
			if !a.sidebarCollapsed {
				width -= 188
			}
			width -= scrollbarGutter(c)
			cells, height := g.justified(page, width)
			ui.Box(c).Width(width).Height(height).Children(func() {
				for _, cell := range cells {
					a.galleryPhoto(c, cell, rows)
				}
			})
		}
	})
	if g.loaded && len(rows) > 0 {
		a.galleryPagination(c, pages)
	}
	if g.busy || g.err != "" || g.dirty {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
			status := tr("图库内容有变化，请刷新图库。")
			if g.err != "" {
				status = tr("刷新未完成，已保留上次列表。")
			}
			if g.busy {
				status = tr("正在处理云端请求…")
			}
			components.Text(c, status).FontSize(11).TextColor(c.Theme().TextMuted)
			if g.busy {
				if components.Button(c, tr("取消请求"), components.ButtonGhost).Clicked() && g.cancel != nil {
					g.cancel()
				}
			} else if g.loaded {
				if components.Button(c, tr("重试刷新"), components.ButtonOutline).Clicked() {
					a.loadGallery()
				}
			}
		})
	}
	if !g.busy && !g.previewOpen && !g.renameOpen && !g.deleteOpen {
		if a.shortcut(c, "gallery-select-all") {
			g.selectPage(page)

		}
	}
}
func (a *application) galleryEmpty(c *ui.Context) {
	g := &a.gallery
	ui.Column(c).FillWidth().MinHeight(256).Padding(28).Gap(16).AlignItems(ui.Center).Justify(ui.Center).Radius(10).Border(1, c.Theme().Border).Children(func() {
		components.Icon(c, galleryIcons.empty).Size(48, 48).TextColor(c.Theme().TextMuted)
		title := tr("未找到照片")
		if g.busy {
			title = tr("正在加载照片…")
		} else if !g.loaded && g.err != "" {
			title = tr("图库加载失败")
		}
		components.Text(c, title).FontSize(16).TextColor(c.Theme().TextMuted)
		if !g.loaded && g.err != "" {
			components.Text(c, g.err).Wrap().FontSize(12).TextColor(c.Theme().TextMuted)
		}
		ui.Row(c).Gap(8).Children(func() {
			if components.Button(c, tr("加载图片"), components.ButtonOutline).Disabled(g.busy).Clicked() {
				a.loadGallery()
			}
			if g.loaded && len(g.objects) > 0 && components.Button(c, tr("重置筛选"), components.ButtonGhost).Clicked() {
				a.resetGalleryView()
			}
		})
	})
}
func (a *application) galleryToolbar(c *ui.Context) {
	g := &a.gallery
	ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Row(c).Shrink(0).Gap(8).Disabled(g.busy).Children(func() {
			if galleryIconButton(c, galleryIcons.refresh, tr("刷新图库")).Background(c.Theme().Accent).TextColor(c.Theme().AccentText).Clicked() {
				a.loadGallery()
			}
			if len(g.selected) > 0 {
				if galleryCountButton(c, galleryIcons.selection, tr("清除选择"), len(g.selected), false).Clicked() {
					g.selected = nil
					g.selectionOrder = nil
					g.selectionAnchor = ""
				}
				if galleryCountButton(c, galleryIcons.trash, tf("删除所选 %d 张", len(g.selected)), len(g.selected), true).Clicked() {
					g.requestDelete(g.selectedObjects())
				}
			}
		})
		ui.Row(c).Width(320).MinWidth(252).Grow(1).AlignItems(ui.Center).Justify(ui.End).Gap(8).Disabled(g.busy).Children(func() {
			components.TextBox(c, &g.search, components.TextBoxOptions{}).Label(tr("搜索图片")).Placeholder(tr("按图片名称搜索...")).Width(180).MinWidth(180).MaxWidth(288).Grow(1)
			ui.Box(c).Key("gallery-filter-control").Size(34, 34).Children(func() {
				filter := galleryIconButton(c, lucide.Funnel(), tr("过滤选项")).Background(c.Theme().Accent).TextColor(c.Theme().AccentText)
				if filter.Clicked() {
					g.filterOpen = !g.filterOpen
					g.sortOpen = false
				}
				count := g.filterCount()
				if count > 0 {
					ui.Box(c).Absolute().Right(-4).Top(-5).Size(16, 16).Radius(8).Background(c.Theme().Danger).AlignItems(ui.Center).Justify(ui.Center).Children(func() { components.Text(c, fmt.Sprint(count)).FontSize(9).TextColor(ui.Hex("#ffffff")) })
				}
				a.galleryFilterPopover(c, filter)
			})
			ui.Box(c).Key("gallery-sort-control").Size(34, 34).Children(func() {
				sort := galleryIconButton(c, lucide.ArrowUpDown(), tr("排序选项"))
				if sort.Clicked() {
					g.sortOpen = !g.sortOpen
					g.filterOpen = false
				}
				a.gallerySortPopover(c, sort)
			})
		})
		if components.Button(c, tr("重置"), components.ButtonGhost).Label(tr("重置图库视图")).Height(34).Disabled(!g.hasViewPreferences()).Clicked() {
			a.resetGalleryView()
		}
	})
}
func (a *application) galleryPagination(c *ui.Context, pages int) {
	g := &a.gallery
	ui.Row(c).FillWidth().Shrink(0).Justify(ui.Center).AlignItems(ui.Center).Gap(5).Disabled(g.busy).Children(func() {
		if galleryIconButton(c, lucide.ChevronLeft(), tr("上一页")).Disabled(g.page == 0).Clicked() {
			g.page--
		}
		for i, n := range galleryPageNumbers(g.page, pages) {
			ui.Box(c).Key(i).Children(func() {
				if n < 0 {
					components.Text(c, "…").FontSize(12).Padding(8)
				} else {
					b := components.Button(c, fmt.Sprint(n+1), components.ButtonGhost).Label(tf("第 %d 页", n+1)).Size(34, 34)
					if n == g.page {
						b.Background(c.Theme().Surface).Border(1, c.Theme().Border)
					}
					if b.Clicked() {
						g.page = n
					}
				}
			})
		}
		if galleryIconButton(c, lucide.ChevronRight(), tr("下一页")).Disabled(g.page+1 >= pages).Clicked() {
			g.page++
		}
	})
}
func (a *application) galleryPhoto(c *ui.Context, cell galleryCell, rows []storage.Object) {
	g := &a.gallery
	o := cell.object
	t := c.Theme()
	ui.Box(c).Key(o.Key).Absolute().Left(cell.x).Top(cell.y).Size(cell.w, cell.h).Clip().Children(func() {
		selected := g.selected[o.Key]
		hit := ui.Box(c).Fill().Focusable().Role(ui.RoleCheckBox).Checked(selected).Label(tf("选择 %s", o.Key))
		clicked, double := hit.Clicked(), hit.DoubleClicked()
		hover := hit.Hovered() || hit.FocusWithin() || (g.optionsOpen && g.optionsKey == o.Key)
		hit.Children(func() {
			thumb, ok := g.thumbs[o.Key]
			if thumb.bitmap != nil {
				image := components.Image(c, thumb.bitmap).Fill().Fit(ui.Contain).Label(o.Key)
				if selected {
					image.Scale(.9, .9).Radius(8)
				}
			} else {
				ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Background(t.Text.Alpha(.04)).Gap(8).Children(func() {
					components.Icon(c, lucide.Image()).Size(28, 28).TextColor(t.TextMuted)
					label := tr("正在加载预览…")
					if ok && thumb.err != nil {
						label = tr("图片加载失败")
					}
					components.Text(c, label).FontSize(12).TextColor(t.TextMuted)
					if ok && thumb.err != nil && components.Button(c, tf("重试预览 %s", o.Key), components.ButtonOutline).Clicked() {
						delete(g.thumbs, o.Key)
						g.thumbBatch = ""
					}
				})
			}
			if hover || selected {
				ui.Box(c).Absolute().Left(0).Top(0).Right(0).Height(min(float32(60), cell.h)).Gradient(ui.Hex("#000000").Alpha(.36), ui.Color{}, 180)
				ui.Box(c).Absolute().Top(8).Left(8).Disabled(g.busy).Children(func() {
					check := galleryIconButton(c, galleryIcons.checked, tf("勾选 %s", o.Key)).Background(ui.Color{}).TextColor(ui.Hex("#ffffff"))
					if selected {
						check.TextColor(t.Accent).Background(t.Surface)
					}
					if check.Clicked() {
						g.toggleSelection(o.Key, check.ClickModifiers()&ui.Shift != 0, rows)
					}
				})
				ui.Row(c).Absolute().Top(8).Right(8).Gap(8).Children(func() {
					if galleryIconButton(c, galleryIcons.copy, tf("复制链接 %s", o.Key)).Clicked() {
						c.WriteClipboard(o.URL)
						a.feedback(tr("已复制图片链接"), nil)
					}
					more := galleryIconButton(c, lucide.Ellipsis(), tf("图片操作 %s", o.Key)).Disabled(g.busy)
					if more.Clicked() {
						g.optionsOpen = !(g.optionsOpen && g.optionsKey == o.Key)
						g.optionsKey = o.Key
						g.infoOpen = false
					}
					if !g.previewOpen {
						a.galleryPhotoOptions(c, more, o, true)
					}
				})
			}
		})
		if !g.busy {
			if double {
				g.openPhoto(o)
			} else if clicked {
				g.toggleSelection(o.Key, hit.ClickModifiers()&ui.Shift != 0, rows)
			}
		}
		hit.ContextMenu(func(m *ui.Menu) { a.galleryContextMenu(c, m, o) })
	})
}
func (a *application) galleryContextMenu(c *ui.Context, m *ui.Menu, o storage.Object) {
	g := &a.gallery
	if m.Item(tr("打开")).Chosen() {
		g.openPhoto(o)
	}
	m.Submenu(tr("信息"), func(m *ui.Menu) { m.Item(galleryMetadata(o)).Disabled(true); m.Item(o.Key).Disabled(true) })
	if m.Item(tr("复制链接")).Chosen() {
		c.WriteClipboard(o.URL)
	}
	if m.Item(tr("复制 Markdown")).Chosen() {
		c.WriteClipboard(galleryMarkdown([]storage.Object{o}))
	}
	if m.Item(tr("下载原图")).Disabled(g.busy).Chosen() {
		a.downloadGallery(o)
	}
	if m.Item(tr("重命名")).Disabled(g.busy).Chosen() {
		g.beginRename(o)
	}
	m.Separator()
	if m.Item(tr("删除图片")).Disabled(g.busy).Chosen() {
		g.requestDelete([]storage.Object{o})
	}
}
func galleryMenuAction(c *ui.Context, icon *ui.SVG, label string, danger bool) bool {
	b := ui.ButtonBase(c).FillWidth().Height(32).Padding(0, 8).Gap(10).Radius(5).Background(ui.Color{}).Label(label).Justify(ui.Start)
	if b.Hovered() {
		b.Background(c.Theme().Text.Alpha(.07))
	}
	if danger {
		b.TextColor(c.Theme().Danger)
	}
	b.Children(func() { components.Icon(c, icon).Size(16, 16); components.Text(c, label).FontSize(12) })
	return b.Clicked()
}
func (a *application) galleryPhotoOptions(c *ui.Context, anchor ui.Element, o storage.Object, allowOpen bool) {
	g := &a.gallery
	if g.optionsKey != o.Key || !g.optionsOpen {
		return
	}
	components.Popover(c, anchor, &g.optionsOpen, func(panel ui.Element) {
		panel.Width(250).Padding(6).Gap(2).Label(tr("图片操作"))
		if allowOpen && galleryMenuAction(c, lucide.Expand(), tr("打开"), false) {
			g.openPhoto(o)
		}
		if galleryMenuAction(c, galleryIcons.info, tr("信息"), false) {
			g.infoOpen = !g.infoOpen
		}
		if g.infoOpen {
			ui.Column(c).FillWidth().Padding(8).Gap(6).Children(func() {
				ui.Row(c).FillWidth().Gap(8).Children(func() {
					components.Icon(c, galleryIcons.time).Size(14, 14)
					components.Text(c, galleryMetadata(o)).FontSize(11).Wrap().MinWidth(0)
				})
				ui.Row(c).FillWidth().Gap(8).Children(func() {
					components.Icon(c, galleryIcons.key).Size(14, 14)
					components.Text(c, o.Key).FontSize(11).Wrap().MinWidth(0).Grow(1)
				})
			})
		}
		if galleryMenuAction(c, lucide.Copy(), tr("复制 Markdown"), false) {
			c.WriteClipboard(galleryMarkdown([]storage.Object{o}))
			g.optionsOpen = false
			a.feedback(tr("已复制 Markdown"), nil)
		}
		if galleryMenuAction(c, lucide.Download(), tr("下载原图"), false) {
			g.optionsOpen = false
			a.downloadGallery(o)
		}
		if galleryMenuAction(c, lucide.Pencil(), tr("重命名"), false) {
			g.beginRename(o)
		}
		if galleryMenuAction(c, lucide.Trash(), tr("删除图片"), true) {
			g.requestDelete([]storage.Object{o})
		}
	})
}
func galleryPreviewButton(c *ui.Context, icon *ui.SVG, label string) ui.Element {
	return components.IconButton(c, icon, label, components.IconButtonPlain).Size(34, 34).Radius(7)
}
func (a *application) galleryPreview(c *ui.Context) {
	g := &a.gallery
	a.galleryFullPreview()
	if !g.previewOpen || g.preview == nil {
		return
	}
	o := *g.preview
	t := *c.Theme()
	t.Background = ui.Hex("#121214")
	t.Surface = ui.Hex("#232327")
	t.Text = ui.Hex("#ffffff")
	t.TextMuted = ui.Hex("#a1a1aa")
	t.Border = ui.Hex("#39393e")
	t.SurfaceHover = ui.Hex("#323237")
	t.Danger = ui.Hex("#f28b82")
	previous := c.Theme()
	c.SetTheme(&t)
	defer c.SetTheme(previous)
	ui.Overlay(c, func() {
		width, height := c.Size()
		height -= 48
		layer := ui.Box(c).Absolute().Left(0).Top(48).Size(width, height).Background(t.Background).Modal().Label(tr("图片预览"))
		if !g.renameOpen && !g.deleteOpen && !g.optionsOpen {
			if a.overlayShortcut(layer, "preview-close") {
				g.previewOpen = false
			}
			if a.overlayShortcut(layer, "preview-previous") {
				g.movePreview(-1, c.Now())
			}
			if a.overlayShortcut(layer, "preview-next") {
				g.movePreview(1, c.Now())
			}
			o = *g.preview
			a.galleryFullPreview()
		}
		layer.Children(func() {
			full := g.full
			if full.bitmap == nil {
				full = g.thumbs[o.Key]
			}
			if full.bitmap != nil {
				components.Image(c, full.bitmap).Absolute().Left(0).Top(0).Size(width, height).Fit(ui.Contain).Label(tf("大图 %s", o.Key))
			}
			if g.full.err != nil || full.bitmap == nil {
				ui.Column(c).Absolute().Left(24).Right(24).Top(height * .45).AlignItems(ui.Center).Gap(12).Children(func() {
					message := tr("正在加载原图…")
					if g.full.err != nil {
						message = tf("原图加载失败：%s", g.full.err.Error())
					}
					components.Text(c, message).FontSize(12).TextColor(t.TextMuted).Wrap()
					if g.full.err != nil {
						if components.Button(c, tr("重试原图"), components.ButtonOutline).Clicked() {
							g.fullKey = ""
						}
					}
				})
			}
			ui.Row(c).Absolute().Left(0).Top(0).Right(0).Padding(8).Gap(8).AlignItems(ui.Center).Gradient(ui.Hex("#000000").Alpha(.40), ui.Color{}, 180).Children(func() {
				if galleryPreviewButton(c, galleryIcons.back, a.shortcutTooltip("返回图库", "preview-close")).Label(tr("返回图库")).Background(ui.Color{}).Clicked() {
					g.previewOpen = false
					g.optionsOpen = false
				}
				if galleryPreviewButton(c, lucide.ChevronLeft(), a.shortcutTooltip("上一张图片", "preview-previous")).Label(tr("上一张图片")).Background(ui.Color{}).Clicked() {
					g.movePreview(-1, c.Now())
					a.galleryFullPreview()
				}
				if galleryPreviewButton(c, lucide.ChevronRight(), a.shortcutTooltip("下一张图片", "preview-next")).Label(tr("下一张图片")).Background(ui.Color{}).Clicked() {
					g.movePreview(1, c.Now())
					a.galleryFullPreview()
				}
				ui.Spacer(c)
				if galleryPreviewButton(c, lucide.Copy(), tr("复制链接")).Background(ui.Color{}).Clicked() {
					c.WriteClipboard(o.URL)
					a.feedback(tr("已复制图片链接"), nil)
				}
				if galleryPreviewButton(c, lucide.Trash(), tr("删除图片")).Background(ui.Color{}).Disabled(g.busy).Clicked() {
					g.requestDelete([]storage.Object{o})
				}
				more := galleryPreviewButton(c, lucide.CircleEllipsis(), tr("图片操作")).Background(ui.Color{}).Disabled(g.busy)
				if more.Clicked() {
					g.optionsOpen = !g.optionsOpen
					g.optionsKey = o.Key
					g.infoOpen = false
				}
				a.galleryPhotoOptions(c, more, o, false)
			})
		})
	})
}
func (a *application) galleryDialogs(c *ui.Context) {
	g := &a.gallery
	ui.Column(c).Key("gallery-preview-dialog").Children(func() { a.galleryPreview(c) })
	ui.Column(c).Key("gallery-rename-dialog").Children(func() {
		components.Dialog(c, &g.renameOpen, components.DialogOptions{Label: tr("重命名云端图片"), DisableBackdropClose: g.busy, DisableEscapeClose: g.busy}, func(panel ui.Element) {
			if g.preview == nil {
				return
			}
			components.Text(c, tr("重命名云端图片")).Bold()
			components.Text(c, tf("当前路径：%s", g.preview.Key)).SingleLine().Ellipsis("…").FontSize(12).TextColor(c.Theme().TextMuted)
			components.TextBox(c, &g.renameKey, components.TextBoxOptions{}).FillWidth().Label(tr("新图片路径")).Disabled(g.busy)
			components.Text(c, tr("填写完整路径。完成后原链接失效，新链接可用。")).FontSize(12).Wrap().TextColor(c.Theme().TextMuted)
			if g.renameError != "" {
				scrollView(c).FillWidth().MaxHeight(100).Children(func() { components.Text(c, g.renameError).FontSize(12).Wrap().TextColor(c.Theme().Danger) })
			}
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Disabled(g.busy).Children(func() {
				if components.Button(c, tr("取消"), components.ButtonSecondary).Clicked() {
					g.renameOpen = false
				}
				if components.Button(c, tr("保存新名称"), components.ButtonPrimary).Clicked() {
					a.renameGallery()
				}
			})
		})
	})
	ui.Column(c).Key("gallery-delete-dialog").Children(func() {
		components.Dialog(c, &g.deleteOpen, components.DialogOptions{Label: tr("删除云端图片")}, func(panel ui.Element) {
			panel.Role(ui.RoleAlertDialog)
			components.Text(c, tf("删除 %d 张云端图片？", len(g.deleteTargets))).Bold()
			components.Text(c, tr("图片将从当前存储桶删除，已有链接将失效。此操作无法撤销。")).Wrap().FontSize(12).TextColor(c.Theme().TextMuted)
			scrollView(c).FillWidth().MaxHeight(120).Children(func() {
				ui.Column(c).FillWidth().Gap(5).Children(func() {
					for _, o := range g.deleteTargets {
						components.Text(c, o.Key).SingleLine().Ellipsis("…").FontSize(11)
					}
				})
			})
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				if components.Button(c, tr("取消"), components.ButtonSecondary).Clicked() {
					g.deleteOpen = false
				}
				if components.Button(c, tr("确认删除云端图片"), components.ButtonPrimary).Disabled(g.busy || len(g.deleteTargets) == 0).Clicked() {
					g.deleteOpen = false
					a.deleteGallery()
				}
			})
		})
	})
}
