package gui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

type notificationWindowTester struct {
	*ui.Tester
	app     *application
	focused bool
}

func notificationTester(a *application) *notificationWindowTester {
	tt := &notificationWindowTester{app: a, focused: true}
	tt.Tester = ui.NewTester(func(c *ui.Context) { a.renderNotifications(c, tt.focused) }, 760, 620)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	return tt
}

func (tt *notificationWindowTester) SetFocused(focused bool) {
	tt.focused = focused
	if !focused {
		tt.app.resumeNotificationTimers(tt.Now())
	}
	tt.Tester.SetFocused(focused)
}

func TestNotificationPausesForForegroundReadingUntilBlur(t *testing.T) {
	for _, interaction := range []string{"hover", "keyboard focus"} {
		t.Run(interaction, func(t *testing.T) {
			a := &application{}
			tt := notificationTester(a)
			a.feedback("已复制图片链接", nil)
			tt.Frame()
			tt.Advance(2 * time.Second)
			if interaction == "hover" {
				r, _ := tt.Find(a.message)
				tt.Move(r.X+1, r.Y+1)
			} else {
				tt.Key(0, ui.KeyTab)
				if !tt.Focused("关闭提示") {
					t.Fatal("notification close control did not receive keyboard focus")
				}
			}
			tt.Advance(10 * time.Second)
			if !tt.HasText(a.message) {
				t.Fatal("notification expired while being read in the foreground")
			}
			tt.SetFocused(false)
			tt.Advance(2 * time.Second)
			if !tt.HasText(a.message) {
				t.Fatal("blur discarded the remaining notification lifetime")
			}
			tt.Advance(time.Second)
			if tt.HasText(a.message) {
				t.Fatal("foreground reading pause continued after blur")
			}
		})
	}
}

func TestNotificationHoverPauseResumesWhenPointerLeaves(t *testing.T) {
	a := &application{}
	tt := notificationTester(a)
	a.feedback("已复制图片链接", nil)
	tt.Frame()
	tt.Advance(2 * time.Second)
	r, _ := tt.Find(a.message)
	tt.Move(r.X+1, r.Y+1)
	tt.Advance(10 * time.Second)
	tt.Move(1, 1)
	tt.Advance(3 * time.Second)
	if tt.HasText(a.message) {
		t.Fatal("notification remained paused after the pointer left")
	}
}

func TestNotificationBlurResumesBeforeDelayedFrame(t *testing.T) {
	a := &application{}
	tt := notificationTester(a)
	a.feedback("已复制图片链接", nil)
	tt.Frame()
	tt.Advance(2 * time.Second)
	r, _ := tt.Find(a.message)
	tt.Move(r.X+1, r.Y+1)
	tt.Advance(10 * time.Second)
	// The native blur hook runs even if an occluded window cannot render yet.
	tt.focused = false
	a.resumeNotificationTimers(tt.Now())
	tt.Advance(10 * time.Second)
	tt.SetFocused(true)
	if tt.HasText(a.message) {
		t.Fatal("delayed rendering extended the foreground pause through background time")
	}
}

func TestNotificationTimeoutContinuesWithoutWindowFocus(t *testing.T) {
	for _, scenario := range []struct {
		name              string
		backgroundAtStart bool
		hoverBeforeBlur   bool
	}{
		{name: "loses focus"},
		{name: "already in background", backgroundAtStart: true},
		{name: "hovered before losing focus", hoverBeforeBlur: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			a := &application{}
			tt := notificationTester(a)
			if scenario.backgroundAtStart {
				tt.SetFocused(false)
			}
			a.feedback("上传成功，测试对象已清理", nil)
			tt.Frame()
			if !tt.HasText(a.message) {
				t.Fatal("success notification did not appear")
			}
			tt.Advance(2 * time.Second)
			if scenario.hoverBeforeBlur {
				r, _ := tt.Find(a.message)
				tt.Move(r.X+1, r.Y+1)
			}
			tt.SetFocused(false)
			tt.Advance(3 * time.Second)
			if tt.HasText(a.message) {
				t.Fatal("success notification did not expire after five seconds in the background")
			}
			tt.SetFocused(true)
			if tt.HasText(a.message) {
				t.Fatal("expired notification returned when the window regained focus")
			}
		})
	}
}

