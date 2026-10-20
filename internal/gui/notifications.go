package gui

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// One viewport covers every page and dialog. MyGo owns timing, announcements,
// keyboard dismissal and pausing while a notification is being read.
func (a *application) notificationView(c *ui.Context) {
	for _, notice := range a.notifications {
		c.AddToast(notice)
	}
	a.notifications = nil
	ui.ToastViewportBase(c, func(viewport ui.Element, notices []ui.Toast) {
		width, height := c.Size()
		viewport.Padding(16).Gap(8).AlignItems(ui.Start)
		for _, notice := range notices {
			parts := ui.ToastBase(c, notice)
			color, icon := c.Theme().Success, lucide.CircleCheck()
			if notice.Type == "error" {
				color, icon = c.Theme().Danger, lucide.CircleX()
			}
			parts.Root.Row().Width(min(float32(420), width-32)).MinWidth(0).Padding(12).Gap(10).AlignItems(ui.Start).
				Radius(10).Border(1, c.Theme().Border).Background(c.Theme().Surface).
				Shadow(0, 4, 18, 0, ui.RGBA(0, 0, 0, .12)).
				Transition(ui.ElementTransition{Duration: 180 * time.Millisecond, Enter: &ui.Motion{Y: 8}, Exit: &ui.Motion{Y: 8}}).
				Children(func() {
					components.Icon(c, icon).Size(18, 18).Shrink(0).TextColor(color)
					components.ScrollView(c, components.ScrollViewOptions{}).Grow(1).Shrink(1).MinWidth(0).MaxHeight(min(float32(160), height-120)).Children(func() {
						components.Text(c, notice.Title).FillWidth().MinWidth(0).Wrap().FontSize(12)
					})
					if notice.Action != "" {
						parts.ActionButton().Shrink(0).Padding(3, 6).Radius(5).Label(notice.Action).Background(c.Theme().Text.Alpha(.06)).Children(func() {
							components.Text(c, notice.Action).FontSize(11)
						})
					}
					parts.CloseButton().Size(20, 20).Shrink(0).Padding(0).Radius(4).Label(tr("关闭提示")).Background(ui.Color{}).Children(func() {
						components.Icon(c, lucide.X()).Size(14, 14).TextColor(c.Theme().TextMuted)
					})
				})
		}
	})
}
