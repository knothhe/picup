"""Generate all native Picup icons from assets/icon.svg using librsvg."""

import argparse
import copy
from pathlib import Path
import subprocess
import sys
import xml.etree.ElementTree as ET


ROOT = Path(__file__).resolve().parent.parent
SVG_NAMESPACE = "http://www.w3.org/2000/svg"
ET.register_namespace("", SVG_NAMESPACE)


def menu_bar_svg(source, template=False):
    """Keep the canonical mark, with padding suited to an 18 DIP menu icon."""
    document = ET.fromstring(source)
    mark = next((node for node in document if node.get("id") == "picup-mark"), None)
    if mark is None:
        raise ValueError("assets/icon.svg is missing its picup-mark path")
    mark = copy.deepcopy(mark)
    if template:
        mark.set("fill", "#000000")
    root = ET.Element(f"{{{SVG_NAMESPACE}}}svg", {
        "viewBox": "80 80 352 352", "width": "36", "height": "36",
    })
    root.append(mark)
    return ET.tostring(root, encoding="utf-8", xml_declaration=True)


def rasterize(svg, size):
    return subprocess.run(["rsvg-convert", "--width", str(size), "--height", str(size)],
                          input=svg, stdout=subprocess.PIPE, check=True).stdout


def generate(check=False):
    source = (ROOT / "assets/icon.svg").read_bytes()
    variants = {
        "icon.png": (source, 1024),
        "menu-bar.png": (menu_bar_svg(source), 36),
        "menu-bar-template.png": (menu_bar_svg(source, template=True), 36),
    }
    stale = []
    for name, (svg, size) in variants.items():
        destination = ROOT / "assets" / name
        rendered = rasterize(svg, size)
        if check:
            if not destination.exists() or destination.read_bytes() != rendered:
                stale.append(name)
        else:
            destination.write_bytes(rendered)
    if stale:
        print("Regenerate icons with python3 scripts/generate_icons.py: " + ", ".join(stale),
              file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="Check generated PNGs without writing")
    args = parser.parse_args()
    raise SystemExit(generate(args.check))
