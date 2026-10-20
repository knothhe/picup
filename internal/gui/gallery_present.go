package gui

import (
	"math"
	"strings"
	"time"

	"github.com/knothhe/picup/internal/storage"
)

// Search tolerates short edits and ranks by accuracy and match position, like
// the reference gallery's threshold-0.3 fuzzy search. Active search owns order.
func gallerySearchScore(text, query string) (float64, bool) {
	lowerText, lowerQuery := strings.ToLower(text), strings.ToLower(query)
	textRunes, pattern := []rune(lowerText), []rune(lowerQuery)
	if len(pattern) == 0 {
		return 0, true
	}
	best := math.Inf(1)
	if index := strings.Index(lowerText, lowerQuery); index >= 0 {
		best = float64(len([]rune(lowerText[:index]))) / 100
		if best == 0 {
			return 0, true
		}
	}
	// A match starting farther than 30 characters cannot pass the location penalty.
	for start := 0; start <= min(30, len(textRunes)); start++ {
		penalty := float64(start) / 100
		if penalty > min(.3, best) {
			break
		}
		maxErrors := int((min(.3, best)-penalty)*float64(len(pattern)) + 1e-9)
		if len(textRunes)-start < len(pattern)-maxErrors {
			continue
		}
		if maxErrors == 0 {
			if len(textRunes)-start >= len(pattern) {
				same := true
				for j, ch := range pattern {
					if textRunes[start+j] != ch {
						same = false
						break
					}
				}
				if same {
					best = min(best, penalty)
				}
			}
			continue
		}
		previous := make([]int, len(pattern)+1)
		for j := range previous {
			previous[j] = j
		}
		current := make([]int, len(pattern)+1)
		for i := 1; i <= min(len(textRunes)-start, len(pattern)+maxErrors); i++ {
			current[0] = i
			for j := 1; j <= len(pattern); j++ {
				cost := 0
				if textRunes[start+i-1] != pattern[j-1] {
					cost = 1
				}
				current[j] = min(current[j-1]+1, previous[j]+1, previous[j-1]+cost)
			}
			if i >= len(pattern)-maxErrors {
				score := float64(current[len(pattern)])/float64(len(pattern)) + penalty
				if score < best {
					best = score
				}
			}
			previous, current = current, previous
		}
	}
	return best, best <= .3+1e-9
}

type galleryCell struct {
	object     storage.Object
	x, y, w, h float32
}

// Justified rows follow s3-image-port's 208px target height and 8px gaps. Image
// proportions stay intact; a final single image only shrinks to fit the width.
func (g *galleryState) justified(rows []storage.Object, width float32) ([]galleryCell, float32) {
	if width <= 0 {
		return nil, 0
	}
	const target, gap = float32(208), float32(8)
	var cells []galleryCell
	var group []storage.Object
	var widths []float32
	var total, y float32
	flush := func(last bool) {
		if len(group) == 0 {
			return
		}
		scale := float32(1)
		if !last || len(group) > 1 || widths[0] > width {
			scale = (width - gap*float32(len(group)-1)) / total
		}
		x := float32(0)
		h := target * scale
		for i, o := range group {
			w := widths[i] * scale
			cells = append(cells, galleryCell{o, x, y, w, h})
			x += w + gap
		}
		y += h + gap
		group = nil
		widths = nil
		total = 0
	}
	for _, o := range rows {
		ratio := float32(384) / 208
		if t, ok := g.thumbs[o.Key]; ok {
			if t.width > 0 && t.height > 0 {
				ratio = float32(t.width) / float32(t.height)
			} else if t.bitmap != nil {
				w, h := t.bitmap.Size()
				if h > 0 {
					ratio = float32(w) / float32(h)
				}
			}
		}
		w := target * ratio
		if len(group) > 0 && total+w+gap*float32(len(group)) > width {
			flush(false)
		}
		group = append(group, o)
		widths = append(widths, w)
		total += w
	}
	flush(true)
	return cells, max(float32(0), y-gap)
}
func (g *galleryState) toggleSelection(key string, shift bool, rows []storage.Object) {
	if g.selected == nil {
		g.selected = map[string]bool{}
	}
	if g.selected[key] {
		delete(g.selected, key)
		g.pruneSelectionOrder()
		return
	}
	if shift && g.selectionAnchor != "" {
		from, to := -1, -1
		for i, o := range rows {
			if o.Key == g.selectionAnchor {
				from = i
			}
			if o.Key == key {
				to = i
			}
		}
		if from >= 0 && to >= 0 {
			for i := min(from, to); i <= max(from, to); i++ {
				if !g.selected[rows[i].Key] {
					g.selectionOrder = append(g.selectionOrder, rows[i].Key)
				}
				g.selected[rows[i].Key] = true
			}
			g.selectionAnchor = g.selectionOrder[len(g.selectionOrder)-1]
			return
		}
	}
	g.selected[key] = true
	g.selectionOrder = append(g.selectionOrder, key)
	g.selectionAnchor = key
}
func (g *galleryState) filterCount() int {
	count := 0
	if g.prefix != "" {
		count++
	}
	if g.period != "" && (g.period != "custom" || g.from != "" || g.to != "") {
		count++
	}
	return count
}
func (g *galleryState) openPhoto(o storage.Object) {
	copy := o
	g.preview = &copy
	g.previewOpen = true
	g.filterOpen = false
	g.sortOpen = false
	g.dateOpen = false
	g.optionsOpen = false
	g.infoOpen = false
}

