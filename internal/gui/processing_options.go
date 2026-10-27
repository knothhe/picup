package gui

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// processingValues are the options a page edits, in the units it saves.
type processingValues struct {
	compress          bool
	format            string
	quality, maxWidth int
}

// qualityApplies reports whether the encoder reads quality for v.
func (v processingValues) qualityApplies() bool {
	return v.format != "png" && !(v.format == "original" && !v.compress)
}

// unprocessed reports whether v leaves images exactly as they are.
func (v processingValues) unprocessed() bool {
	return !v.compress && v.format == "original" && v.maxWidth == 0
}

type processingPreset struct {
	id, name, description string
	values                processingValues
}

// Presets cover the common cases so most changes need no editor. The default
// options are the standard preset.
var processingPresets = []processingPreset{
	{"original", "原图", "不压缩、不转换、不缩放", processingValues{false, "original", 85, 0}},
	{"standard", "标准", "保留格式压缩，质量 85", processingValues{true, "original", 85, 0}},
	{"balanced", "均衡", "转为 WebP，质量 85", processingValues{true, "webp", 85, 0}},
	{"smallest", "体积优先", "转为 AVIF，质量 60，最宽 1920 px", processingValues{true, "avif", 60, 1920}},
}

// matchPreset returns the preset v is, or "custom". Quality only counts
// where the encoder reads it.
func matchPreset(v processingValues) string {
	for _, p := range processingPresets {
		w := p.values
		if v.compress == w.compress && v.format == w.format && v.maxWidth == w.maxWidth && (!w.qualityApplies() || v.quality == w.quality) {
			return p.id
		}
	}
	return "custom"
}

// processingPart is one value in the collapsed summary. Field names the
// editor control a summary chip opens, or is empty when no control fits.
type processingPart struct{ text, field string }

func processingSummaryParts(compress bool, format string, quality, maxWidth int) []processingPart {
	if (processingValues{compress, format, quality, maxWidth}).unprocessed() {
		return []processingPart{{tr("保持原图，不做处理"), "compress"}}
	}
	label := formatLabels[format]
	if label == "" {
		label = format
	}
	if format == "original" {
		label = tr("原格式")
	}
	parts := []processingPart{{label, "format"}}
	switch format {
	case "original":
		if compress {
			parts = append(parts, processingPart{tr("压缩"), "compress"})
		} else {
			parts = append(parts, processingPart{tr("不压缩"), "compress"})
		}
	case "png":
		parts = append(parts, processingPart{tr("无损"), "format"})
	case "jpeg", "webp", "avif":
		parts = append(parts, processingPart{tf("质量 %d", quality), "quality"})
	}
	if maxWidth > 0 {
		parts = append(parts, processingPart{tf("最宽 %d px", maxWidth), "max-width"})
	}
	return parts
}

func processingSummary(compress bool, format string, quality, maxWidth int) string {
	var texts []string
	for _, part := range processingSummaryParts(compress, format, quality, maxWidth) {
		texts = append(texts, part.text)
	}
	return strings.Join(texts, " · ")
}

var processingChipLabels = map[string]string{
	"format":    "修改输出格式",
	"compress":  "修改压缩设置",
	"quality":   "修改压缩质量",
	"max-width": "修改最大宽度",
}

const processingSavedFlash = 2 * time.Second

// processingDisclosureState is one page's presentation of its options. Only
// expanded is remembered; focus is a one-shot request from a summary chip.
// The rest follows saves so the header can confirm an edit was kept.
type processingDisclosureState struct {
	expanded bool
	focus    string

	seeded      bool
	lastSaved   processingValues
	lastPending bool
	savedAt     time.Time
}

// noteSave records a save of an edit: the saved values moved to what the
// editor held after a frame in which the two differed. A profile switch or a
// discarded edit moves both together, or neither, and shows nothing.
func (s *processingDisclosureState) noteSave(now time.Time, current, saved processingValues) {
	if s.seeded && saved != s.lastSaved && s.lastPending && current == saved {
		s.savedAt = now
	}
	s.seeded, s.lastSaved, s.lastPending = true, saved, current != saved
}

