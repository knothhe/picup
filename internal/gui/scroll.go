package gui

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

// MyGo paints scrollbars over the viewport, including a wider drag target.
// Reserve a stable lane so both the thumb and its hit area clear the content.
func scrollbarGutter(c *ui.Context) float32 {
	return max(float32(6), c.Theme().ScrollbarWidth) + 12
}

func scrollView(c *ui.Context) ui.Element {
	return components.ScrollView(c, components.ScrollViewOptions{}).Padding(0, scrollbarGutter(c), 0, 0)
}
