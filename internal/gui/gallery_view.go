package gui

import (
	"fmt"
	"path"
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
func (a *application) galleryHeaderContext(c *ui.Context) {
	g := &a.gallery
	t := c.Theme()
	label := storageDestination(a.uploadDisplayConfig())
	if g.loaded {
		if g.paged && !g.listComplete {
			label += tf(" · 已加载 %d 张", len(g.objects))
		} else {
			label += tf(" · %d 张图片", len(g.objects))
		}
	}
	headerChip(c, nil, label)
	status, color := "", t.TextMuted
	switch {
	case g.busy:
		status = tr("正在处理云端请求…")
	case g.err != "" && g.loaded:
		status, color = tr("刷新未完成，已保留上次列表。"), t.Danger
	case g.dirty:
		status = tr("图库内容有变化，请刷新图库。")
	}
	if status != "" {
		ui.Row(c).Role(ui.RoleStatus).MinWidth(0).Shrink(1).Gap(6).AlignItems(ui.Center).Children(func() {
			if g.busy {
				components.Spinner(c, components.SpinnerRing).Size(12, 12)
			}
			components.Text(c, status).FontSize(12).TextColor(color).SingleLine().Ellipsis("…").MinWidth(0)
		})
	}
}

func (a *application) galleryHeaderActions(c *ui.Context) {
	g := &a.gallery
	t := c.Theme()
	if g.busy && g.cancel != nil {
		if components.Button(c, tr("取消请求"), components.ButtonGhost).Clicked() {
			g.cancel()
		}
	} else if g.loaded && g.err != "" {
		if components.Button(c, tr("重试刷新"), components.ButtonGhost).TextColor(t.Accent).Clicked() {
			if g.failedMore {
				a.loadMoreGallery()
			} else {
				a.loadGallery()
			}
		}
	}
	refresh := components.IconButton(c, galleryIcons.refresh, tr("刷新图库"), components.IconButtonPlain).Disabled(g.busy).TextColor(t.TextMuted)
	if g.dirty {
		refresh.TextColor(t.Accent)
	}
	if refresh.Clicked() {
		a.loadGallery()
	}
	if components.IconButton(c, lucide.Settings(), tr("存储设置"), components.IconButtonPlain).TextColor(t.TextMuted).Clicked() {
		a.openSettings(0)
	}
}

func (a *application) galleryView(c *ui.Context) {
	defer a.queueGalleryPreferences()
	g := &a.gallery
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
	a.galleryThumbs(page)
	page, presentedPage := g.presentedThumbnailPage(page)
	a.galleryDirectoryBar(c)
	a.galleryToolbar(c)
	if g.paged && !g.listComplete {
		ui.Row(c).FillWidth().Shrink(0).Padding(0, 24, 8, 24).Gap(10).AlignItems(ui.Center).Children(func() {
			components.Text(c, tr("列表尚未加载完；搜索、排序和日期筛选只作用于已加载图片。")).Grow(1).MinWidth(0).Wrap().FontSize(11).TextColor(c.Theme().TextMuted)
			if components.Button(c, tr("加载更多图片"), components.ButtonOutline).Shrink(0).Disabled(g.busy || a.busy || g.nextToken == "").Clicked() {
				a.loadMoreGallery()
			}
		})
	}
	if filterErr != nil {
		components.Text(c, filterErr.Error()).FontSize(12).Wrap().Padding(0, 24, 8, 24).TextColor(c.Theme().Danger)
	}
	ui.Column(c).Key("gallery-grid").FillWidth().Grow(1).MinHeight(0).Padding(0, 24).Children(func() {
		scroll := scrollView(c).Key(fmt.Sprintf("gallery-scroll-%d-%s", presentedPage, g.filterKey)).FillWidth().Grow(1).MinHeight(0).Transition(tabTransition())
		scroll.Children(func() {
			if !g.loaded || len(page) == 0 {
				a.galleryEmpty(c)
			} else {
				width, _ := c.Size()
				width -= 48 + a.sidebarWidth + scrollbarGutter(c)
				cells, height := g.justified(page, width)
				ui.Box(c).Width(width).Height(height).Children(func() {
					for _, cell := range cells {
						a.galleryPhoto(c, cell, rows)
					}
				})
			}
		})
	})
	if len(g.selected) > 0 && !g.previewOpen {
		a.gallerySelectionBar(c, page)
	}
	if g.loaded && len(rows) > 0 {
		a.galleryPagination(c, len(rows), pages, presentedPage)
	} else {
		ui.Box(c).Height(20).Shrink(0)
	}
	if !g.busy && !g.thumbLoading && !g.previewOpen && !g.renameOpen && !g.deleteOpen {
		if a.shortcut(c, "gallery-select-all") {
			g.selectPage(page)
		}
	}
}

func (a *application) galleryDirectoryBar(c *ui.Context) {
	g := &a.gallery
	if !g.prefixInputInitialized {
		g.prefixInput, g.prefixInputInitialized = g.prefix, true
	}
	ui.Row(c).Key("gallery-directory").FillWidth().Shrink(0).Padding(10, 24, 0, 24).Gap(8).AlignItems(ui.Center).Disabled(g.operation || a.busy).Children(func() {
		components.Text(c, tr("当前目录")).FontSize(12).Shrink(0)
		components.TextBox(c, &g.prefixInput, components.TextBoxOptions{}).Label(tr("目录前缀")).Placeholder(tr("全部目录，或输入 images/2026/")).Grow(1).MinWidth(100).Height(32)
		if components.Button(c, tr("加载目录"), components.ButtonOutline).Shrink(0).Clicked() {
			a.setGalleryPrefix(g.prefixInput)
		}
		if components.Button(c, tr("全部目录"), components.ButtonGhost).Shrink(0).Disabled(g.prefix == "" && g.prefixInput == "").Clicked() {
			a.setGalleryPrefix("")
		}
	})
}

// galleryChip is a toolbar control that shows its current value.
func galleryChip(c *ui.Context, label string, icon *ui.SVG, text string, active bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(label).Height(32).Shrink(0).Padding(0, 10).Gap(6).Radius(7).AlignItems(ui.Center)
	color := t.Text
	if active {
		b.Background(accentSoft(t))
		color = t.Accent
	} else {
		b.Border(1, controlBorder(t)).Background(t.Surface)
		if b.Hovered() {
			b.Background(t.SurfaceHover)
		}
	}
	b.Children(func() {
		iconColor := t.TextMuted
		if active {
			iconColor = t.Accent
		}
		components.Icon(c, icon).Size(14, 14).TextColor(iconColor)
		components.Text(c, text).FontSize(12.5).TextColor(color).SingleLine().MaxWidth(160).Ellipsis("…")
	})
	return b
}

// galleryFilterTag shows an active filter with its own clear action.
func galleryFilterTag(c *ui.Context, icon *ui.SVG, text, clearLabel string) bool {
	t := c.Theme()
	cleared := false
	ui.Row(c).Height(32).Shrink(0).Padding(0, 2, 0, 10).Gap(4).Radius(7).Background(accentSoft(t)).AlignItems(ui.Center).Children(func() {
		components.Icon(c, icon).Size(14, 14).TextColor(t.Accent)
		components.Text(c, text).FontSize(12.5).TextColor(t.Accent).SingleLine().MaxWidth(140).Ellipsis("…")
		cleared = components.IconButton(c, lucide.X(), clearLabel, components.IconButtonPlain).Size(26, 26).TextColor(t.Accent).Clicked()
	})
	return cleared
}

func (g *galleryState) sortLabel() string {
	if strings.TrimSpace(g.search) != "" {
		return tr("按匹配度")
	}
	property, desc := g.sortSettings()
	if property == "date" {
		if desc {
			return tr("日期 · 新 → 旧")
		}
		return tr("日期 · 旧 → 新")
	}
	if desc {
		return tr("名称 · Z → A")
	}
	return tr("名称 · A → Z")
}

// The pagination footer reports the filtered total, so the toolbar holds controls only.
func (a *application) galleryToolbar(c *ui.Context) {
	g := &a.gallery
	t := c.Theme()
	ui.Row(c).Key("gallery-toolbar").FillWidth().Shrink(0).Wrap().Padding(12, 24).AlignItems(ui.Center).Gap(8).Disabled(g.busy).Children(func() {
		components.TextBox(c, &g.search, components.TextBoxOptions{LeadingIcon: lucide.Search()}).Label(tr("搜索图片")).Placeholder(tr("按图片名称搜索...")).Width(220).MinWidth(150).Shrink(1).Height(32)
		ui.Box(c).Key("gallery-filter-control").Shrink(0).Children(func() {
			text := tr("筛选")
			if count := g.filterCount(); count > 0 {
				text = tf("筛选 · %d", count)
			}
			filter := galleryChip(c, tr("过滤选项"), lucide.Funnel(), text, false)
			if filter.Clicked() {
				g.filterOpen = !g.filterOpen
				g.sortOpen = false
			}
			a.galleryFilterPopover(c, filter)
		})
		if g.prefix != "" {
			label := g.prefix
			if label == "/" {
				label = tr("仅根目录")
			}
			if galleryFilterTag(c, lucide.Folder(), label, tr("清除前缀")) {
				a.setGalleryPrefix("")
			}
		}
		if g.period != "" && (g.period != "custom" || g.from != "" || g.to != "") {
			if galleryFilterTag(c, lucide.Calendar(), g.dateLabel(), tr("清除日期")) {
				g.period, g.from, g.to = "", "", ""
			}
		}
		ui.Box(c).Key("gallery-sort-control").Shrink(0).Children(func() {
			sort := galleryChip(c, tr("排序选项"), lucide.ArrowUpDown(), g.sortLabel(), false)
			if sort.Clicked() {
				g.sortOpen = !g.sortOpen
				g.filterOpen = false
			}
			a.gallerySortPopover(c, sort)
		})
		if g.hasViewPreferences() {
			if components.IconButton(c, lucide.RotateCcw(), tr("重置图库视图"), components.IconButtonPlain).TextColor(t.TextMuted).Clicked() {
				a.resetGalleryView()
			}
		}
	})
}

// selectionAction drops its text for an icon and tooltip in narrow windows.
func selectionAction(c *ui.Context, icon *ui.SVG, label, text string, compact bool) ui.Element {
	t := c.Theme()
	if compact && icon != nil {
		text = ""
	}
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Height(28).Shrink(0).Padding(0, 10).Gap(6).Radius(6).AlignItems(ui.Center).Background(ui.Color{})
	if b.Hovered() || b.FocusVisible() {
		b.Background(t.AccentText.Alpha(.14))
	}
	b.Children(func() {
		if icon != nil {
			components.Icon(c, icon).Size(14, 14).TextColor(t.AccentText)
		}
		if text != "" {
			components.Text(c, text).FontSize(12.5).TextColor(t.AccentText).SingleLine().Shrink(0)
		}
	})
	return b
}

// The selection bar replaces the toolbar while photos are selected.
func (a *application) gallerySelectionBar(c *ui.Context, page []storage.Object) {
	g := &a.gallery
	t := c.Theme()
	width, _ := c.Size()
	compact := width-a.sidebarWidth-48 < 640
	// The bar floats over the grid's lower edge, so filters stay in reach.
	ui.Column(c).Key("gallery-selection").Height(0).FillWidth().Shrink(0).Children(func() {
		ui.Row(c).Absolute().Left(24).Right(24).Bottom(12).Role(ui.RoleToolbar).Label(tr("已选图片操作")).Height(40).Padding(0, 4, 0, 12).Gap(2).Radius(10).Background(t.Accent).Shadow(0, 6, 20, 0, ui.Hex("#000000").Alpha(.18)).AlignItems(ui.Center).Children(func() {
			components.Text(c, tf("已选 %d 张", len(g.selected))).FontSize(12.5).FontWeight(600).TextColor(t.AccentText).Padding(0, 8, 0, 0).SingleLine().Shrink(0)
			if selectionAction(c, lucide.Link(), tr("复制所选链接"), tr("复制链接"), compact).Clicked() {
				c.WriteClipboard(galleryLinks(g.selectedObjects()))
				a.feedback(tr("已复制图片链接"), nil)
			}
			if selectionAction(c, lucide.Code(), tr("复制所选 Markdown"), tr("复制 Markdown"), compact).Clicked() {
				c.WriteClipboard(galleryMarkdown(g.selectedObjects()))
				a.feedback(tr("已复制 Markdown"), nil)
			}
			ui.Box(c).Disabled(g.busy).Children(func() {
				if selectionAction(c, lucide.Trash(), tf("删除所选 %d 张", len(g.selected)), tr("删除"), compact).Clicked() {
					g.requestDelete(g.selectedObjects())
				}
			})
			ui.Spacer(c)
			if selectionAction(c, galleryIcons.selection, tr("全选当前页"), tr("全选当前页"), compact).Clicked() {
				g.selectPage(page)
			}
			if selectionAction(c, lucide.X(), tr("清除选择"), tr("取消"), compact).Clicked() {
				g.selected = nil
				g.selectionOrder = nil
				g.selectionAnchor = ""
			}
		})
	})
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
func (a *application) galleryPagination(c *ui.Context, total, pages, presentedPage int) {
	g := &a.gallery
	t := c.Theme()
	ui.Row(c).FillWidth().Height(52).Shrink(0).Padding(0, 16, 0, 24).AlignItems(ui.Center).Gap(5).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		start := presentedPage * galleryPageSize
		caption := tf("第 %d–%d 张，共 %d 张", start+1, min(total, start+galleryPageSize), total)
		if g.paged && !g.listComplete {
			caption = tf("第 %d–%d 张，已加载结果 %d 张", start+1, min(total, start+galleryPageSize), total)
		}
		components.Text(c, caption).FontSize(12).TextColor(t.TextMuted).SingleLine()
		ui.Spacer(c)
		ui.Row(c).AlignItems(ui.Center).Gap(5).Disabled(g.busy || g.thumbLoading).Children(func() {
			if galleryIconButton(c, lucide.ChevronLeft(), tr("上一页")).Disabled(presentedPage == 0).Clicked() {
				g.page--
			}
			for i, n := range galleryPageNumbers(presentedPage, pages) {
				ui.Box(c).Key(i).Children(func() {
					if n < 0 {
						components.Text(c, "…").FontSize(12).Padding(8)
					} else {
						b := components.Button(c, "", components.ButtonGhost).Label(tf("第 %d 页", n+1)).Size(34, 34)
						b.Children(func() {
							if n == presentedPage && g.thumbLoading {
								components.Spinner(c, components.SpinnerRing).Size(14, 14).Label(tr("正在加载预览…"))
							} else {
								components.Text(c, fmt.Sprint(n+1)).SingleLine()
							}
						})
						if n == presentedPage {
							b.Background(sunkenColor(t)).Border(1, t.Border)
						}
						if b.Clicked() {
							g.page = n
						}
					}
				})
			}
		})
		if g.thumbLoading && len(g.thumbPage) > 0 && g.thumbPageFilter == g.filterKey {
			if galleryIconButton(c, lucide.X(), tr("取消请求")).Clicked() {
				if g.thumbCancel != nil {
					g.thumbCancel()
					g.thumbCancel = nil
				}
				g.page = g.thumbPageNumber
				g.thumbBatch = ""
				g.thumbRetry = ""
				g.thumbLoading = false
			}
		} else if galleryIconButton(c, lucide.ChevronRight(), tr("下一页")).Disabled(g.busy || g.thumbLoading || presentedPage+1 >= pages).Clicked() {
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
					if ok && thumb.err != nil && components.Button(c, tf("重试预览 %s", o.Key), components.ButtonOutline).Disabled(g.thumbLoading).Clicked() {
						// Keep the previous failure cached so cancellation can stay stopped.
						g.thumbRetry = o.Key
						g.thumbBatch = ""
					}
				})
			}
			if hover || selected {
				ui.Box(c).Absolute().Left(0).Top(0).Right(0).Height(min(float32(60), cell.h)).Gradient(ui.Hex("#000000").Alpha(.36), ui.Color{}, 180)
				ui.Box(c).Absolute().Top(8).Left(8).Disabled(g.busy || g.thumbLoading).Children(func() {
					check := galleryIconButton(c, galleryIcons.checked, tf("勾选 %s", o.Key)).Background(ui.Color{}).TextColor(ui.Hex("#ffffff"))
					if selected {
						check.TextColor(t.Accent).Background(t.Surface)
					}
					if check.Clicked() {
						g.toggleSelection(o.Key, check.ClickModifiers()&ui.Shift != 0, rows)
					}
				})
				ui.Row(c).Absolute().Top(8).Right(8).Gap(8).Disabled(g.thumbLoading).Children(func() {
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
					if !g.previewOpen && !g.thumbLoading {
						a.galleryPhotoOptions(c, more, o, true)
					}
				})
			}
		})
		if !g.busy && !g.thumbLoading {
			if double {
				g.openPhoto(o)
			} else if clicked {
				g.toggleSelection(o.Key, hit.ClickModifiers()&ui.Shift != 0, rows)
			}
		}
		if !g.thumbLoading {
			hit.ContextMenu(func(m *ui.Menu) { a.galleryContextMenu(c, m, o) })
		}
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
		layer := ui.Box(c).Absolute().Left(0).Top(48).Size(width, height).Background(t.Background).TextColor(t.Text).Modal().Label(tr("图片预览"))
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
			// Wide windows keep the photo's details beside it.
			info := float32(0)
			if width >= 860 {
				info = 288
			}
			stage := width - info
			if full.bitmap != nil {
				components.Image(c, full.bitmap).Absolute().Left(24).Top(64).Size(stage-48, height-88).Fit(ui.Contain).Label(tf("大图 %s", o.Key))
			}
			if info > 0 {
				a.galleryPreviewInfo(c, o, stage, info, height)
			}
			if g.full.err != nil || full.bitmap == nil {
				ui.Column(c).Absolute().Left(24).Width(stage - 48).Top(height * .45).AlignItems(ui.Center).Gap(12).Children(func() {
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
			ui.Row(c).Absolute().Left(0).Top(0).Width(stage).Height(52).Padding(0, 12).Gap(4).AlignItems(ui.Center).Children(func() {
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
				ui.Column(c).Grow(1).MinWidth(0).Padding(0, 0, 0, 8).Gap(1).Children(func() {
					components.Text(c, path.Base(o.Key)).FontSize(13).FontWeight(600).SingleLine().Ellipsis("…").MinWidth(0)
					if position := a.galleryPreviewPosition(o); position != "" {
						components.Text(c, position).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
					}
				})
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

// galleryPreviewPosition locates the photo within the current view, like "2 / 86".
func (a *application) galleryPreviewPosition(o storage.Object) string {
	rows, err := a.gallery.filtered(time.Now())
	if err != nil {
		return ""
	}
	for i, row := range rows {
		if row.Key == o.Key {
			return fmt.Sprintf("%d / %d", i+1, len(rows))
		}
	}
	return ""
}

func previewCopyField(c *ui.Context, value, label string) bool {
	t := c.Theme()
	copied := false
	ui.Row(c).FillWidth().MinWidth(0).Height(34).Padding(0, 4, 0, 10).Gap(6).Radius(7).Border(1, t.Border).Background(t.Background).AlignItems(ui.Center).Children(func() {
		components.Text(c, value).Grow(1).MinWidth(0).FontSize(11.5).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Tooltip(value)
		copied = components.IconButton(c, lucide.Copy(), label, components.IconButtonPlain).Size(26, 26).Clicked()
	})
	return copied
}

func (a *application) galleryPreviewInfo(c *ui.Context, o storage.Object, left, width, height float32) {
	g := &a.gallery
	t := c.Theme()
	ui.Column(c).Key("preview-info").Absolute().Left(left).Top(0).Size(width, height).Padding(20).Gap(18).Background(t.Surface).BorderWidth(0, 0, 0, 1).BorderColor(t.Border).Label(tr("图片信息")).Children(func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			components.Text(c, tr("信息")).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			modified := tr("时间未知")
			if !o.Modified.IsZero() {
				modified = o.Modified.Local().Format("2006-01-02 15:04")
			}
			for _, row := range [][2]string{{tr("路径"), o.Key}, {tr("大小"), bytesLabel(int(o.Size))}, {tr("修改时间"), modified}} {
				ui.Row(c).Key(row[0]).FillWidth().MinWidth(0).Gap(10).AlignItems(ui.Start).Children(func() {
					components.Text(c, row[0]).Width(64).Shrink(0).FontSize(12.5).TextColor(t.TextMuted)
					components.Text(c, row[1]).Grow(1).MinWidth(0).Wrap().FontSize(12.5)
				})
			}
		})
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			components.Text(c, tr("复制")).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			if previewCopyField(c, o.URL, tr("复制链接")) {
				c.WriteClipboard(o.URL)
				a.feedback(tr("已复制图片链接"), nil)
			}
			if previewCopyField(c, galleryMarkdown([]storage.Object{o}), tr("复制 Markdown")) {
				c.WriteClipboard(galleryMarkdown([]storage.Object{o}))
				a.feedback(tr("已复制 Markdown"), nil)
			}
		})
		ui.Spacer(c)
		ui.Row(c).FillWidth().Gap(6).Disabled(g.busy).Children(func() {
			if components.Button(c, tr("重命名"), components.ButtonOutline).Grow(1).Clicked() {
				g.beginRename(o)
			}
			if components.Button(c, tr("下载原图"), components.ButtonOutline).Grow(1).Clicked() {
				a.downloadGallery(o)
			}
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
