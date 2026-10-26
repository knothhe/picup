//go:build picup_dev

package assets

import _ "embed"

// IconPNG uses the same derived Dev artwork as the development bundle.
//
//go:embed dev/icon-dev.png
var IconPNG []byte
