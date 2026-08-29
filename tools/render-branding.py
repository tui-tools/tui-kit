#!/usr/bin/env python3
"""Generate the tui-tools branding assets: SVG sources and their PNG exports.

The whole family shares one frame: a rounded terminal square in Tokyo Night
colors carrying a green prompt chevron. The family icon closes it with a blue
cursor block; a tool icon replaces that cursor with the tool's own glyph
(a shield for tui-firewall, a cog for tui-systemd).

Usage:
    tools/render-branding.py [--out assets/branding]

Needs one SVG rasterizer on PATH: rsvg-convert, magick or inkscape.
"""
from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys

# Tokyo Night, the family default palette (see docs/branding.md).
BG = "#1a1b26"
FRAME = "#2f334d"
FG = "#c0caf5"
GREEN = "#9ece6a"
BLUE = "#7aa2f7"

ICON_SIZES = (512, 256, 128, 64)
AVATAR_SIZE = 500


def frame(bg: str = BG) -> str:
    """The rounded terminal square every icon in the family shares."""
    return (
        f'<rect x="32" y="32" width="448" height="448" rx="104" ry="104" '
        f'fill="{bg}" stroke="{FRAME}" stroke-width="8"/>'
    )


def chevron(color: str = GREEN) -> str:
    """The prompt glyph: a shell caret, the mark of the family."""
    return (
        f'<polyline points="150,178 244,256 150,334" fill="none" '
        f'stroke="{color}" stroke-width="36" stroke-linecap="round" '
        f'stroke-linejoin="round"/>'
    )


def cursor(color: str = BLUE) -> str:
    """The blinking block after the prompt: the family icon's right half."""
    return f'<rect x="290" y="306" width="132" height="34" rx="17" fill="{color}"/>'


def shield(color: str = BLUE) -> str:
    """tui-firewall: a shield, drawn on the same baseline as the cursor."""
    return (
        f'<path d="M352 176 L418 204 L418 262 C418 306 388 332 352 344 '
        f'C316 332 286 306 286 262 L286 204 Z" fill="none" stroke="{color}" '
        f'stroke-width="26" stroke-linejoin="round"/>'
    )


def cog(color: str = BLUE) -> str:
    """tui-systemd: a cog, eight teeth around the same optical center."""
    cx, cy = 352, 258
    teeth = "".join(
        f'<rect x="{cx - 13}" y="{cy - 92}" width="26" height="34" rx="8" '
        f'fill="{color}" transform="rotate({angle} {cx} {cy})"/>'
        for angle in range(0, 360, 45)
    )
    return (
        teeth
        + f'<circle cx="{cx}" cy="{cy}" r="52" fill="none" stroke="{color}" '
        f'stroke-width="26"/>'
    )


def icon_svg(glyph: str, bg: str = BG, accent: str = GREEN) -> str:
    """Wrap a glyph in the shared 512x512 frame."""
    return (
        '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512" '
        'width="512" height="512" role="img" aria-label="tui-tools">'
        f"{frame(bg)}{chevron(accent)}{glyph}"
        "</svg>"
    )


# Monospace advance width, as a fraction of the font size. DejaVu Sans Mono
# and JetBrains Mono both sit at ~0.6em; the slack keeps the box from clipping.
ADVANCE = 0.63
FONT = "JetBrains Mono, DejaVu Sans Mono, Menlo, monospace"


# The lockup box, in SVG user units. LOGO_HEIGHT is fixed so every logo in
# the family lines up; the width grows with the name.
LOGO_HEIGHT = 160
LOGO_FONT_SIZE = 82
LOGO_TEXT_X = 184


def logo_width(name: str) -> int:
    """Width of the lockup box for a name, in SVG user units."""
    return int(LOGO_TEXT_X + ADVANCE * LOGO_FONT_SIZE * len(name) + 28)


