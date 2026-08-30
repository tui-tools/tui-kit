#!/usr/bin/env python3
"""Tests for the ANSI replay in tools/render-screenshots.py.

Run with `make tools-test` or `python3 -m unittest discover -s tools/tests`.
Nothing outside the standard library is needed: the replay is pure Python and
the recorded sample is checked in.
"""
from __future__ import annotations

import importlib.util
import os
import unittest

TOOLS = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DATA = os.path.join(os.path.dirname(os.path.abspath(__file__)), "data")


def load_renderer():
    """Import render-screenshots.py, whose name is not a Python identifier."""
    path = os.path.join(TOOLS, "render-screenshots.py")
    spec = importlib.util.spec_from_file_location("render_screenshots", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


renderer = load_renderer()


def render(raw: bytes, cols: int = 104, rows: int = 26) -> str:
    """Replay a stream at a fixed screen size and return the plain text."""
    renderer.COLS, renderer.ROWS = cols, rows
    try:
        html = renderer.to_html(raw)
    finally:
        renderer.COLS, renderer.ROWS = (renderer.DEFAULT_COLS,
                                        renderer.DEFAULT_ROWS)
    # The frame is HTML; strip the spans so the assertions read as text.
    text, inside = [], False
    i = 0
    while i < len(html):
        ch = html[i]
        if ch == "<":
            inside = True
        elif ch == ">":
            inside = False
        elif not inside:
            text.append(ch)
        i += 1
    return ("".join(text)
            .replace("&lt;", "<").replace("&gt;", ">")
            .replace("&quot;", '"').replace("&#x27;", "'")
            .replace("&amp;", "&"))


class FrameAfterDialogTest(unittest.TestCase):
    """The regression this file exists for.

    `dialog-exit.ansi` is a real recording of tui-update --demo: the confirm
    dialog, then the apply screen that replaces it. The apply frame is shorter
    than the dialog frame, so Bubble Tea ends it by parking the cursor under
    the new content and sending ESC[0J to wipe what is left below. Replaying
    that as a full-screen erase threw the frame away and the screenshot came
    out blank.
    """

    def setUp(self):
        with open(os.path.join(DATA, "dialog-exit.ansi"), "rb") as fh:
            self.raw = fh.read()

    def test_last_frame_survives_the_dialog(self):
        frame = render(self.raw)
        self.assertIn("upgrade finished", frame)
        self.assertIn("sudo -n dnf -y upgrade", frame)

    def test_dialog_is_gone_from_the_last_frame(self):
        frame = render(self.raw)
        self.assertNotIn("y confirm", frame)


class EraseTest(unittest.TestCase):
    """The three modes of ESC[J and ESC[K, on synthetic input."""

    def test_erase_below_keeps_what_is_above_the_cursor(self):
        raw = b"\x1b[Habove\r\n\x1b[2;1Hbelow\x1b[2;1H\x1b[J"
        frame = render(raw, cols=20, rows=4)
        self.assertIn("above", frame)
        self.assertNotIn("below", frame)

    def test_erase_above_keeps_what_is_below_the_cursor(self):
        raw = b"\x1b[Habove\r\n\x1b[2;1Hbelow\x1b[1;6H\x1b[1J"
        frame = render(raw, cols=20, rows=4)
        self.assertNotIn("above", frame)
        self.assertIn("below", frame)

    def test_erase_all_clears_the_screen(self):
        raw = b"\x1b[Habove\r\n\x1b[2;1Hbelow\x1b[2J"
        frame = render(raw, cols=20, rows=4)
        self.assertNotIn("above", frame)
        self.assertNotIn("below", frame)

    def test_erase_line_modes(self):
        raw = b"\x1b[Hleft-right\x1b[1;6H\x1b[K"
        self.assertIn("left", render(raw, cols=20, rows=2))
        self.assertNotIn("right", render(raw, cols=20, rows=2))

        raw = b"\x1b[Hleft-right\x1b[1;5H\x1b[1K"
        frame = render(raw, cols=20, rows=2)
        self.assertNotIn("left", frame)
        self.assertIn("right", frame)

        raw = b"\x1b[Hleft-right\x1b[1;5H\x1b[2K"
        frame = render(raw, cols=20, rows=2)
        self.assertNotIn("left", frame)
        self.assertNotIn("right", frame)


if __name__ == "__main__":
    unittest.main()
