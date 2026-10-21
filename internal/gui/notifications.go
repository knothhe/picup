package gui

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// Resume at blur as well as during rendering, so a foreground reading pause
// ends even when the next frame is delayed by hiding or occluding the window.
func (a *application) resumeNotificationTimers(now time.Time) {
	if a.notificationPausedAt.IsZero() {
		return
	}
	paused := now.Sub(a.notificationPausedAt)
	for id, deadline := range a.notificationDeadlines {
		a.notificationDeadlines[id] = deadline.Add(paused)
	}
	a.notificationPausedAt = time.Time{}
}

// One viewport covers every page and dialog. Only foreground hover and keyboard
// focus pause the app-owned clock. MyGo owns presentation and dismissal.
func (a *application) notificationView(c *ui.Context) {
	a.renderNotifications(c, a.win != nil && mygo.FocusedWindow() == a.win)
}

func (a *application) renderNotifications(c *ui.Context, windowFocused bool) {
	now := c.Now()
	a.resumeNotificationTimers(now)
	for _, notice := range a.notifications {
		lifetime := notice.Timeout
		if lifetime == 0 {
			lifetime = 4 * time.Second // MyGo's default Toast lifetime.
		}
		notice.Timeout = -1
		id, onClose := notice.ID, notice.OnClose
		notice.OnClose = func() {
			delete(a.notificationDeadlines, id)
			if len(a.notificationDeadlines) == 0 {
				a.notificationPausedAt = time.Time{}
			}
			if onClose != nil {
				onClose()
			}
		}
		id = c.AddToast(notice)
		if lifetime > 0 {
			if a.notificationDeadlines == nil {
				a.notificationDeadlines = make(map[string]time.Time)
			}
			a.notificationDeadlines[id] = now.Add(lifetime)
		} else {
			delete(a.notificationDeadlines, id)
		}
	}
	a.notifications = nil
	for id, deadline := range a.notificationDeadlines {
		if !deadline.After(now) {
			delete(a.notificationDeadlines, id)
			c.CloseToast(id)
		}
	}
	ui.ToastViewportBase(c, func(viewport ui.Element, notices []ui.Toast) {
		width, height := c.Size()
		viewport.Padding(16).Gap(8).AlignItems(ui.Start)
		if windowFocused && len(a.notificationDeadlines) > 0 && (viewport.Hovered() || viewport.FocusWithin()) {
			a.notificationPausedAt = now
		}
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
					scrollView(c).Grow(1).Shrink(1).MinWidth(0).MaxHeight(min(float32(160), height-120)).Children(func() {
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
	if a.notificationPausedAt.IsZero() {
		for _, deadline := range a.notificationDeadlines {
			c.After(deadline.Sub(now))
		}
	}
}
