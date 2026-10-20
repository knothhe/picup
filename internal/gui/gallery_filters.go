package gui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

func galleryPopoverHeading(c *ui.Context, title, description string, open *bool) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		components.Text(c, title).Bold().FontSize(13)
		ui.Spacer(c)
		if galleryIconButton(c, lucide.X(), tf("关闭%s", title)).Size(24, 24).Background(ui.Color{}).Clicked() {
			*open = false
		}
	})
	components.Text(c, description).FontSize(11).TextColor(c.Theme().TextMuted).Wrap()
}
func galleryFilterLabel(c *ui.Context, title, description string) {
	components.Text(c, title).Bold().FontSize(12)
	if description != "" {
		components.Text(c, description).FontSize(11).TextColor(c.Theme().TextMuted).Wrap()
	}
}
func (a *application) galleryFilterPopover(c *ui.Context, anchor ui.Element) {
	g := &a.gallery
	components.Popover(c, anchor, &g.filterOpen, func(panel ui.Element) {
		width, height := c.Size()
		panel.Width(360).MaxWidth(width - 40).Label(tr("过滤选项"))
		galleryPopoverHeading(c, tr("过滤选项"), tr("根据属性筛选图片。"), &g.filterOpen)
		components.ScrollView(c, components.ScrollViewOptions{}).FillWidth().MaxHeight(height - 200).Children(func() {
			ui.Column(c).FillWidth().Gap(10).Children(func() {
				galleryFilterLabel(c, tr("搜索"), "")
				ui.Box(c).Key("filter-search").FillWidth().Children(func() {
					components.TextBox(c, &g.search, components.TextBoxOptions{}).FillWidth().Label(tr("筛选中搜索图片")).Placeholder(tr("按图片名称搜索..."))
				})
				galleryFilterLabel(c, tr("按前缀过滤"), tr("像在目录树中一样查找图像。"))
				a.galleryPrefixPicker(c)
				galleryFilterLabel(c, tr("按日期筛选"), tr("点击按钮打开日历以选择日期范围。"))
				date := ui.ButtonBase(c).FillWidth().Height(34).Padding(0, 10).Gap(8).Radius(7).Background(c.Theme().Surface).Border(1, c.Theme().Border).Label(tr("选择日期范围")).Justify(ui.Start)
				date.Children(func() {
					components.Icon(c, lucide.Calendar()).Size(16, 16)
					components.Text(c, g.dateLabel()).SingleLine().Ellipsis("…").FontSize(12).MinWidth(0)
				})
				if date.Clicked() {
					g.dateOpen = !g.dateOpen
					if g.dateOpen {
						month := time.Now().AddDate(0, -1, 0)
						if g.from != "" {
							if d, e := time.ParseInLocation("2006-01-02", g.from, time.Local); e == nil {
								month = d
							}
						}
						g.calendarMonth = time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.Local)
					}
				}
				a.galleryDatePopover(c, date)
			})
		})
	})
	if !g.filterOpen {
		g.dateOpen = false
	}
}
func (a *application) galleryPrefixPicker(c *ui.Context) {
	g := &a.gallery
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			s := components.Select(c, &g.prefix)
			label := g.prefix
			if label == "" {
				label = tr("选择前缀...")
			} else if label == "/" {
				label = tr("（无前缀）")
			} else {
				label = strings.TrimSuffix(label, "/")
			}
			s.Trigger.FillWidth().Label(tr("筛选目录")).Children(func() { components.Text(c, label).SingleLine().Ellipsis("…").FontSize(12).MinWidth(0).Grow(1) })
			s.Popup(func(panel ui.Element) {
				panel.Width(310).MaxWidth(640).Label(tr("目录前缀"))
				s.SearchField(&g.prefixQuery).Label(tr("搜索前缀")).Placeholder(tr("搜索前缀..."))
				keys, labels := g.prefixes()
				matches := 0
				components.ScrollView(c, components.ScrollViewOptions{}).FillWidth().MaxHeight(220).Children(func() {
					ui.Column(c).FillWidth().Children(func() {
						for i, key := range keys {
							if key == "" {
								continue
							}
							text := strings.TrimSuffix(labels[i], "/")
							if key == "/" {
								text = tr("（无前缀）")
							}
							if !strings.Contains(strings.ToLower(text), strings.ToLower(g.prefixQuery)) {
								continue
							}
							matches++
							depth := strings.Count(strings.TrimSuffix(key, "/"), "/")
							s.Item(key).Label(text).FillWidth().MinWidth(0).Padding(6, 10, 6, float32(10+depth*12)).Children(func() { components.Text(c, text).SingleLine().Ellipsis("…").FontSize(12).MinWidth(0).Grow(1) })
						}
						if matches == 0 {
							components.Text(c, tr("未找到前缀。")).FontSize(12).TextColor(c.Theme().TextMuted).Padding(8)
						}
					})
				})
			})
		})
		if galleryIconButton(c, lucide.X(), tr("清除前缀")).Disabled(g.prefix == "").Clicked() {
			g.prefix = ""
		}
	})
}
func (a *application) gallerySortPopover(c *ui.Context, anchor ui.Element) {
	g := &a.gallery
	components.Popover(c, anchor, &g.sortOpen, func(panel ui.Element) {
		panel.Width(300).Label(tr("排序选项"))
		active := strings.TrimSpace(g.search) != ""
		description := tr("选择图片的排序方式。")
		if active {
			description = tr("搜索已激活，排序选项已禁用。图片将按搜索匹配度排序。")
		}
		galleryPopoverHeading(c, tr("排序选项"), description, &g.sortOpen)
		property, desc := g.sortSettings()
		ui.Column(c).FillWidth().Gap(10).Disabled(active).Children(func() {
			galleryFilterLabel(c, tr("按 ... 排序"), tr("排序所依据的属性。"))
			before := property
			selectControl(c, tr("排序属性"), &property, []string{"date", "key"}, []string{tr("日期"), tr("名称 (路径)")})
			if property != before {
				g.setSort(property, desc)
			}
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() { galleryFilterLabel(c, tr("升降序"), tr("以升序或降序显示结果。")) })
				old := desc
				components.Switch(c, &desc, tr("排序顺序切换"), components.SwitchOptions{HideLabel: true})
				if old != desc {
					g.setSort(property, desc)
				}
			})
		})
	})
}
func (a *application) galleryDatePopover(c *ui.Context, anchor ui.Element) {
	g := &a.gallery
	components.Popover(c, anchor, &g.dateOpen, func(panel ui.Element) {
		width, height := c.Size()
		panel.Width(620).MaxWidth(width - 40).MaxHeight(height - 70).Padding(12).Gap(8).Label(tr("日期范围"))
		ui.Row(c).Gap(12).Children(func() {
			ui.Column(c).Width(108).Shrink(0).Gap(3).Children(func() {
				values := []string{"", "7d", "14d", "30d", "3m", "6m", "1y"}
				labels := []string{tr("全部（重置）"), tr("最近 7 天"), tr("最近 14 天"), tr("最近 30 天"), tr("最近 3 个月"), tr("最近 6 个月"), tr("最近 1 年")}
				for i, value := range values {
					if components.Button(c, labels[i], components.ButtonGhost).FillWidth().Justify(ui.Start).FontSize(11).Clicked() {
						g.period = value
						g.from = ""
						g.to = ""
						g.dateOpen = false
					}
				}
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					if galleryIconButton(c, lucide.ChevronLeft(), tr("上个月")).Size(26, 26).Clicked() {
						g.calendarMonth = g.calendarMonth.AddDate(0, -1, 0)
					}
					ui.Spacer(c)
					components.Text(c, tr("选择开始与结束日期")).FontSize(11).TextColor(c.Theme().TextMuted)
					ui.Spacer(c)
					if galleryIconButton(c, lucide.ChevronRight(), tr("下个月")).Size(26, 26).Clicked() {
						g.calendarMonth = g.calendarMonth.AddDate(0, 1, 0)
					}
				})
				ui.Row(c).FillWidth().Gap(12).Children(func() {
					a.galleryRangeMonth(c, g.calendarMonth)
					a.galleryRangeMonth(c, g.calendarMonth.AddDate(0, 1, 0))
				})
			})
		})
	})
}
func (a *application) galleryRangeMonth(c *ui.Context, month time.Time) {
	g := &a.gallery
	if month.IsZero() {
		month = time.Now()
	}
	month = time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.Local)
	ui.Column(c).Key(month.Format("2006-01")).Grow(1).MinWidth(0).Gap(8).Children(func() {
		components.Text(c, tf("%d 年 %d 月", month.Year(), month.Month())).FontSize(12).Bold().FillWidth().TextAlign(ui.Center)
		ui.Grid(c).FillWidth().Columns(7).Gap(2).Children(func() {
			for _, day := range []string{tr("一"), tr("二"), tr("三"), tr("四"), tr("五"), tr("六"), tr("日")} {
				components.Text(c, day).FontSize(10).TextColor(c.Theme().TextMuted).Height(24).TextAlign(ui.Center)
			}
			offset := (int(month.Weekday()) + 6) % 7
			for i := 0; i < offset; i++ {
				ui.Box(c).Height(26)
			}
			for day := 1; day <= month.AddDate(0, 1, -1).Day(); day++ {
				date := month.AddDate(0, 0, day-1)
				value := date.Format("2006-01-02")
				b := ui.ButtonBase(c).Height(26).MinWidth(0).Radius(4).Padding(0).Label(tf("选择日期 %s", value)).Background(ui.Color{})
				if g.period == "custom" && g.from != "" {
					if value == g.from || value == g.to {
						b.Background(c.Theme().Accent).TextColor(c.Theme().AccentText)
					} else if value > g.from && g.to != "" && value < g.to {
						b.Background(c.Theme().Accent.Alpha(.14))
					}
				}
				if b.Hovered() {
					b.Border(1, c.Theme().Accent)
				}
				b.Children(func() { components.Text(c, fmt.Sprint(day)).FontSize(11) })
				if b.Clicked() {
					g.chooseDate(date)
				}
			}
		})
	})
}
