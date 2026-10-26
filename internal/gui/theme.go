package gui

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/theme"
)

// Content sits on a plain canvas; the sidebar and sunken areas carry the tint.
// Onevra keeps its controls, typography scaling, accessibility, and native
// appearance handling.
func palette(c *ui.Context) *ui.Theme {
	t := theme.Onevra(c)
	if t.Dark {
		t.Background = ui.Hex("#17181b")
		t.Surface = ui.Hex("#212226")
		t.SurfaceHover = ui.Hex("#2a2b30")
		t.SurfacePressed = ui.Hex("#33343a")
		t.Border = ui.Hex("#2c2e33")
		t.Text = ui.Hex("#ecedf0")
		t.TextMuted = ui.Hex("#a3a6ae")
		t.Accent = ui.Hex("#7ab6ea")
		t.AccentText = ui.Hex("#0d2236")
		t.Success = ui.Hex("#5acf94")
		t.Danger = ui.Hex("#f28b82")
	} else {
		t.Background = ui.Hex("#ffffff")
		t.Surface = ui.Hex("#ffffff")
		t.SurfaceHover = ui.Hex("#f3f4f6")
		t.SurfacePressed = ui.Hex("#eaebee")
		t.Border = ui.Hex("#e6e7eb")
		t.Text = ui.Hex("#16171b")
		t.TextMuted = ui.Hex("#5f626a")
		t.Accent = ui.Hex("#1f6fb2")
		t.AccentText = ui.Hex("#ffffff")
		t.Success = ui.Hex("#137845")
		t.Danger = ui.Hex("#bf3a31")
	}
	t.AccentHover = t.Accent.Mix(t.Text, .10)
	t.AccentPressed = t.Accent.Mix(t.Text, .20)
	t.Selection = t.Accent.Alpha(.18)
	t.Focus = t.Accent.Alpha(.65)
	t.Radius = 7
	if c.Preferences().HighContrast {
		t.Border = t.Border.Mix(t.Text, .45)
		t.TextMuted = t.TextMuted.Mix(t.Text, .4)
	}
	return t
}

// sidebarColor tints navigation apart from the content canvas.
func sidebarColor(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.Hex("#1d1e22")
	}
	return ui.Hex("#f3f4f6")
}

// sunkenColor fills drop zones, chips and other recessed areas.
func sunkenColor(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.Hex("#141518")
	}
	return ui.Hex("#f5f6f8")
}

// controlBorder outlines buttons and inputs a step stronger than dividers.
func controlBorder(t *ui.Theme) ui.Color { return t.Border.Mix(t.TextMuted, .18) }

func accentSoft(t *ui.Theme) ui.Color  { return t.Accent.Alpha(.13) }
func successSoft(t *ui.Theme) ui.Color { return t.Success.Alpha(.12) }
func dangerSoft(t *ui.Theme) ui.Color  { return t.Danger.Alpha(.11) }

// profileColors give each profile a stable avatar tint derived from its id.
var profileColors = []string{"#1f6fb2", "#2e8a6b", "#6b5bb8", "#8a5a2e", "#b0466e", "#3d7a8c"}

func profileColor(id string) ui.Color {
	sum := 0
	for _, r := range id {
		sum = sum*31 + int(r)
	}
	if sum < 0 {
		sum = -sum
	}
	return ui.Hex(profileColors[sum%len(profileColors)])
}

// profileInitial is the first visible character of a profile name.
func profileInitial(name string) string {
	for _, r := range name {
		if r != ' ' {
			return string(r)
		}
	}
	return "?"
}
