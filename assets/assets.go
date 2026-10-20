// Package assets holds the Picup artwork used by native and in-window UI.
package assets

import _ "embed"

// IconSVG is the canonical application artwork. Generate the PNG variants with
// python3 scripts/generate_icons.py after changing it.
//
//go:embed icon.svg
var IconSVG []byte

// IconPNG is the packaged app icon, also used for native window and Dock icons.
//
//go:embed icon.png
var IconPNG []byte

// MenuBarPNG is the same photograph and upload mark without its app tile.
//
//go:embed menu-bar.png
var MenuBarPNG []byte

// MenuBarTemplatePNG preserves the mark's alpha for the macOS menu bar tint.
//
//go:embed menu-bar-template.png
var MenuBarTemplatePNG []byte
