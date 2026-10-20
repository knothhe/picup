"""The native menu icon must retain the application mark's SVG geometry."""

import unittest
import xml.etree.ElementTree as ET

from generate_icons import ROOT, menu_bar_svg


class IconTests(unittest.TestCase):
    def test_menu_variants_reuse_canonical_mark(self):
        source = (ROOT / "assets/icon.svg").read_bytes()
        canonical = next(node for node in ET.fromstring(source)
                         if node.get("id") == "picup-mark")
        for template in (False, True):
            document = ET.fromstring(menu_bar_svg(source, template))
            self.assertEqual(len(document), 1, "The menu icon must omit the app tile")
            expected = dict(canonical.attrib)
            if template:
                expected["fill"] = "#000000"
            self.assertEqual(document[0].attrib, expected)

    def test_missing_mark_fails_instead_of_drawing_another_icon(self):
        with self.assertRaisesRegex(ValueError, "picup-mark"):
            menu_bar_svg(b'<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>')


if __name__ == "__main__":
    unittest.main()
