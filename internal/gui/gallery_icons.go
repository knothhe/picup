package gui

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

// MingCute paths match the icons used by s3-image-port. The embedded license and
// attribution accompany these assets in both source and packaged executables.
//
//go:embed gallery-assets/*.svg gallery-assets/LICENSE gallery-assets/NOTICE
var galleryIconAssets embed.FS

func galleryAssetIcon(name string) *ui.SVG {
	b, err := galleryIconAssets.ReadFile("gallery-assets/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	return ui.MustParseSVG(b)
}

var galleryIcons = struct{ refresh, selection, checked, copy, trash, empty, back, info, key, time *ui.SVG }{
	galleryAssetIcon("refresh-2-line"), galleryAssetIcon("checkbox-line"), galleryAssetIcon("checkbox-fill"), galleryAssetIcon("copy-2-line"), galleryAssetIcon("delete-3-line"), galleryAssetIcon("empty-box-line"), galleryAssetIcon("arrow-left-line"), galleryAssetIcon("information-line"), galleryAssetIcon("key-2-line"), galleryAssetIcon("time-line"),
}