// processingEditor is what a page hands the shared options disclosure.
type processingEditor struct {
	key               string
	state             *processingDisclosureState
	compress          *bool
	format            *string
	quality, maxWidth *float64
	// shown are the values the summary describes; saved are those on disk.
	shown, saved processingValues
	// stacked puts the format above the numeric fields.
	stacked bool
	locked  bool
	// commit saves at once after a preset is chosen, reporting success.
	commit func() bool
	// savedNotice confirms saves in the header. Uploads leave this to the
	// app's settings toast so one save is not announced twice.
	savedNotice bool
}

func (e processingEditor) current() processingValues {
	return processingValues{*e.compress, *e.format, int(*e.quality), int(*e.maxWidth)}
}

func (e processingEditor) apply(v processingValues) {
	*e.compress, *e.format, *e.maxWidth = v.compress, v.format, float64(v.maxWidth)
	if v.qualityApplies() {
		*e.quality = float64(v.quality)
	}
}

// Disclosure changes only presentation. Parameter values and their save state
// belong to the page, so folding the editor never loses or commits an edit;
// choosing a preset is an edit and saves at once.
// While locked, an open editor may still be closed but a closed one stays shut.
func (a *application) processingDisclosure(c *ui.Context, e processingEditor) (editorFocused bool) {
	t, state, locked := c.Theme(), e.state, e.locked
	now := c.Now()
	state.noteSave(now, e.current(), e.saved)
	saved := e.savedNotice && !state.savedAt.IsZero() && now.Sub(state.savedAt) < processingSavedFlash
	if saved {
		c.After(processingSavedFlash - now.Sub(state.savedAt))
	}
	canToggle := state.expanded || !locked
	open := func(expanded bool, focus string) {
		state.expanded, state.focus = expanded, focus
		a.saveWorkspacePreferences()
		c.After(0)
	}
	ui.Column(c).Key(e.key+"-disclosure").FillWidth().MinWidth(0).Shrink(0).Radius(10).Border(1, t.Border).Background(t.Surface).Clip().Children(func() {
		// The whole row toggles for pointers; the named button, preset and
		// chips inside it are the keyboard and assistive targets.
		header := ui.Row(c).Key("processing-header").FillWidth().MinWidth(0).Height(44).Padding(0, 14, 0, 0).Gap(8).AlignItems(ui.Center)
		if canToggle {
			if header.Hovered() {
				header.Background(sunkenColor(t))
			}
			if header.Clicked() {
				open(!state.expanded, "")
			}
		}
		header.Children(func() {
			label := "展开处理选项"
			if state.expanded {
				label = "收起处理选项"
			}
			toggle := ui.ButtonBase(c).Key("processing-toggle").Shrink(0).Height(44).Padding(0, 4, 0, 14).Gap(9).AlignItems(ui.Center).Label(tr(label)).Expanded(state.expanded).Background(ui.Color{}).Disabled(!canToggle)
			if locked {
				toggle.Tooltip(tr("处理中，参数已锁定"))
			}
			toggle.Children(func() {
				components.Icon(c, lucide.SlidersHorizontal()).Size(15, 15).Shrink(0).TextColor(t.TextMuted)
				components.Text(c, tr("处理选项")).FontSize(12.5).FontWeight(500).SingleLine().Shrink(0)
			})
			if toggle.Clicked() {
				open(!state.expanded, "")
			}
			if state.expanded {
				ui.Spacer(c)
				if !saved && !locked {
					components.Text(c, tr("参数自动记忆")).FontSize(11.5).TextColor(t.TextMuted).SingleLine().Shrink(0)
				}
			} else {
				ui.Row(c).Key("processing-summary").Grow(1).Shrink(1).MinWidth(0).Gap(6).AlignItems(ui.Center).Clip().Children(func() {
					if a.processingPresetChip(c, e) && e.commit != nil && e.commit() {
						state.savedAt = c.Now()
						c.After(0)
					}
					unprocessed := e.shown.unprocessed()
					for _, part := range processingSummaryParts(e.shown.compress, e.shown.format, e.shown.quality, e.shown.maxWidth) {
						processingChip(c, part, locked, unprocessed, func() { open(true, part.field) })
					}
				})
			}
			if saved {
				ui.Row(c).Key("processing-saved").Shrink(0).Gap(4).AlignItems(ui.Center).Children(func() {
					components.Icon(c, lucide.Check()).Size(13, 13).TextColor(t.Success)
					components.Text(c, tr("已保存")).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
				})
			}
			if locked {
				ui.Row(c).Shrink(0).Gap(4).AlignItems(ui.Center).Tooltip(tr("处理中，参数已锁定")).Children(func() {
					components.Icon(c, lucide.Lock()).Size(13, 13).TextColor(t.TextMuted)
					components.Text(c, tr("已锁定")).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
				})
			}
			if canToggle {
				chevron := lucide.ChevronDown()
				if state.expanded {
					chevron = lucide.ChevronUp()
				}
				components.Icon(c, chevron).Size(14, 14).Shrink(0).TextColor(t.TextMuted)
			}
		})
		if state.expanded {
			ui.Column(c).FillWidth().MinWidth(0).BorderWidth(1, 0, 0, 0).BorderColor(t.Border.Alpha(.65)).Children(func() {
				editorFocused = processingOptions(c, e.key, &state.focus, e.compress, e.format, e.quality, e.maxWidth, e.stacked, locked)
			})
		}
	})
	return editorFocused
}