func TestNotificationClosedIDCanBeReused(t *testing.T) {
	a := &application{}
	tt := notificationTester(a)
	closed := 0
	a.notifications = append(a.notifications, ui.Toast{
		ID: "copy", Title: "第一次复制", Timeout: 5 * time.Second,
		OnClose: func() { closed++ },
	})
	tt.Frame()
	tt.Advance(2 * time.Second)
	if err := tt.Click("关闭提示"); err != nil {
		t.Fatal(err)
	}
	if closed != 1 || len(a.notificationDeadlines) != 0 {
		t.Fatal("manual dismissal retained the automatic deadline or skipped OnClose")
	}
	tt.Advance(time.Second)
	a.notifications = append(a.notifications, ui.Toast{
		ID: "copy", Title: "再次复制", Timeout: 5 * time.Second,
		OnClose: func() { closed++ },
	})
	tt.Frame()
	tt.SetFocused(false)
	tt.Advance(2 * time.Second)
	if !tt.HasText("再次复制") {
		t.Fatal("reopened notification expired at the dismissed notification's deadline")
	}
	tt.Advance(3 * time.Second)
	if tt.HasText("再次复制") || closed != 2 || len(a.notificationDeadlines) != 0 {
		t.Fatal("reopened notification did not expire once and release its deadline")
	}
}

func TestNotificationReplacementRestartsTimeoutInBackground(t *testing.T) {
	a := &application{}
	tt := notificationTester(a)
	a.feedback("已复制图片链接", nil)
	tt.Frame()
	tt.Advance(2 * time.Second)
	tt.SetFocused(false)
	tt.Advance(2 * time.Second)
	a.feedback("已复制图片链接", nil)
	tt.Frame()
	tt.Advance(time.Second)
	if !tt.HasText(a.message) {
		t.Fatal("repeated notification used the previous notification's deadline")
	}
	tt.Advance(4 * time.Second)
	if tt.HasText(a.message) {
		t.Fatal("repeated notification did not expire five seconds after its update")
	}
}

func TestPersistentNotificationReplacementAndDismissal(t *testing.T) {
	a := &application{}
	tt := notificationTester(a)
	closed, acted := 0, 0
	a.notifications = append(a.notifications, ui.Toast{ID: "result", Title: "处理中", Timeout: time.Second})
	tt.Frame()
	tt.Advance(500 * time.Millisecond)
	a.notifications = append(a.notifications, ui.Toast{
		ID: "result", Title: "上传失败", Type: "error", Timeout: -1, Action: "重试",
		OnAction: func() { acted++ },
		OnClose:  func() { closed++ },
	})
	tt.Frame()
	tt.SetFocused(false)
	tt.Advance(time.Minute)
	if !tt.HasText("上传失败") || closed != 0 {
		t.Fatal("persistent error was dismissed by the notification it replaced")
	}
	tt.SetFocused(true)
	if err := tt.Click("重试"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Minute)
	if tt.HasText("上传失败") || acted != 1 || closed != 1 || len(a.notificationDeadlines) != 0 {
		t.Fatalf("notification action/dismissal changed: action=%d close=%d", acted, closed)
	}
}

func TestNotificationDeadlinesReleasedWithPresentation(t *testing.T) {
	a := &application{}
	tt := notificationTester(a)
	a.feedback("上传成功", nil)
	tt.Frame()
	if len(a.notificationDeadlines) != 1 {
		t.Fatal("automatic notification has no deadline")
	}
	a.releaseUIPresentation()
	if a.notificationDeadlines != nil || !a.notificationPausedAt.IsZero() {
		t.Fatal("discarded window retained notification deadlines")
	}
}
