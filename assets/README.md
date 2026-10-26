# Picup icons

`icon.svg` is the source for the app, window header, Dock, and menu bar artwork.
The menu bar keeps the photograph, sun, and upload arrow geometry, with the app
tile removed. macOS uses its black alpha template so the system controls its tint.

After editing the SVG, run `make icons`. This uses Python 3 and `rsvg-convert`
from librsvg to regenerate `icon.png`, `menu-bar.png`, and
`menu-bar-template.png`. Use `python3 scripts/generate_icons.py --check` to check
that the generated PNGs match the source. Generated assets are embedded, so
normal builds do not need librsvg.

Development builds use the independent `dev/icon-dev.png`, generated from the
unchanged `icon.png` by the h2-desktop-client `scripts/dev_icon.py` helper. The
lower-right Dev badge is shared by the bundle, Dock, and native window icon;
the menu bar template keeps its existing monochrome geometry. Regenerate with
`make icons-dev PYTHON=/path/to/python3` using Pillow 10.1+ (the checked-in files
were generated with Pillow 12.3.0). The build checks source/generator hashes,
generation parameters, and every output hash without requiring Pillow.
