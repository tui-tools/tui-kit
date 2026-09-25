#!/usr/bin/env python3
"""Tests for the --from-ansi mode of tools/render-screenshots.py.

`tailscale-main.ansi` and `tailscale-dialog.ansi` are real frames of
`tui-tailscale --demo`, captured in a detached 120x32 tmux session with
`tmux capture-pane -e -p` (the command docs/screenshots.md tells a guide
author to use). The HTML stage is checked without a browser; the one
end-to-end PNG render runs only when Chrome or Chromium is installed.
"""
from __future__ import annotations

import importlib.util
import os
import re
import struct
import subprocess
import sys
import tempfile
import unittest

TOOLS = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DATA = os.path.join(os.path.dirname(os.path.abspath(__file__)), "data")
SCRIPT = os.path.join(TOOLS, "render-screenshots.py")


def load_renderer():
    """Import render-screenshots.py, whose name is not a Python identifier."""
    spec = importlib.util.spec_from_file_location("render_screenshots", SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


renderer = load_renderer()


def fixture(name: str) -> bytes:
    with open(os.path.join(DATA, name), "rb") as fh:
        return fh.read()


def plain(body: str) -> list[str]:
    """The rows of a rendered body as plain text, spans stripped."""
    text = re.sub(r"<[^>]+>", "", body)
    text = (text.replace("&lt;", "<").replace("&gt;", ">")
            .replace("&quot;", '"').replace("&#x27;", "'")
            .replace("&amp;", "&"))
    return text.split("\n")


class CaptureFixtureTest(unittest.TestCase):
    """The real tmux capture renders at its own size, in its own colors."""

    def setUp(self):
        self.body, self.cols, self.rows = renderer.capture_html(
            fixture("tailscale-main.ansi"))

    def test_size_defaults_to_the_capture(self):
        raw = fixture("tailscale-main.ansi").decode("utf-8")
        lines = raw.rstrip("\n").split("\n")
        widest = max(renderer.visible_width(line) for line in lines)
        self.assertEqual(self.rows, len(lines))
        self.assertEqual(self.cols, widest)
        self.assertLessEqual(self.cols, 120)
        self.assertEqual(len(plain(self.body)), self.rows)

    def test_text_survives(self):
        text = "\n".join(plain(self.body))
        self.assertIn("tui-tailscale", text)
        self.assertIn("headscale.example.com", text)
        self.assertIn("every change is previewed and confirmed", text)
        # Box drawing and the middle dot are multi-byte UTF-8.
        self.assertIn("┃ headscale: users", text)
        self.assertIn("running · online", text)

    def test_truecolor_and_bold_are_kept(self):
        # Tokyo Night blue (title), the muted gray the family uses for
        # secondary text, green for the healthy state, yellow for keys.
        for color in ("#79a2f7", "#565f89", "#9ece69", "#e0af68"):
            self.assertIn(f"color:{color};", self.body)
        self.assertIn("font-weight:bold;", self.body)

    def test_default_colors_are_left_to_the_page(self):
        # Unstyled cells carry no color, so the frame's own fg/bg shows.
        first = self.body.split("\n")[0]
        self.assertTrue(first.startswith(" <span"), first[:40])
        self.assertIn(renderer.DEFAULT_BG, renderer.page_html(self.body))
        self.assertIn(renderer.DEFAULT_FG, renderer.page_html(self.body))

    def test_dialog_borders_are_colored(self):
        # Every border glyph of the real dialog capture comes out in the
        # accent color, none of it falls back to the default foreground.
        body, _, _ = renderer.capture_html(fixture("tailscale-dialog.ansi"))
        styled = re.findall(r'<span style="([^"]*)">([^<]*)</span>', body)
        borders = [css for css, text in styled if "│" in text]
        self.assertGreater(len(borders), 20)
        self.assertTrue(all("color:#79a2f7;" in css for css in borders))
        # Nothing outside a span is a border glyph.
        bare = re.sub(r"<span[^>]*>[^<]*</span>", "", body)
        self.assertFalse(set(bare) & set("╭╮╰╯│─"))


class CropAndSizeTest(unittest.TestCase):

    def test_crop_rows_keeps_a_region(self):
        body, cols, rows = renderer.capture_html(
            fixture("tailscale-main.ansi"),
            crop=renderer.parse_crop("4:11"))
        lines = plain(body)
        self.assertEqual(rows, 7)
        self.assertIn("state", lines[0])
        self.assertIn("MagicDNS", lines[-1])
        self.assertNotIn("tui-tailscale", "\n".join(lines))
        # The width follows the region, not the whole capture.
        self.assertLess(cols, 104)

    def test_crop_keeps_the_color_set_on_an_earlier_line(self):
        raw = b"\x1b[38;2;1;2;3mred\nstill\x1b[0m\nplain\n"
        body, _, rows = renderer.capture_html(
            raw, crop=renderer.parse_crop("1:"))
        self.assertEqual(rows, 2)
        self.assertIn('<span style="color:#010203;">still</span>', body)

    def test_explicit_size_pads_and_clips(self):
        body, cols, rows = renderer.capture_html(b"abcdef\nxy\n", cols=4,
                                                 rows=3)
        self.assertEqual((cols, rows), (4, 3))
        self.assertEqual(plain(body), ["abcd", "xy", ""])

    def test_size_is_capped(self):
        raw = ("x" * 1000 + "\n").encode() * 500
        _, cols, rows = renderer.capture_html(raw)
        self.assertEqual((cols, rows),
                         (renderer.MAX_COLS, renderer.MAX_ROWS))

    def test_trailing_blank_rows_are_dropped(self):
        _, _, rows = renderer.capture_html(b"one\ntwo\n\n\x1b[0m\n\n")
        self.assertEqual(rows, 2)

    def test_parse_crop(self):
        self.assertEqual(renderer.parse_crop("2:5"), slice(2, 5))
        self.assertEqual(renderer.parse_crop(":-1"), slice(None, -1))
        with self.assertRaises(ValueError):
            renderer.parse_crop("5")


class AttributeTest(unittest.TestCase):
    """SGR attributes a capture can carry, on synthetic input."""

    def render(self, raw: bytes) -> str:
        return renderer.capture_html(raw)[0]

    def test_dim_blends_the_foreground_toward_the_background(self):
        body = self.render(b"\x1b[2mfaint\x1b[22m normal\n")
        dim = renderer.blend(renderer.DEFAULT_FG, renderer.DEFAULT_BG,
                             renderer.DIM_RATIO)
        self.assertIn(f'<span style="color:{dim};">faint</span>', body)
        self.assertNotIn(f"color:{dim}", body.split("faint")[1])

    def test_dim_on_an_explicit_color_and_background(self):
        body = self.render(b"\x1b[2;38;2;255;255;255;48;2;0;0;0mx\n")
        self.assertIn("color:#8c8c8c;background:#000000;", body)

    def test_bold_off_also_ends_dim(self):
        body = self.render(b"\x1b[1;2mab\x1b[22mcd\n")
        self.assertIn("font-weight:bold;", body)
        self.assertIn("ab</span>cd", body)

    def test_reverse_swaps_default_colors(self):
        body = self.render(b"\x1b[7m sel \x1b[27m\n")
        self.assertIn(f"color:{renderer.DEFAULT_BG};"
                      f"background:{renderer.DEFAULT_FG};", body)

    def test_256_and_basic_palette(self):
        body = self.render(b"\x1b[38;5;196mA\x1b[31mB\x1b[48;5;244mC\n")
        self.assertIn("color:#ff0000;", body)
        self.assertIn("color:#f7768e;", body)
        self.assertIn("background:#808080;", body)

    def test_colon_subparameters(self):
        body = self.render(b"\x1b[38:2::10:20:30mA\x1b[38:5:21mB\x1b[4:3mC\n")
        self.assertIn("color:#0a141e;", body)
        self.assertIn("color:#0000ff;", body)
        self.assertIn("text-decoration:underline;", body)

    def test_wide_characters_count_two_columns(self):
        self.assertEqual(renderer.visible_width("\x1b[1m日本\x1b[0m"), 4)


class PageTest(unittest.TestCase):

    def test_title_adds_the_window_bar(self):
        page = renderer.page_html("x", "ubuntu: <tui-tailscale>")
        self.assertIn("ubuntu: &lt;tui-tailscale&gt;", page)
        self.assertIn("border-radius:50%", page)

    def test_no_title_keeps_the_readme_page(self):
        page = renderer.page_html("x")
        self.assertNotIn("border-radius:50%", page)
        self.assertEqual(page, renderer.PAGE.replace("{bar}", "")
                         .replace("{body}", "x"))

    def test_window_fits_the_frame(self):
        width, height = map(int, renderer.auto_window(120, 32).split(","))
        self.assertGreater(width, 120 * 8)
        self.assertGreater(height, 32 * 17)
        _, titled = map(int, renderer.auto_window(120, 32, "t").split(","))
        self.assertGreater(titled, height)


@unittest.skipUnless(renderer.find_chrome(), "no chrome/chromium installed")
class EndToEndTest(unittest.TestCase):
    """One real PNG through headless Chrome, when a browser is available."""

    def test_renders_a_png_named_after_the_capture(self):
        with tempfile.TemporaryDirectory() as out:
            subprocess.run(
                [sys.executable, SCRIPT, "--from-ansi",
                 os.path.join(DATA, "tailscale-main.ansi"), "--out", out,
                 "--title", "demo: tui-tailscale"],
                check=True, stdout=subprocess.DEVNULL, timeout=120)
            png = os.path.join(out, "tailscale-main.png")
            with open(png, "rb") as fh:
                head = fh.read(24)
            self.assertEqual(head[:8], b"\x89PNG\r\n\x1a\n")
            width, height = struct.unpack(">II", head[16:24])
            self.assertEqual(f"{width},{height}",
                             renderer.auto_window(113, 32, "t"))
            self.assertEqual(os.listdir(out), ["tailscale-main.png"])


if __name__ == "__main__":
    unittest.main()
