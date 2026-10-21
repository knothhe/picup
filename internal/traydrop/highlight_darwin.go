//go:build darwin

package traydrop

import (
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

type point struct{ X, Y float64 }
type size struct{ Width, Height float64 }
type rect struct {
	Origin point
	Size   size
}

// SetPanelVisible keeps the menu bar item selected while its panel is open.
// Drag feedback shares the selection, so ending a drag cannot clear it.
func SetPanelVisible(tray *mygo.Tray, visible bool) {
	mygo.RunOnMain(func() {
		for _, d := range destinations {
			if d.tray == tray {
				d.panelVisible = visible
				d.updateHighlight()
			}
		}
	})
}

func (d *destination) updateHighlight() {
	send(d.button, "highlight:", d.panelVisible || d.dragging)
}

func (d *destination) watchClicks() {
	block := objc.NewBlock(func(_ objc.Block, event objc.ID) objc.ID {
		return d.handleClick(event)
	})
	defer block.Release()
	d.monitor = send(send(objc.ID(objc.GetClass("NSEvent")), "addLocalMonitorForEventsMatchingMask:handler:",
		uintptr(1<<1|1<<3), block), "retain") // left and right mouse down
}

func (d *destination) handleClick(event objc.ID) objc.ID {
	// Leave Command presses to AppKit so the user can move the menu bar item.
	if send(event, "window") != d.window || send(event, "modifierFlags")&(1<<20) != 0 {
		return event
	}
	location := objc.Send[point](event, objc.RegisterName("locationInWindow"))
	location = objc.Send[point](d.button, objc.RegisterName("convertPoint:fromView:"), location, objc.ID(0))
	bounds := objc.Send[rect](d.button, objc.RegisterName("bounds"))
	if location.X < bounds.Origin.X || location.Y < bounds.Origin.Y ||
		location.X >= bounds.Origin.X+bounds.Size.Width || location.Y >= bounds.Origin.Y+bounds.Size.Height {
		return event
	}
	// Dispatch before NSStatusBarButton starts tracking. Its mouse-up cleanup
	// would otherwise clear the panel's selection and cause a visible flash.
	if send(event, "type") == 3 { // NSEventTypeRightMouseDown
		if d.rightClick == nil {
			return event
		}
		// MyGo's action recognizes only right mouse-up, so a right press
		// must invoke the same application callback directly.
		d.rightClick()
		return 0
	}
	app := send(objc.ID(objc.GetClass("NSApplication")), "sharedApplication")
	send(app, "sendAction:to:from:", send(d.button, "action"), send(d.button, "target"), d.button)
	return 0
}
