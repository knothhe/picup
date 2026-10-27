package gui

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

func processingSummary(compress bool, format string, quality, maxWidth int) string {
	label := formatLabels[format]
	if label == "" {
		label = format
	}
	if format == "original" {
		label = tr("原格式")
	}
	parts := []string{label}
	switch format {
	case "original":
		if compress {
			parts = append(parts, tr("压缩"))
		} else {
			parts = append(parts, tr("不压缩"))
		}
	case "png":
		parts = append(parts, tr("无损"))
	case "jpeg", "webp", "avif":
		parts = append(parts, tf("质量 %d", quality))
	}
	if maxWidth > 0 {
		parts = append(parts, tf("最宽 %d px", maxWidth))
	}
	return strings.Join(parts, " · ")
}

// Disclosure changes only presentation. Parameter values and their save state
// belong to the page, so folding the editor never loses or commits an edit.
func processingDisclosure(c *ui.Context, key string, expanded *bool, summary string, compress *bool, format *string, quality, maxWidth *float64, disabled bool) (editorFocused bool) {
	t := c.Theme()
	ui.Column(c).Key(key+"-disclosure").FillWidth().MinWidth(0).Shrink(0).Radius(10).Border(1, t.Border).Background(t.Surface).Clip().Children(func() {
		label, state, chevron := "展开处理选项", "展开", lucide.ChevronDown()
		if *expanded {
			label, state, chevron = "收起处理选项", "收起", lucide.ChevronUp()
		}
		header := ui.ButtonBase(c).Key("processing-toggle").FillWidth().MinWidth(0).Height(44).Padding(0, 14).Gap(9).AlignItems(ui.Center).Justify(ui.Start).Label(tr(label)).Expanded(*expanded).Background(ui.Color{})
		if header.Hovered() || header.FocusVisible() {
			header.Background(sunkenColor(t))
		}
		header.Children(func() {
			components.Icon(c, lucide.SlidersHorizontal()).Size(15, 15).Shrink(0).TextColor(t.TextMuted)
			components.Text(c, tr("处理选项")).FontSize(12.5).FontWeight(500).SingleLine().Shrink(0)
			components.Text(c, summary).Grow(1).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…").Tooltip(summary).FontSize(12).TextColor(t.TextMuted)
			components.Text(c, tr(state)).FontSize(11.5).TextColor(t.TextMuted).SingleLine().Shrink(0)
			components.Icon(c, chevron).Size(14, 14).Shrink(0).TextColor(t.TextMuted)
		})
		if header.Clicked() {
			*expanded = !*expanded
			c.After(0)
		}
		if *expanded {
			ui.Column(c).FillWidth().MinWidth(0).BorderWidth(1, 0, 0, 0).BorderColor(t.Border.Alpha(.65)).Children(func() {
				editorFocused = processingOptions(c, key, compress, format, quality, maxWidth, disabled)
			})
		}
	})
	return editorFocused
}

// processingOptions edits only the values owned by its caller. Uploads keep
// these in the active profile; local image tools have their own preferences.
// Disabling compression still permits a requested conversion or resize.
func processingOptions(c *ui.Context, key string, compress *bool, format *string, quality, maxWidth *float64, disabled bool) (editorFocused bool) {
	t := c.Theme()
	width, _ := c.Size()
	ui.Column(c).Key(key).FillWidth().MinWidth(0).Shrink(0).Padding(12, 14).Gap(10).Disabled(disabled).Children(func() {
		ui.Row(c).FillWidth().MinWidth(0).Wrap().AlignItems(ui.Center).Gap(10).Children(func() {
			components.Switch(c, compress, tr("压缩图片"))
			ui.Spacer(c)
			components.Text(c, tr("参数自动记忆")).FontSize(11).TextColor(t.TextMuted).SingleLine()
		})
		formatField := func() {
			ui.Column(c).Key("format").Grow(1.4).MinWidth(0).Gap(5).Children(func() {
				components.Text(c, tr("输出格式")).FontSize(12).TextColor(t.TextMuted)
				selectControl(c, "图片格式", format, []string{"original", "webp", "avif", "jpeg", "png"}, []string{"保留原格式", "WebP", "AVIF", "JPEG", "PNG"})
			})
		}
		numericFields := func() {
			ui.Column(c).Key("quality").Grow(1).MinWidth(0).Gap(5).Disabled(*format == "png").Children(func() {
				components.Text(c, tr("质量")).FontSize(12).TextColor(t.TextMuted)
				input := components.NumberInput(c, quality, 1, 100, 1).Label(tr("压缩质量")).FillWidth().MinWidth(0)
				editorFocused = editorFocused || input.FocusWithin() && !input.IsDisabled()
			})
			ui.Column(c).Key("max-width").Grow(1).MinWidth(0).Gap(5).Children(func() {
				components.Text(c, tr("最大宽度（px）")).FontSize(12).TextColor(t.TextMuted)
				input := components.NumberInput(c, maxWidth, 0, 32768, 1).Label(tr("最大宽度")).FillWidth().MinWidth(0)
				editorFocused = editorFocused || input.FocusWithin() && !input.IsDisabled()
			})
		}
		if width < 800 && key != "upload-processing" {
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
	return editorFocused
}