// processingPresetChip names the preset the shown values match and offers
// the others. It reports whether a preset was just applied to the editor.
func (a *application) processingPresetChip(c *ui.Context, e processingEditor) (applied bool) {
	t := c.Theme()
	current := matchPreset(e.shown)
	name := tr("自定义")
	for _, p := range processingPresets {
		if p.id == current {
			name = tr(p.name)
		}
	}
	if e.locked {
		ui.Row(c).Key("preset").Shrink(0).Height(24).Padding(0, 8).Radius(6).AlignItems(ui.Center).Border(1, t.Border).Children(func() {
			components.Text(c, name).FontSize(12).FontWeight(500).TextColor(t.TextMuted).SingleLine()
		})
		return false
	}
	choice := current
	s := components.Select(c.Key("preset"), &choice)
	s.Trigger.Shrink(0).Height(24).MinHeight(24).Padding(0, 20, 0, 8).Radius(6).Label(tr("处理预设")).Tooltip(tr("处理预设")).Children(func() {
		components.Text(c, name).FontSize(12).FontWeight(500).SingleLine()
	})
	s.Popup(func(ui.Element) {
		for _, p := range processingPresets {
			s.Item(p.id).Label(tr(p.name)).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					components.Text(c, tr(p.name)).FontSize(12).FontWeight(500)
					components.Text(c, tr(p.description)).FontSize(11).TextColor(t.TextMuted)
				})
			})
		}
	})
	if choice == current {
		return false
	}
	for _, p := range processingPresets {
		if p.id == choice {
			e.apply(p.values)
		}
	}
	return true
}

// processingChip shows one summary value. Clicking it opens the editor at the
// matching control; while locked it is only a label.
func processingChip(c *ui.Context, part processingPart, locked, unprocessed bool, open func()) {
	t := c.Theme()
	var chip ui.Element
	if locked {
		chip = ui.Row(c)
	} else {
		chip = ui.ButtonBase(c).Label(tr(processingChipLabels[part.field])).Tooltip(tr(processingChipLabels[part.field]))
	}
	chip.Key("chip-"+part.text).Shrink(1).MinWidth(0).Height(24).Padding(0, 8).Radius(6).AlignItems(ui.Center)
	if !unprocessed {
		// A summary that changes nothing reads as plain text, not a value.
		chip.Background(sunkenColor(t))
	}
	text := t.TextMuted
	if !locked && (chip.Hovered() || chip.FocusVisible()) {
		chip.Background(accentSoft(t))
		text = t.Accent
	}
	chip.Children(func() {
		components.Text(c, part.text).FontSize(12).TextColor(text).SingleLine().Ellipsis("…").MinWidth(0)
	})
	if !locked && chip.Clicked() {
		open()
	}
}

