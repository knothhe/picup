# Picup icons

`icon.svg` is the source for the app, window header, Dock, and menu bar artwork.
The menu bar keeps the photograph, sun, and upload arrow geometry, with the app
tile removed. macOS uses its black alpha template so the system controls its tint.

After editing the SVG, run `make icons`. This uses Python 3 and `rsvg-convert`
from librsvg to regenerate `icon.png`, `menu-bar.png`, and
`menu-bar-template.png`. Use `python3 scripts/generate_icons.py --check` to check
that the generated PNGs match the source. Generated assets are embedded, so
normal builds do not need librsvg.