func (g *galleryState) movePreview(delta int, now time.Time) bool {
	if g.preview == nil {
		return false
	}
	rows, err := g.filtered(now)
	if err != nil {
		return false
	}
	for i, row := range rows {
		if row.Key != g.preview.Key {
			continue
		}
		next := i + delta
		if next < 0 || next >= len(rows) {
			return false
		}
		g.openPhoto(rows[next])
		g.full = galleryThumb{}
		return true
	}
	return false
}

func (g *galleryState) beginRename(o storage.Object) {
	copy := o
	g.preview = &copy
	g.renameKey = o.Key
	g.renameError = ""
	g.renameOpen = true
	g.optionsOpen = false
}
func (g *galleryState) requestDelete(objects []storage.Object) {
	g.deleteTargets = append([]storage.Object(nil), objects...)
	g.deleteOpen = true
	g.optionsOpen = false
}
func (g *galleryState) dateLabel() string {
	labels := map[string]string{"": tr("所有"), "7d": tr("最近 7 天"), "14d": tr("最近 14 天"), "30d": tr("最近 30 天"), "3m": tr("最近 3 个月"), "6m": tr("最近 6 个月"), "1y": tr("最近 1 年")}
	if g.period == "custom" {
		if g.from == "" && g.to == "" {
			return tr("所有")
		}
		return tf("%s 至 %s", g.from, g.to)
	}
	return labels[g.period]
}
func (g *galleryState) chooseDate(day time.Time) {
	date := day.Format("2006-01-02")
	if g.period != "custom" || g.from == "" || g.to != "" {
		g.period = "custom"
		g.from = date
		g.to = ""
		return
	}
	if date < g.from {
		g.from, g.to = date, g.from
	} else {
		g.to = date
	}
	g.dateOpen = false
}
func (g *galleryState) sortSettings() (string, bool) {
	property := "key"
	desc := true
	switch g.order {
	case "oldest":
		property = "date"
		desc = false
	case "newest":
		property = "date"
	case "name-asc":
		desc = false
	}
	return property, desc
}
func (g *galleryState) setSort(property string, desc bool) {
	if property == "date" {
		g.order = "oldest"
		if desc {
			g.order = "newest"
		}
	} else {
		g.order = "name-asc"
		if desc {
			g.order = "name-desc"
		}
	}
}
func galleryPageNumbers(page, pages int) []int {
	if pages <= 5 {
		out := make([]int, pages)
		for i := range out {
			out[i] = i
		}
		return out
	}
	out := []int{0}
	if page > 2 {
		out = append(out, -1)
	}
	for i := max(1, page-1); i <= min(pages-2, page+1); i++ {
		out = append(out, i)
	}
	if page < pages-3 {
		out = append(out, -1)
	}
	return append(out, pages-1)
}
