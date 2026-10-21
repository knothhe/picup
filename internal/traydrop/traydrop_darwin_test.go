//go:build darwin

package traydrop

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

// PICUP_NATIVE_TRAY_TEST=1 go test ./internal/traydrop runs a real status
// item in the desktop session. The normal suite needs no native app loop.
func TestMain(m *testing.M) {
	if os.Getenv("PICUP_NATIVE_TRAY_TEST") != "1" {
		os.Exit(m.Run())
	}
	runtime.LockOSThread()
	code := 1
	mygo.App.WhenReady(func() {
		go func() {
			code = m.Run()
			mygo.App.Quit()
		}()
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}

func TestNativeTrayWindowForwardsFileDrops(t *testing.T) {
	if os.Getenv("PICUP_NATIVE_TRAY_TEST") != "1" {
		t.Skip("requires an AppKit desktop session")
	}
	// Two live trays prove registration targets this exact item, without
	// relying on the app having only one status window.
	prefix := fmt.Sprintf("Picup test %d", markerSerial.Add(1))
	firstTip, secondTip := prefix+" first", prefix+" second"
	first, err := mygo.NewTray(mygo.TrayOptions{Title: "Picup test 1", ToolTip: firstTip})
	if err != nil {
		t.Fatal(err)
	}
	second, err := mygo.NewTray(mygo.TrayOptions{Title: "Picup test 2", ToolTip: secondTip})
	if err != nil {
		first.Destroy()
		t.Fatal(err)
	}
	defer first.Destroy()
	defer second.Destroy()
	available, clicks := true, 0
	var drops [][]string
	first.OnClick(func() { clicks++ })
	stop, err := Register(first, firstTip, func() bool { return available }, func(paths []string) { drops = append(drops, paths) })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	photo := filepath.Join(t.TempDir(), "照片 空格.PNG")
	if err := os.WriteFile(photo, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	mygo.RunOnMain(func() {
		button := markedButton(firstTip)
		window := send(button, "window")
		if window == 0 || send(window, "delegate") == 0 || send(send(markedButton(secondTip), "window"), "delegate") != 0 {
			t.Error("registration did not isolate the selected status window")
			return
		}
		if send(send(window, "registeredDraggedTypes"), "containsObject:", nsString("public.file-url")) == 0 {
			t.Error("status window did not register file URL drops")
		}
		pasteboard := send(objc.ID(objc.GetClass("NSPasteboard")), "pasteboardWithUniqueName")
		defer send(pasteboard, "releaseGlobally")
		url := send(objc.ID(objc.GetClass("NSURL")), "fileURLWithPath:", nsString(photo))
		send(pasteboard, "writeObjects:", array(url))
		// A genuine NSPasteboard and an NSDraggingInfo stand-in drive the
		// window receiver, so AppKit itself must forward to our delegate.
		infoClass, err := objc.RegisterClass(fmt.Sprintf("PicupTestTrayDraggingInfo%d", markerSerial.Add(1)), objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
			{Cmd: objc.RegisterName("draggingPasteboard"), Fn: func(objc.ID, objc.SEL) objc.ID { return pasteboard }},
			{Cmd: objc.RegisterName("draggingSourceOperationMask"), Fn: func(objc.ID, objc.SEL) uintptr { return 1 }},
		})
		if err != nil {
			t.Error(err)
			return
		}
		info := send(send(objc.ID(infoClass), "alloc"), "init")
		defer send(info, "release")
		if send(window, "draggingEntered:", info) != 1 || send(window, "prepareForDragOperation:", info) == 0 || send(window, "performDragOperation:", info) == 0 {
			t.Error("AppKit status window did not accept and forward the image drop")
		}
		if len(drops) != 1 || !slices.Equal(drops[0], []string{photo}) {
			t.Errorf("drop did not preserve the local Unicode path: %q", drops)
		}
		available = false
		if send(window, "draggingUpdated:", info) != 0 || send(window, "prepareForDragOperation:", info) != 0 || send(window, "performDragOperation:", info) != 0 || len(drops) != 1 {
			t.Error("a busy application accepted a second batch")
		}
		available = true
		send(pasteboard, "clearContents")
		send(pasteboard, "setString:forType:", nsString("https://example.com/image.png"), nsString("public.utf8-plain-text"))
		if send(window, "draggingEntered:", info) != 0 || send(window, "performDragOperation:", info) != 0 || len(drops) != 1 {
			t.Error("a text or remote URL drag was treated as a local image")
		}
		send(button, "performClick:", objc.ID(0))
		if clicks != 1 {
			t.Error("drop registration changed the tray's click action")
		}
	})
	stop()
	stop() // cleanup is idempotent.
	mygo.RunOnMain(func() {
		if send(send(markedButton(firstTip), "window"), "delegate") != 0 || len(destinations) != 0 {
			t.Error("cleanup retained a delegate or callback")
		}
	})
	// Picup's existing quit listener removes the tray before this helper's
	// listener runs. Retained native objects must keep late cleanup safe.
	lateStop, err := Register(first, firstTip, func() bool { return true }, func([]string) {})
	if err != nil {
		t.Fatal(err)
	}
	first.Destroy()
	lateStop()
	mygo.RunOnMain(func() {
		if len(destinations) != 0 {
			t.Error("cleanup after tray destruction retained callbacks")
		}
	})
}
