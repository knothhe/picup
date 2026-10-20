package assets

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func decodeIcon(t *testing.T, data []byte, size int) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != image.Rect(0, 0, size, size) {
		t.Fatalf("icon bounds = %v; want %d square", img.Bounds(), size)
	}
	return img
}

func TestNativeIconsShareBrandMark(t *testing.T) {
	app := decodeIcon(t, IconPNG, 1024)
	colored := decodeIcon(t, MenuBarPNG, 36)
	template := decodeIcon(t, MenuBarTemplatePNG, 36)
	for y := range 36 {
		for x := range 36 {
			colorPixel := color.NRGBAModel.Convert(colored.At(x, y)).(color.NRGBA)
			templatePixel := color.NRGBAModel.Convert(template.At(x, y)).(color.NRGBA)
			if templatePixel.A != colorPixel.A || templatePixel.R != 0 || templatePixel.G != 0 || templatePixel.B != 0 {
				t.Fatalf("menu bar template changes the mark at %d,%d: color %v; template %v", x, y, colorPixel, templatePixel)
			}
		}
	}
	// These points are in canonical SVG coordinates: clear margin, solid photo,
	// cutout sun, upload arrow head and stem, landscape, and solid bottom edge.
	points := []struct {
		x, y  int
		solid bool
	}{{88, 256, false}, {120, 256, true}, {172, 176, false}, {284, 220, false}, {284, 300, false}, {204, 320, false}, {256, 380, true}}
	for _, point := range points {
		x, y := (point.x-80)*36/352, (point.y-80)*36/352
		_, _, _, alpha := template.At(x, y).RGBA()
		if (alpha > 0x7fff) != point.solid {
			t.Fatalf("menu icon loses brand geometry at SVG %d,%d", point.x, point.y)
		}
		appPixel := color.NRGBAModel.Convert(app.At(point.x*2, point.y*2)).(color.NRGBA)
		if (appPixel.B > appPixel.R+20 && appPixel.R < 100) != point.solid {
			t.Fatalf("app and menu icons differ at SVG %d,%d: %v", point.x, point.y, appPixel)
		}
	}
}
