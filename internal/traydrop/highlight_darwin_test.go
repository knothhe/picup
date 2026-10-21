//go:build darwin

package traydrop

import (
	"fmt"
	"os"
	"testing"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Posting both halves before handling the press also lets AppKit finish its
// normal tracking loop when this test is run with click interception disabled.
func nativeTrayClick(t *testing.T, button objc.ID, kinds ...uint) {
	t.Helper()
	app := send(objc.ID(objc.GetClass("NSApplication")), "sharedApplication")
	if len(kinds) == 0 {
		kinds = []uint{1, 2}
	}
	for _, kind := range kinds {
		send(app, "postEvent:atStart:", nativeTrayMouseEvent(t, button, kind, 0), false)
	}
	nativeTrayHandlePosted(t)
}

func nativeTrayMouseEvent(t *testing.T, button objc.ID, kind, flags uint) objc.ID {
	t.Helper()
	bounds := objc.Send[rect](button, objc.RegisterName("bounds"))
	location := point{bounds.Origin.X + bounds.Size.Width/2, bounds.Origin.Y + bounds.Size.Height/2}
	location = objc.Send[point](button, objc.RegisterName("convertPoint:toView:"), location, objc.ID(0))
	window := send(button, "window")
	lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		t.Fatal(err)
	}
	defer purego.Dlclose(lib)
	var mouseEvent func(objc.ID, objc.SEL, uint, point, uint, float64, int, objc.ID, int, int, float32) objc.ID
	purego.RegisterLibFunc(&mouseEvent, lib, "objc_msgSend")
	return mouseEvent(objc.ID(objc.GetClass("NSEvent")), objc.RegisterName("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
		kind, location, flags, 0, int(send(window, "windowNumber")), 0, 0, 1, 1)
}

func nativeTrayHandlePosted(t *testing.T) {
	t.Helper()
	app := send(objc.ID(objc.GetClass("NSApplication")), "sharedApplication")
	until := send(objc.ID(objc.GetClass("NSDate")), "distantPast")
	mode := nsString("kCFRunLoopDefaultMode")
	for i := 0; i < 1000; i++ {
		event := send(app, "nextEventMatchingMask:untilDate:inMode:dequeue:", ^uint64(0), until, mode, true)
		if event == 0 {
			return
		}
		send(app, "sendEvent:", event)
	}
	t.Error("native event queue did not drain")
}

// Count entry into AppKit's actual button tracking. A live monitor prevents
// it; removing that monitor must restore it for the retained status button.
func nativeTrayWatchPresses(t *testing.T, button objc.ID, presses *int) func() {
	t.Helper()
	probe, err := objc.RegisterClass(fmt.Sprintf("PicupTestStatusButton%d", markerSerial.Add(1)), button.Class(), nil, nil, []objc.MethodDef{
		{Cmd: objc.RegisterName("mouseDown:"), Fn: func(self objc.ID, selector objc.SEL, event objc.ID) {
			*presses++
			objc.SendSuper[objc.ID](self, selector, event)
		}},
	})
	if err != nil {
		t.Error(err)
		return func() {}
	}
	lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		t.Error(err)
		return func() {}
	}
	var setClass func(objc.ID, objc.Class) objc.Class
	purego.RegisterLibFunc(&setClass, lib, "object_setClass")
	original := setClass(button, probe)
	return func() { setClass(button, original); purego.Dlclose(lib) }
}