// processingOptions edits only the values owned by its caller. Uploads keep
// these in the active profile; local image tools have their own preferences.
// Disabling compression still permits a requested conversion or resize.
func processingOptions(c *ui.Context, key string, focus *string, compress *bool, format *string, quality, maxWidth *float64, stacked, disabled bool) (editorFocused bool) {
	t := c.Theme()
	ui.Column(c).Key(key).FillWidth().MinWidth(0).Shrink(0).Padding(12, 14).Gap(10).Disabled(disabled).Children(func() {
		components.Switch(c, compress, tr("压缩图片"))
		formatField := func() {
			ui.Column(c).Key("format").Grow(1.4).MinWidth(0).Gap(5).Children(func() {
				components.Text(c, tr("输出格式")).FontSize(12).TextColor(t.TextMuted)
				trigger := selectControl(c, "图片格式", format, []string{"original", "webp", "avif", "jpeg", "png"}, []string{"保留原格式", "WebP", "AVIF", "JPEG", "PNG"})
				requestFocus(focus, "format", trigger.FocusWithin(), func() { trigger.AutoFocus() })
			})
		}
		numericFields := func() {
			ui.Column(c).Key("quality").Grow(1).MinWidth(0).Gap(5).Disabled(*format == "png").Children(func() {
				components.Text(c, tr("质量")).FontSize(12).TextColor(t.TextMuted)
				input := components.NumberInput(c, quality, 1, 100, 1).Label(tr("压缩质量")).FillWidth().MinWidth(0)
				requestFocus(focus, "quality", input.FocusWithin(), func() { input.AutoFocus() })
				editorFocused = editorFocused || input.FocusWithin() && !input.IsDisabled()
			})
			ui.Column(c).Key("max-width").Grow(1).MinWidth(0).Gap(5).Children(func() {
				components.Text(c, tr("最大宽度（px）")).FontSize(12).TextColor(t.TextMuted)
				input := components.NumberInput(c, maxWidth, 0, 32768, 1).Label(tr("最大宽度")).FillWidth().MinWidth(0)
				requestFocus(focus, "max-width", input.FocusWithin(), func() { input.AutoFocus() })
				editorFocused = editorFocused || input.FocusWithin() && !input.IsDisabled()
			})
		}
		if stacked {
			ui.Row(c).FillWidth().MinWidth(0).Children(formatField)
			ui.Row(c).FillWidth().MinWidth(0).Gap(12).Children(numericFields)
		} else {
			ui.Row(c).FillWidth().MinWidth(0).Gap(12).Children(func() {
				formatField()
				numericFields()
			})
		}
		components.Text(c, tr("PNG 无损优化；动画保持原样；HEIC 默认转为 WebP；宽度 0 为不限，只缩小不放大。")).FillWidth().MinWidth(0).Wrap().FontSize(11).TextColor(t.TextMuted)
	})
	if *focus == "compress" {
		*focus = "" // The switch exposes no focus handle; opening is enough.
	}
	return editorFocused
}

// requestFocus moves the focus into control once. AutoFocus acts only in the
// frame a control appears, and the page may rebuild the editor in a new
// container right after opening it, so the request lasts until it lands.
func requestFocus(focus *string, field string, focused bool, autoFocus func()) {
	if *focus != field {
		return
	}
	if focused {
		*focus = ""
	} else {
		autoFocus()
	}
}
