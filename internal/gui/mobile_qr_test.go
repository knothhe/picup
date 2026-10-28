package gui

import (
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestMobileTransferQRKeepsReadableContrastOnDarkUI(t *testing.T) {
	const sessionURL = "http://192.168.1.20:43827/#fEYZny9mhG_JkBWOsvs3FpPr6YRlB8V53sKKUY5VRtw"
	bitmap, err := mobileTransferQR(sessionURL)
	if err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Image(c, bitmap).Size(288, 288).Fit(ui.Contain)
	}, 288, 288)
	tt.SetDark(true)
	tt.Frame()
	img := tt.Image()
	black := 0
	for y := 0; y < 288; y++ {
		for x := 0; x < 288; x++ {
			pixel := img.RGBAAt(x, y)
			if pixel != (color.RGBA{255, 255, 255, 255}) && pixel != (color.RGBA{0, 0, 0, 255}) {
				t.Fatalf("QR code was tinted or blurred at %d,%d: %v", x, y, pixel)
			}
			if x < 12 || y < 12 || x >= 276 || y >= 276 {
				if pixel != (color.RGBA{255, 255, 255, 255}) {
					t.Fatal("QR code lost its white quiet zone")
				}
			}
			if pixel.R == 0 {
				black++
			}
		}
	}
	if black < 288*288/5 {
		t.Fatal("QR code did not render its modules")
	}
	// Optional artifact for checking the rendered code with a real QR reader.
	if path := os.Getenv("PICUP_QR_SCREENSHOT"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMobileTransferQRRejectsUnencodableContent(t *testing.T) {
	for _, content := range []string{"", strings.Repeat("x", 10_000)} {
		if bitmap, err := mobileTransferQR(content); err == nil || bitmap != nil {
			t.Fatal("unencodable QR content was accepted")
		}
	}
}
