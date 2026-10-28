package gui

import (
	"github.com/egoist/mygo/ui"
	qrcode "github.com/skip2/go-qrcode"
)

// mobileTransferQR keeps the quiet zone and white background even when the
// desktop uses a dark theme. Create it once for each receiving session.
func mobileTransferQR(url string) (*ui.Bitmap, error) {
	code, err := qrcode.New(url, qrcode.Medium)
	if err != nil {
		return nil, err
	}
	return ui.NewBitmap(code.Image(288)).Pixelated(), nil
}
