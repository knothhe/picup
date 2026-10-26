//go:build !picup_dev

package assets

import _ "embed"

// IconPNG is the packaged application, native window and Dock artwork.
//
//go:embed icon.png
var IconPNG []byte
