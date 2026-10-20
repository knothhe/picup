package gui

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/theme"
)

// Pair Magpie's quiet surfaces with Picup's ocean blue while retaining Onevra's
// controls, typography scaling, accessibility, and native appearance handling.
func palette(c *ui.Context) *ui.Theme {
	t := theme.Onevra(c)
	if t.Dark {
		t.Background = ui.Hex("#1a1a1e")
		t.Surface = ui.Hex("#232327")
		t.SurfaceHover = ui.Hex("#2d2d33")
		t.SurfacePressed = ui.Hex("#36363d")
		t.Border = ui.Hex("#333339")
		t.Text = ui.Hex("#ededf1")
		t.TextMuted = ui.Hex("#a1a1aa")
		t.Accent = ui.Hex("#8dc5ed")
		t.AccentText = ui.Hex("#102a3d")
		t.Success = ui.Hex("#6fd99b")
		t.Danger = ui.Hex("#f28b82")
	} else {
		t.Background = ui.Hex("#f4f4f6")
		t.Surface = ui.Hex("#ffffff")
		t.SurfaceHover = ui.Hex("#f1f1f4")
		t.SurfacePressed = ui.Hex("#e9e9ee")
		t.Border = ui.Hex("#e3e3e8")
		t.Text = ui.Hex("#1c1c21")
		t.TextMuted = ui.Hex("#63636b")
		t.Accent = ui.Hex("#2676af")
		t.AccentText = ui.Hex("#ffffff")
		t.Success = ui.Hex("#189a58")
		t.Danger = ui.Hex("#d64545")
	}
	t.AccentHover = t.Accent.Mix(t.Text, .10)
	t.AccentPressed = t.Accent.Mix(t.Text, .20)
	t.Selection = t.Accent.Alpha(.18)
	t.Focus = t.Accent.Alpha(.65)
	t.Radius = 10
	if c.Preferences().HighContrast {
		t.Border = t.Border.Mix(t.Text, .45)
		t.TextMuted = t.TextMuted.Mix(t.Text, .4)
	}
	return t
}