def logo_svg(name: str, glyph: str, text_color: str, bg: str | None) -> str:
    """A horizontal lockup: the icon, then `tui-` in green and the rest in
    the foreground color. The green prefix is the naming rule made visible."""
    size = 128
    font_size = LOGO_FONT_SIZE
    text_x = LOGO_TEXT_X
    prefix, _, rest = name.partition("-")
    prefix += "-"
    width = logo_width(name)
    height = LOGO_HEIGHT
    background = (
        f'<rect width="{width}" height="{height}" fill="{bg}"/>' if bg else ""
    )
    return (
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {width} {height}" '
        f'width="{width}" height="{height}" role="img" aria-label="{name}">'
        f"{background}"
        f'<g transform="translate(16 16) scale({size / 512})">'
        f"{frame()}{chevron()}{glyph}"
        "</g>"
        f'<text x="{text_x}" y="107" font-family="{FONT}" font-size="{font_size}" '
        f'font-weight="700" letter-spacing="-1">'
        f'<tspan fill="{GREEN}">{prefix}</tspan>'
        f'<tspan fill="{text_color}">{rest}</tspan>'
        "</text>"
        "</svg>"
    )


def rasterizer() -> list[str]:
    """Pick an SVG rasterizer, as an argv template using {src} {dst} {size}."""
    if shutil.which("rsvg-convert"):
        return ["rsvg-convert", "-w", "{size}", "-h", "{h}", "-o", "{dst}", "{src}"]
    if shutil.which("magick"):
        return [
            "magick", "-background", "none", "-density", "600", "{src}",
            "-resize", "{size}x{h}", "{dst}",
        ]
    if shutil.which("inkscape"):
        return [
            "inkscape", "{src}", "-w", "{size}", "-h", "{h}",
            "--export-filename={dst}",
        ]
    return []


def png(tool: list[str], src: str, dst: str, width: int, height: int) -> None:
    argv = [
        a.format(src=src, dst=dst, size=str(width), h=str(height)) for a in tool
    ]
    subprocess.run(argv, check=True)
    print(dst)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default="assets/branding")
    args = ap.parse_args()

    tool = rasterizer()
    if not tool:
        print("no SVG rasterizer found (rsvg-convert, magick or inkscape)",
              file=sys.stderr)
        return 1
    os.makedirs(args.out, exist_ok=True)

    def write(name: str, body: str) -> str:
        path = os.path.join(args.out, name)
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(body + "\n")
        print(path)
        return path

    glyphs = {
        "": cursor(),
        "firewall": shield(),
        "systemd": cog(),
    }

    # Square icons: the family mark and one variant per tool.
    for suffix, glyph in glyphs.items():
        stem = "icon" if not suffix else f"icon-{suffix}"
        src = write(f"{stem}.svg", icon_svg(glyph))
        for size in ICON_SIZES:
            png(tool, src, os.path.join(args.out, f"{stem}-{size}.png"), size, size)

    # The org avatar: the family icon on a full-bleed background, since the
    # GitHub avatar is cropped to a circle and a transparent margin looks off.
    avatar = write(
        "org-avatar.svg",
        '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512" '
        'width="512" height="512" role="img" aria-label="tui-tools">'
        f'<rect width="512" height="512" fill="{BG}"/>'
        f"{chevron()}{cursor()}"
        "</svg>",
    )
    png(tool, avatar, os.path.join(args.out, "org-avatar-500.png"),
        AVATAR_SIZE, AVATAR_SIZE)

    # Horizontal lockups, one pair per name: dark for dark READMEs, light for
    # light ones. `logo.svg` is the dark-background default.
    names = {"tui-tools": cursor(), "tui-firewall": shield(),
             "tui-systemd": cog(), "tui-kit": cursor()}
    for name, glyph in names.items():
        stem = "logo" if name == "tui-tools" else f"logo-{name.removeprefix('tui-')}"
        dark = write(f"{stem}-dark.svg", logo_svg(name, glyph, FG, None))
        light = write(f"{stem}-light.svg", logo_svg(name, glyph, "#24283b", None))
        if name == "tui-tools":
            write("logo.svg", logo_svg(name, glyph, FG, None))
        ratio = LOGO_HEIGHT / logo_width(name)
        for src, tag in ((dark, "dark"), (light, "light")):
            for width in (640, 320):
                dst = os.path.join(args.out, f"{stem}-{tag}-{width}.png")
                png(tool, src, dst, width, round(width * ratio))
    return 0


if __name__ == "__main__":
    sys.exit(main())
