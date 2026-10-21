//go:build darwin

package traydrop

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

// AppKit's NSWindow drag implementation forwards these methods to its
// delegate. Registering the status item's own window avoids replacing the
// system button or changing its normal left/right-click handling.
// https://developer.apple.com/documentation/appkit/nswindow/registerfordraggedtypes(_:)
type destination struct {
	window, button, delegate objc.ID
	available                func() bool
	drop                     func([]string)
}

var (
	destinationClass objc.Class
	classOnce        sync.Once
	classError       error
	markerSerial     atomic.Uint64
	// All accesses, including AppKit callbacks, belong to the main UI thread.
	destinations = map[objc.ID]*destination{}
)

func send(object objc.ID, selector string, args ...any) objc.ID {
	return object.Send(objc.RegisterName(selector), args...)
}

func nsString(value string) objc.ID {
	return send(objc.ID(objc.GetClass("NSString")), "stringWithUTF8String:", value)
}

func goString(value objc.ID) string {
	if value == 0 {
		return ""
	}
	return objc.Send[string](value, objc.RegisterName("UTF8String"))
}

func array(item objc.ID) objc.ID {
	return send(objc.ID(objc.GetClass("NSArray")), "arrayWithObject:", item)
}

func findButton(view objc.ID, marker string) objc.ID {
	if view == 0 {
		return 0
	}
	if send(view, "isKindOfClass:", objc.GetClass("NSStatusBarButton")) != 0 && goString(send(view, "toolTip")) == marker {
		return view
	}
	children := send(view, "subviews")
	for i := 0; i < int(send(children, "count")); i++ {
		if button := findButton(send(children, "objectAtIndex:", i), marker); button != 0 {
			return button
		}
	}
	return 0
}

func markedButton(marker string) objc.ID {
	app := send(objc.ID(objc.GetClass("NSApplication")), "sharedApplication")
	windows := send(app, "windows")
	for i := 0; i < int(send(windows, "count")); i++ {
		window := send(windows, "objectAtIndex:", i)
		if button := findButton(send(window, "contentView"), marker); button != 0 {
			return button
		}
	}
	return 0
}

func (d *destination) highlight(on bool) {
	send(d.button, "highlight:", on)
}

func (d *destination) paths(info objc.ID) []string {
	if d == nil || !d.available() || send(info, "draggingSourceOperationMask")&1 == 0 {
		return nil // NSDragOperationCopy is the only operation uploads support.
	}
	options := send(objc.ID(objc.GetClass("NSDictionary")), "dictionaryWithObject:forKey:",
		send(objc.ID(objc.GetClass("NSNumber")), "numberWithBool:", true), nsString("NSPasteboardURLReadingFileURLsOnlyKey"))
	urls := send(send(info, "draggingPasteboard"), "readObjectsForClasses:options:", array(objc.ID(objc.GetClass("NSURL"))), options)
	var paths []string
	for i := 0; i < int(send(urls, "count")); i++ {
		url := send(urls, "objectAtIndex:", i)
		if send(url, "isFileURL") != 0 {
			paths = append(paths, goString(send(send(url, "filePathURL"), "path")))
		}
	}
	return imageFiles(paths)
}

func registerClass() {
	classOnce.Do(func() {
		dragged := func(self objc.ID, _ objc.SEL, info objc.ID) uintptr {
			d := destinations[self]
			if d == nil {
				return 0
			}
			accepted := len(d.paths(info)) != 0
			d.highlight(accepted)
			if accepted {
				return 1 // NSDragOperationCopy
			}
			return 0
		}
		clear := func(self objc.ID, _ objc.SEL, _ objc.ID) {
			if d := destinations[self]; d != nil {
				d.highlight(false)
			}
		}
		destinationClass, classError = objc.RegisterClass("PicupTrayDropDestination", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
			{Cmd: objc.RegisterName("draggingEntered:"), Fn: dragged},
			{Cmd: objc.RegisterName("draggingUpdated:"), Fn: dragged},
			{Cmd: objc.RegisterName("draggingExited:"), Fn: clear},
			{Cmd: objc.RegisterName("draggingEnded:"), Fn: clear},
			{Cmd: objc.RegisterName("concludeDragOperation:"), Fn: clear},
			{Cmd: objc.RegisterName("prepareForDragOperation:"), Fn: func(self objc.ID, _ objc.SEL, info objc.ID) bool {
				return len(destinations[self].paths(info)) != 0
			}},
			{Cmd: objc.RegisterName("performDragOperation:"), Fn: func(self objc.ID, _ objc.SEL, info objc.ID) bool {
				d := destinations[self]
				if d == nil {
					return false
				}
				d.highlight(false)
				paths := d.paths(info)
				if len(paths) == 0 {
					return false
				}
				d.drop(paths)
				return true
			}},
		})
	})
}

// Register accepts local image files dropped on this tray icon. available
// and dropped run on the main UI thread; cleanup may run from any goroutine.
// toolTip is restored after a unique temporary marker identifies this exact
// MyGo tray's public NSStatusBarButton in the native view tree.
func Register(tray *mygo.Tray, toolTip string, available func() bool, dropped func([]string)) (func(), error) {
	if tray == nil || available == nil || dropped == nil {
		return nil, errors.New("tray drop: missing tray or callback")
	}
	var d *destination
	var err error
	mygo.RunOnMain(func() {
		registerClass()
		if classError != nil {
			err = classError
			return
		}
		marker := fmt.Sprintf("Picup tray drop %d", markerSerial.Add(1))
		tray.SetToolTip(marker)
		button := markedButton(marker)
		tray.SetToolTip(toolTip)
		if button == 0 {
			err = errors.New("tray drop: status button unavailable")
			return
		}
		window := send(button, "window")
		if window == 0 || send(window, "delegate") != 0 {
			err = errors.New("tray drop: status window unavailable or already has a delegate")
			return
		}
		d = &destination{window: send(window, "retain"), button: send(button, "retain"),
			delegate: send(send(objc.ID(destinationClass), "alloc"), "init"), available: available, drop: dropped}
		destinations[d.delegate] = d
		send(window, "setDelegate:", d.delegate)
		send(window, "registerForDraggedTypes:", array(nsString("public.file-url")))
	})
	if err != nil {
		return nil, err
	}
	return func() {
		mygo.RunOnMain(func() {
			if d == nil {
				return
			}
			d.highlight(false)
			send(d.window, "unregisterDraggedTypes")
			if send(d.window, "delegate") == d.delegate {
				send(d.window, "setDelegate:", objc.ID(0))
			}
			delete(destinations, d.delegate)
			send(d.delegate, "release")
			send(d.button, "release")
			send(d.window, "release")
			d = nil
		})
	}, nil
}