func TestNativeTrayPanelSelectionSurvivesMouseRelease(t *testing.T) {
	if os.Getenv("PICUP_NATIVE_TRAY_TEST") != "1" {
		t.Skip("requires an AppKit desktop session")
	}
	// Destroying the last test window must not stop TestMain's app loop.
	off := mygo.App.OnWindowAllClosed(func() {})
	defer off()
	tip := fmt.Sprintf("Picup selection test %d", markerSerial.Add(1))
	tray, err := mygo.NewTray(mygo.TrayOptions{Title: "Picup test", ToolTip: tip})
	if err != nil {
		t.Fatal(err)
	}
	defer tray.Destroy()
	panel := mygo.NewFlyout(mygo.FlyoutOptions{
		Tray: tray, Placement: mygo.PlacementBottom, Width: 120, Height: 80,
		Hidden: true, Focusable: true, Content: ui.View(func(*ui.Context) {}),
	})
	defer panel.Destroy()
	panel.OnShow(func() { SetPanelVisible(tray, true) })
	panel.OnHide(func() { SetPanelVisible(tray, false) })
	panel.OnClosed(func() { SetPanelVisible(tray, false) })
	panel.OnClose(func(e *mygo.CloseEvent) { e.PreventDefault(); panel.Hide() })
	clicks, rightClicks := 0, 0
	toggle := func() {
		if panel.IsVisible() {
			panel.Hide()
		} else {
			panel.Show()
			panel.Focus()
		}
	}
	tray.OnClick(func() { clicks++; toggle() })
	rightClick := func() { rightClicks++; toggle() }
	tray.OnRightClick(rightClick)
	stop, err := Register(tray, tip, func() bool { return true }, func([]string) {}, rightClick)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	other := mygo.NewWindow(mygo.WindowOptions{Title: "Picup focus test", Width: 120, Height: 80, Hidden: true, Content: ui.View(func(*ui.Context) {})})
	defer other.Destroy()
	mygo.RunOnMain(func() {
		button := markedButton(tip)
		nativeTrayClick(t, button)
		if clicks != 1 || !panel.IsVisible() {
			t.Errorf("tray click did not open one panel: clicks=%d visible=%v", clicks, panel.IsVisible())
			return
		}
		if send(button, "isHighlighted") == 0 {
			t.Error("mouse release cleared the tray background while its panel remained visible")
		}
		nativeTrayClick(t, button)
		if clicks != 2 || panel.IsVisible() || send(button, "isHighlighted") != 0 {
			t.Error("second tray click did not hide the panel and clear its selection")
		}
		nativeTrayClick(t, button, 3, 4)
		if rightClicks != 1 || clicks != 2 || !panel.IsVisible() || send(button, "isHighlighted") == 0 {
			t.Error("right mouse release did not preserve selection on the opened panel")
		}
		nativeTrayClick(t, button, 3, 4)
		if rightClicks != 2 || panel.IsVisible() || send(button, "isHighlighted") != 0 {
			t.Error("second right click did not hide the panel and clear its selection")
		}
		panel.Show()
		panel.Focus()
		other.Show()
		other.Focus()
	})
	// Focus loss posts dismissal to the main queue after AppKit changes key
	// windows. This following dispatch runs after that dismissal.
	mygo.RunOnMain(func() {
		button := markedButton(tip)
		if panel.IsVisible() || send(button, "isHighlighted") != 0 {
			t.Error("focus loss did not hide the panel and clear its tray selection")
		}
		panel.Show()
		panel.Destroy()
		if send(button, "isHighlighted") != 0 {
			t.Error("destroyed panel retained the tray selection")
		}
	})
}

func TestNativeTraySelectionIsIsolatedAndSurvivesDragExit(t *testing.T) {
	if os.Getenv("PICUP_NATIVE_TRAY_TEST") != "1" {
		t.Skip("requires an AppKit desktop session")
	}
	prefix := fmt.Sprintf("Picup selection isolation %d", markerSerial.Add(1))
	first, err := mygo.NewTray(mygo.TrayOptions{Title: "Picup test 1", ToolTip: prefix + " first"})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Destroy()
	second, err := mygo.NewTray(mygo.TrayOptions{Title: "Picup test 2", ToolTip: prefix + " second"})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Destroy()
	stop, err := Register(first, prefix+" first", func() bool { return true }, func([]string) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	clicks := 0
	first.OnClick(func() { clicks++ })
	mygo.RunOnMain(func() {
		button, other := markedButton(prefix+" first"), markedButton(prefix+" second")
		window := send(button, "window")
		d := destinations[send(window, "delegate")]
		SetPanelVisible(first, true)
		if send(button, "isHighlighted") == 0 || send(other, "isHighlighted") != 0 {
			t.Error("panel selection did not isolate the requested tray")
		}
		for _, selector := range []string{"draggingExited:", "draggingEnded:", "concludeDragOperation:"} {
			d.highlight(true)
			send(d.delegate, selector, objc.ID(0))
			if send(button, "isHighlighted") == 0 {
				t.Errorf("%s cleared the background of an open panel", selector)
			}
		}
		SetPanelVisible(first, false)
		if send(button, "isHighlighted") != 0 {
			t.Error("hidden panel retained its selection after dragging ended")
		}
		commandPress := nativeTrayMouseEvent(t, button, 1, 1<<20)
		if d.handleClick(commandPress) != commandPress || clicks != 0 {
			t.Error("Command press was consumed instead of allowing system status item movement")
		}
		otherPress := nativeTrayMouseEvent(t, other, 1, 0)
		if d.handleClick(otherPress) != otherPress || clicks != 0 {
			t.Error("another tray's press dispatched this tray's action")
		}
		presses := 0
		restore := nativeTrayWatchPresses(t, button, &presses)
		defer restore()
		nativeTrayClick(t, button)
		if presses != 0 || clicks != 1 {
			t.Error("registered click entered AppKit's transient button tracking")
		}
		SetPanelVisible(first, true)
		stop()
		if send(button, "isHighlighted") != 0 {
			t.Error("cleanup retained the panel's tray selection")
		}
		nativeTrayClick(t, button)
		if presses != 1 || clicks != 2 {
			t.Error("cleanup did not remove the monitor and restore native button tracking")
		}
	})
}
