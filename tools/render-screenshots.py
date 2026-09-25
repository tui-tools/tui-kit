#!/usr/bin/env python3
r"""Render README screenshots from a real tui-tools binary in --demo mode.

Runs the tool inside a pseudo-terminal, answers the terminal queries Lip Gloss
sends (background color, cursor position), replays the ANSI stream into a
small virtual screen, converts the final frame to HTML and screenshots it with
headless Chrome. No screen recording tools needed, and the frames are the real
UI rather than a mock-up.

Every tool in the family shares this script, parameterized by the binary and
by one key sequence per screen:

    tui-kit/tools/render-screenshots.py \
        --bin bin/tui-firewall --name tui-firewall --out docs/screenshots \
        --screen main= --screen add=a --screen delete=d --screen help=?

Each --screen is `name=keys`: the keys are typed one at a time once the UI has
drawn, and the resulting frame is written to <out>/<name-prefix>-<name>.png.
Escapes are accepted in the key string: \t, \n, \r, \e and \xNN.

The second mode renders frames captured elsewhere -- a real run on a lab host,
for a guide -- instead of running a binary. Each file is what
`tmux capture-pane -e -p` prints (SGR colors and attributes, one line per row,
no cursor movement), and each is written to <out>/<basename>.png with the same
palette, frame and fonts as the README screenshots:

    tmux capture-pane -t lab -e -p > frames/01-main.ansi
    tui-kit/tools/render-screenshots.py \
        --from-ansi frames/ --out docs/guide --title "ubuntu: tui-tailscale"

--from-ansi takes a file or a directory (every *.ansi and *.txt in it) and can
be repeated. The screen size defaults to the widest line and the line count of
the capture; --crop-rows a:b keeps just rows a..b-1 (0-based, like a Python
slice, either end may be left out or negative).
"""
from __future__ import annotations

import argparse
import fcntl
import functools
import html
import math
import os
import pty
import re
import select
import shutil
import struct
import subprocess
import sys
import termios
import time
import unicodedata
import urllib.parse
from collections import namedtuple

DEFAULT_COLS, DEFAULT_ROWS = 104, 26
# Hard caps for a captured frame, so a stray huge file cannot ask Chrome for
# a window the size of a wall.
MAX_COLS, MAX_ROWS = 400, 200
# The default foreground and background of the frame, the Tokyo Night pair the
# family ships. Cells without an explicit color inherit them from the page.
DEFAULT_FG, DEFAULT_BG = "#c0caf5", "#1a1b26"
# How much of the foreground survives the dim (faint) attribute: the color is
# blended toward the cell background, the way terminals render SGR 2.
DIM_RATIO = 0.55
# Cell metrics of the page: a 15px font in whole-pixel 9x18 cells, so that
# neighbouring cells meet on a pixel edge and drawn lines show no seams. With
# the space the page adds around the text they size the Chrome window.
CELL_W, CELL_H = 9, 18
PAD_W, PAD_H, TITLE_H = 88, 80, 26
# The virtual screen size, overridden by --cols/--rows. Module-level because
# the ANSI replay indexes the frame buffer with them.
COLS, ROWS = DEFAULT_COLS, DEFAULT_ROWS
PALETTE = {
    30: "#15161e", 31: "#f7768e", 32: "#9ece6a", 33: "#e0af68", 34: "#7aa2f7",
    35: "#bb9af7", 36: "#7dcfff", 37: "#a9b1d6", 90: "#414868", 91: "#f7768e",
    92: "#9ece6a", 93: "#e0af68", 94: "#7aa2f7", 95: "#bb9af7", 96: "#7dcfff",
    97: "#c0caf5",
}
def parse_keys(spec: str) -> bytes:
    """Turn a --screen key string into the bytes to type."""
    return spec.encode("utf-8").decode("unicode_escape").encode("latin-1")


def capture(binary: str, args: list[str], keys: bytes, settle: float,
            budget: float) -> bytes:
    """Run the binary under a PTY and return everything it wrote."""
    pid, fd = pty.fork()
    if pid == 0:
        os.environ["TERM"] = "xterm-256color"
        os.environ["COLORTERM"] = "truecolor"
        # A fixed theme keeps the frames reproducible on any developer machine,
        # whatever desktop theme happens to be active.
        os.environ.pop("TUI_THEME", None)
        os.environ.pop("NO_COLOR", None)
        os.execv(binary, [binary, *args])
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
    out = b""
    started = time.time()
    answered = sent = False
    while time.time() - started < budget:
        ready, _, _ = select.select([fd], [], [], 0.2)
        if ready:
            try:
                out += os.read(fd, 65536)
            except OSError:
                break
        if not answered and (b"]11;?" in out or b"[6n" in out or b"[c" in out):
            os.write(fd, b"\x1b]11;rgb:1a1a/1b1b/2626\x1b\\\x1b[1;1R\x1b[?62;c")
            answered = True
        if answered and keys and not sent and time.time() - started > settle:
            for k in keys:
                os.write(fd, bytes([k]))
                time.sleep(0.15)
            sent = True
    try:
        os.kill(pid, 9)
    except OSError:
        pass
    return out


def color_css(col):
    if col is None:
        return None
    if col[0] == "rgb":
        return "#%02x%02x%02x" % col[1:]
    if col[0] == "idx":
        return PALETTE.get(col[1], DEFAULT_FG)
    n = col[1]
    if n < 16:
        return PALETTE.get(30 + n if n < 8 else 90 + n - 8, DEFAULT_FG)
    if n < 232:
        n -= 16
        r, g, b = n // 36, (n // 6) % 6, n % 6
        lv = lambda v: 0 if v == 0 else 55 + v * 40  # noqa: E731
        return "#%02x%02x%02x" % (lv(r), lv(g), lv(b))
    gray = 8 + (n - 232) * 10
    return "#%02x%02x%02x" % (gray, gray, gray)


def blend(fg: str, bg: str, ratio: float) -> str:
    """Mix two #rrggbb colors; ratio is the share of fg in the result."""
    a = [int(fg[k:k + 2], 16) for k in (1, 3, 5)]
    b = [int(bg[k:k + 2], 16) for k in (1, 3, 5)]
    return "#%02x%02x%02x" % tuple(
        round(x * ratio + y * (1 - ratio)) for x, y in zip(a, b))


# The SGR state of one cell: colors plus the attributes the page shows.
Style = namedtuple(
    "Style", ("fg", "bg", "bold", "dim", "italic", "underline", "reverse"),
    defaults=(None, None, False, False, False, False, False))


PLAIN = Style()


def style_css(st: Style) -> str:
    """The inline CSS for a cell style; empty for the default look."""
    fg, bg = color_css(st.fg), color_css(st.bg)
    if st.reverse:
        fg, bg = bg or DEFAULT_BG, fg or DEFAULT_FG
    if st.dim:
        fg = blend(fg or DEFAULT_FG, bg or DEFAULT_BG, DIM_RATIO)
    css = (f"color:{fg};" if fg else "") + (f"background:{bg};" if bg else "")
    if st.bold:
        css += "font-weight:bold;"
    if st.italic:
        css += "font-style:italic;"
    if st.underline:
        css += "text-decoration:underline;"
    return css


def sgr_params(params: str) -> list[int]:
    """Split CSI parameters into integers.

    Colon sub-parameters (ITU T.416: `38:2::r:g:b`, `38:5:n`, `4:3`) are
    flattened to the semicolon form the replay understands, so a capture from
    a terminal that prefers colons renders the same.
    """
    if not params:
        return [0]
    out: list[int] = []
    for group in params.replace("?", "").split(";"):
        if ":" not in group:
            out.append(int(group) if group else 0)
            continue
        parts = group.split(":")
        head = int(parts[0]) if parts[0] else 0
        if head in (38, 48, 58) and len(parts) > 1 and parts[1] == "2":
            rgb = [int(x) if x else 0 for x in parts[2:]]
            # The color space id is optional: `38:2::r:g:b` or `38:2:r:g:b`.
            rgb = rgb[-3:] if len(rgb) >= 3 else rgb + [0] * (3 - len(rgb))
            out.extend([head, 2, *rgb])
        elif head in (38, 48, 58) and len(parts) > 2 and parts[1] == "5":
            out.extend([head, 5, int(parts[2] or 0)])
        elif head == 4:
            # Underline style: 4:0 turns it off, anything else is an underline.
            out.append(24 if len(parts) > 1 and parts[1] == "0" else 4)
        else:
            out.append(head)
    return out


def apply_sgr(st: Style, p: list[int]) -> Style:
    """Fold one SGR sequence into the current style."""
    j = 0
    while j < len(p):
        v = p[j]
        if v == 0:
            st = PLAIN
        elif v == 1:
            st = st._replace(bold=True)
        elif v == 2:
            st = st._replace(dim=True)
        elif v == 3:
            st = st._replace(italic=True)
        elif v == 4:
            st = st._replace(underline=True)
        elif v == 7:
            st = st._replace(reverse=True)
        elif v == 22:
            st = st._replace(bold=False, dim=False)
        elif v == 23:
            st = st._replace(italic=False)
        elif v == 24:
            st = st._replace(underline=False)
        elif v == 27:
            st = st._replace(reverse=False)
        elif v == 39:
            st = st._replace(fg=None)
        elif v == 49:
            st = st._replace(bg=None)
        elif 30 <= v <= 37 or 90 <= v <= 97:
            st = st._replace(fg=("idx", v))
        elif 40 <= v <= 47 or 100 <= v <= 107:
            st = st._replace(bg=("idx", v - 10))
        elif v in (38, 48, 58) and j + 1 < len(p):
            col = None
            if p[j + 1] == 2 and j + 4 < len(p):
                col = ("rgb", p[j + 2], p[j + 3], p[j + 4])
                j += 4
            elif p[j + 1] == 5 and j + 2 < len(p):
                col = ("256", p[j + 2])
                j += 2
            if v == 38:
                st = st._replace(fg=col)
            elif v == 48:
                st = st._replace(bg=col)
            # 58 is the underline color, which the page does not draw.
        j += 1
    return st


def replay(raw: bytes, cols: int | None = None,
           rows_n: int | None = None) -> list[list[tuple]]:
    """Replay an ANSI stream into a cols x rows grid of (char, style) cells."""
    cols = cols or COLS
    rows_n = rows_n or ROWS
    raw = re.sub(
        rb"\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1bP[^\x1b]*\x1b\\|\x1b\[\?[0-9;]*[hl]|\x1b[=>]",
        b"", raw)
    txt = raw.decode("utf-8", "replace")
    blank = (" ", None)
    rows = [[blank] * cols for _ in range(rows_n)]
    r = c = 0
    st = PLAIN
    i = 0
    while i < len(txt):
        ch = txt[i]
        if ch == "\x1b" and i + 1 < len(txt) and txt[i + 1] == "[":
            m = re.match(r"\x1b\[([0-9;:?]*)([A-Za-z])", txt[i:])
            if not m:
                i += 1
                continue
            params, cmd = m.group(1), m.group(2)
            i += m.end()
            p = sgr_params(params)
            if cmd == "m":
                st = apply_sgr(st, p)
            elif cmd in ("H", "f"):
                r = (p[0] if p and p[0] else 1) - 1
                c = (p[1] if len(p) > 1 and p[1] else 1) - 1
            elif cmd == "J":
                # Erase in display. The parameter matters: Bubble Tea ends a
                # frame that is shorter than the one before it -- the frame
                # right after a dialog closes -- by parking the cursor under
                # the new content and sending ESC[0J to wipe the leftovers.
                # Treating that as ESC[2J erased the frame itself and the
                # screenshot came out blank.
                mode = p[0] if p else 0
                if mode == 0:
                    for k in range(c, cols):
                        rows[r][k] = blank
                    for row_index in range(r + 1, rows_n):
                        rows[row_index] = [blank] * cols
                elif mode == 1:
                    for row_index in range(0, r):
                        rows[row_index] = [blank] * cols
                    for k in range(0, min(c + 1, cols)):
                        rows[r][k] = blank
                else:
                    rows = [[blank] * cols for _ in range(rows_n)]
            elif cmd == "K":
                # Erase in line, same three modes as above.
                mode = p[0] if p else 0
                start, stop = (c, cols) if mode == 0 else (
                    (0, min(c + 1, cols)) if mode == 1 else (0, cols))
                for k in range(start, stop):
                    rows[r][k] = blank
            elif cmd == "A":
                r = max(0, r - (p[0] or 1))
            elif cmd == "B":
                r = min(rows_n - 1, r + (p[0] or 1))
            elif cmd == "C":
                c = min(cols - 1, c + (p[0] or 1))
            elif cmd == "D":
                c = max(0, c - (p[0] or 1))
            elif cmd == "G":
                c = (p[0] or 1) - 1
            continue
        if ch == "\r":
            c = 0
        elif ch == "\n":
            r = min(rows_n - 1, r + 1)
        elif ch >= " ":
            cell_style = st if st != PLAIN else None
            width = char_width(ch)
            if width == 0:
                # A combining mark or a joiner belongs to the cell before it.
                if 0 < c <= cols and r < rows_n:
                    prev, prev_style = rows[r][c - 1]
                    rows[r][c - 1] = (prev + ch, prev_style)
            else:
                if c < cols and r < rows_n:
                    rows[r][c] = (ch, cell_style)
                    if width == 2 and c + 1 < cols:
                        # The right half of a wide glyph: an empty cell the
                        # page skips, so the row keeps its column count.
                        rows[r][c + 1] = ("", cell_style)
                c += width
        i += 1
    return rows


def char_width(ch: str) -> int:
    """Terminal columns of one character: 0, 1 or 2.

    East Asian Wide and Fullwidth characters take two columns. Ambiguous ones
    (box drawing, the middle dot) take one, as in tmux and every terminal
    outside a CJK locale.
    """
    if unicodedata.combining(ch) or unicodedata.category(ch) in ("Mn", "Me",
                                                                 "Cf"):
        return 0
    return 2 if unicodedata.east_asian_width(ch) in ("W", "F") else 1


# Box drawing (U+2500-U+257F) is drawn as vector lines instead of font
# glyphs: few fonts make their glyphs fill the whole cell height, so borders
# came out as dashes with a gap on every row. The geometry comes from the
# Unicode names, e.g. "BOX DRAWINGS DOWN LIGHT AND RIGHT HEAVY".
BOX_WEIGHTS = {"LIGHT": 1, "SINGLE": 1, "HEAVY": 2, "DOUBLE": 3}
BOX_ARMS = {"UP": "u", "DOWN": "d", "LEFT": "l", "RIGHT": "r",
            "VERTICAL": "ud", "HORIZONTAL": "lr"}
# The drawing is in page pixels, one cell (CELL_W x CELL_H). The center sits on
# a pixel center so a 1px light line and a 3px heavy one stay crisp.
BOX_W, BOX_H = 9, 18
BOX_CX, BOX_CY = 4.5, 8.5
BOX_STROKE = {1: 1, 2: 3}


def box_arms(ch: str) -> tuple[dict[str, int], str] | None:
    """The arms of a box drawing character and its shape.

    Returns ({arm: weight}, shape) with arms among u/d/l/r, weights 1 light,
    2 heavy, 3 double, and shape "line", "arc" or "diagonal".
    """
    if len(ch) != 1 or not 0x2500 <= ord(ch) <= 0x257F:
        return None
    name = unicodedata.name(ch, "")
    if not name.startswith("BOX DRAWINGS "):
        return None
    words = name[len("BOX DRAWINGS "):].split()
    if "DIAGONAL" in words:
        return {}, "diagonal"
    shape = "arc" if "ARC" in words else "line"
    # "DOUBLE DASH" and friends describe dashes, not a double line; the
    # dashes themselves are drawn solid.
    if "DASH" in words:
        k = words.index("DASH")
        del words[k - 1:k + 1]
    arms: dict[str, int] = {}
    weight = 1
    for clause in " ".join(words).split(" AND "):
        tokens = clause.split()
        clause_weight = next(
            (BOX_WEIGHTS[t] for t in tokens if t in BOX_WEIGHTS), weight)
        for token in tokens:
            for arm in BOX_ARMS.get(token, ""):
                arms[arm] = clause_weight
        weight = clause_weight
    return arms, shape


@functools.lru_cache(maxsize=None)
def box_image(ch: str, color: str) -> str | None:
    """A CSS background-image drawing a box character in one cell."""
    parsed = box_arms(ch)
    if parsed is None:
        return None
    arms, shape = parsed
    cx, cy = BOX_CX, BOX_CY
    ends = {"u": (cx, 0), "d": (cx, BOX_H), "l": (0, cy), "r": (BOX_W, cy)}
    paths: list[tuple[str, float]] = []
    if shape == "diagonal":
        if ch in "╱╳":
            paths.append((f"M{BOX_W} 0L0 {BOX_H}", 1))
        if ch in "╲╳":
            paths.append((f"M0 0L{BOX_W} {BOX_H}", 1))
    elif shape == "arc":
        # One vertical and one horizontal arm, joined by a quarter curve.
        v = "u" if "u" in arms else "d"
        h = "l" if "l" in arms else "r"
        vx, vy = ends[v]
        hx, hy = ends[h]
        radius = 4
        sy = cy + (radius if v == "d" else -radius)
        sx = cx + (radius if h == "r" else -radius)
        paths.append((f"M{vx} {vy}L{cx} {sy}Q{cx} {cy} {sx} {cy}L{hx} {hy}",
                       BOX_STROKE[1]))
    else:
        for arm, weight in arms.items():
            x, y = ends[arm]
            if weight == 3:
                # Two thin lines either side of the center line.
                for off in (-2, 2):
                    if arm in "ud":
                        paths.append((f"M{cx + off} {cy}L{x + off} {y}", 1))
                    else:
                        paths.append((f"M{cx} {cy + off}L{x} {y + off}", 1))
            else:
                paths.append((f"M{cx} {cy}L{x} {y}", BOX_STROKE[weight]))
    svg = (f"<svg xmlns='http://www.w3.org/2000/svg' width='{BOX_W}' "
           f"height='{BOX_H}' viewBox='0 0 {BOX_W} {BOX_H}'>" + "".join(
               f"<path d='{d}' stroke='{color}' stroke-width='{w}' "
               "stroke-linecap='square' fill='none'/>" for d, w in paths)
           + "</svg>")
    return "url(\"data:image/svg+xml," + urllib.parse.quote(svg, safe="=:/' ") \
        + "\")"


def cell_colors(st) -> tuple[str, str | None]:
    """The foreground a cell is drawn in, and its background if it has one."""
    if not st:
        return DEFAULT_FG, None
    css = style_css(st)
    fg = re.search(r"(?:^|;)color:(#[0-9a-f]{6})", css)
    bg = re.search(r"background:(#[0-9a-f]{6})", css)
    return (fg.group(1) if fg else DEFAULT_FG), (bg.group(1) if bg else None)


def grid_html(rows: list[list[tuple]]) -> str:
    """Turn a replayed grid into the body of the page.

    Every row is a block exactly one cell high and every cell an inline box
    exactly one column wide (two for a wide glyph), so rows touch, the columns
    line up whatever the font does with bold or fallback glyphs, and trailing
    spaces never collapse.
    """
    lines = []
    for row in rows:
        runs: list[tuple] = []
        k = 0
        while k < len(row):
            ch, st = row[k]
            wide = (ch and char_width(ch[0]) == 2 and k + 1 < len(row)
                    and row[k + 1][0] == "")
            cell = (ch or " ", st, 2 if wide else 1)
            if runs and runs[-1][0] == st:
                runs[-1][1].append(cell)
            else:
                runs.append((st, [cell]))
            k += 2 if wide else 1
        line = ""
        for st, cells in runs:
            css = style_css(st) if st else ""
            fg, _ = cell_colors(st)
            inner = ""
            for ch, _, width in cells:
                image = box_image(ch, fg)
                if image:
                    attr = html.escape(f"background-image:{image}")
                    inner += (f'<c class="b" style="{attr}">'
                              f"{html.escape(ch)}</c>")
                elif width == 2:
                    inner += f'<c class="w">{html.escape(ch)}</c>'
                else:
                    inner += f"<c>{html.escape(ch)}</c>"
            line += f'<span style="{css}">{inner}</span>' if css else inner
        lines.append(f'<div class="r">{line}</div>')
    return "\n".join(lines)


def to_html(raw: bytes) -> str:
    return grid_html(replay(raw))


SGR_RE = re.compile(r"\x1b\[[0-9;:?]*[A-Za-z]")


def visible_width(line: str) -> int:
    """Terminal columns a captured line occupies, SGR sequences excluded."""
    return sum(char_width(ch) for ch in SGR_RE.sub("", line) if ch >= " ")


def drawn_width(row: list[tuple]) -> int:
    """Cells up to the last one that shows something: a glyph or a fill."""
    for k in range(len(row) - 1, -1, -1):
        ch, st = row[k]
        if ch != " " or (st and (st.bg or st.reverse or st.underline)):
            return k + 1
    return 0


def parse_crop(spec: str) -> slice:
    """--crop-rows a:b as a Python slice over the captured rows."""
    a, sep, b = spec.partition(":")
    if not sep:
        raise ValueError(f"--crop-rows wants a:b, got {spec!r}")
    return slice(int(a) if a.strip() else None, int(b) if b.strip() else None)


def capture_html(raw: bytes, cols: int | None = None, rows: int | None = None,
                 crop: slice | None = None) -> tuple[str, int, int]:
    """Render a `tmux capture-pane -e -p` frame.

    Returns the page body and the size in cells actually drawn. The whole
    capture is replayed first and cropped afterwards: tmux carries the SGR
    state from one line to the next, so cutting the text before the replay
    would lose the colors a cropped region starts with.
    """
    text = raw.decode("utf-8", "replace").replace("\r\n", "\n")
    lines = text.split("\n")
    # tmux pads the capture to the pane height; the blank tail is not content.
    while lines and not SGR_RE.sub("", lines[-1]).strip():
        lines.pop()
    if not lines:
        lines = [""]
    widest = max(visible_width(line) for line in lines) or 1
    grid = replay("\r\n".join(lines).encode("utf-8"),
                  min(widest, MAX_COLS), min(len(lines), MAX_ROWS))
    if crop is not None:
        grid = grid[crop] or grid[:1]
    if cols is None:
        cols = max((drawn_width(row) for row in grid), default=1) or 1
    rows = rows if rows is not None else len(grid)
    cols, rows = max(1, min(cols, MAX_COLS)), max(1, min(rows, MAX_ROWS))
    blank = [(" ", None)] * cols
    grid = [(row + blank)[:cols] for row in grid[:rows]]
    grid += [list(blank) for _ in range(rows - len(grid))]
    return grid_html(grid), cols, rows


PAGE = """<html><body style="margin:0;background:#0f0f14;padding:24px;font-family:'Noto Sans Mono','JetBrains Mono',monospace">
<div style="display:inline-block;background:#1a1b26;color:#c0caf5;border-radius:10px;padding:16px 20px;box-shadow:0 8px 30px #0008">{bar}
<style>
.t{font:15px/18px 'Noto Sans Mono','JetBrains Mono','DejaVu Sans Mono',monospace}
.r{height:18px;white-space:pre}
c{display:inline-block;width:9px;height:18px;vertical-align:top;text-align:center}
c.w{width:18px}
c.b{color:transparent;background-size:100% 100%;background-repeat:no-repeat}
</style>
<div class="t">{body}</div></div></body></html>"""

# The window bar a --title adds on top of the frame: three dots and the title,
# in the same palette, so a guide can say which host a frame came from.
BAR = """<div style="display:flex;align-items:center;gap:7px;margin:-4px 0 12px;height:18px;font:12px 'Noto Sans Mono','JetBrains Mono',monospace;color:#565f89">
<span style="width:11px;height:11px;border-radius:50%;background:#f7768e"></span><span style="width:11px;height:11px;border-radius:50%;background:#e0af68"></span><span style="width:11px;height:11px;border-radius:50%;background:#9ece6a"></span>
<span style="flex:1;text-align:center;margin-right:51px">{title}</span></div>"""


def page_html(body: str, title: str = "") -> str:
    """Wrap a frame in the page; the window bar only when a title is given."""
    bar = BAR.replace("{title}", html.escape(title)) if title else ""
    return PAGE.replace("{bar}", bar).replace("{body}", body)


def auto_window(cols: int, rows: int, title: str = "") -> str:
    """A Chrome window just large enough for a cols x rows frame."""
    width = math.ceil(cols * CELL_W) + PAD_W + 2
    height = math.ceil(rows * CELL_H) + PAD_H + 2 + (TITLE_H if title else 0)
    return f"{width},{height}"


def fit_window(requested: str, cols: int, rows: int, title: str = "") -> str:
    """The window to shoot: at least the requested W,H, never smaller than
    the frame, so a size a tool's Makefile pinned for the old, narrower
    cells cannot clip the right edge."""
    width, height = map(int, auto_window(cols, rows, title).split(","))
    if requested:
        want_w, want_h = map(int, requested.split(","))
        width, height = max(width, want_w), max(height, want_h)
    return f"{width},{height}"


def find_chrome() -> str | None:
    return (shutil.which("google-chrome") or shutil.which("chromium")
            or shutil.which("chromium-browser"))


def screenshot(chrome: str, page: str, png: str, window: str) -> None:
    """Write the page to a hidden file next to the PNG and shoot it."""
    tmp = os.path.join(os.path.dirname(png) or ".",
                       "." + os.path.basename(png)[:-4] + ".html")
    with open(tmp, "w", encoding="utf-8") as fh:
        fh.write(page)
    try:
        subprocess.run(
            [chrome, "--headless=new", "--no-sandbox", "--hide-scrollbars",
             f"--window-size={window}", f"--screenshot={png}",
             f"file://{os.path.abspath(tmp)}"],
            check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    finally:
        os.remove(tmp)


def ansi_inputs(specs: list[str]) -> list[str]:
    """Expand --from-ansi arguments: files as given, directories sorted."""
    files = []
    for spec in specs:
        if os.path.isdir(spec):
            files += sorted(
                os.path.join(spec, name) for name in os.listdir(spec)
                if name.endswith((".ansi", ".txt"))
                and os.path.isfile(os.path.join(spec, name)))
        else:
            files.append(spec)
    return files


def render_captures(args, chrome: str) -> int:
    crop = parse_crop(args.crop_rows) if args.crop_rows else None
    files = ansi_inputs(args.from_ansi)
    if not files:
        print("--from-ansi matched no *.ansi or *.txt file", file=sys.stderr)
        return 1
    for path in files:
        with open(path, "rb") as fh:
            body, cols, rows = capture_html(fh.read(), args.cols, args.rows,
                                            crop)
        name = os.path.splitext(os.path.basename(path))[0]
        png = os.path.join(args.out, f"{name}.png")
        window = fit_window(args.window, cols, rows, args.title)
        screenshot(chrome, page_html(body, args.title), png, window)
        print(png)
    return 0


def render_binary(args, chrome: str) -> int:
    global COLS, ROWS
    COLS = args.cols or DEFAULT_COLS
    ROWS = args.rows or DEFAULT_ROWS
    prefix = args.name or os.path.basename(args.bin)
    binary = os.path.abspath(args.bin)
    if not os.access(binary, os.X_OK):
        print(f"{binary} is not executable; build it first", file=sys.stderr)
        return 1
    if not args.screen:
        print("no --screen given; nothing to render", file=sys.stderr)
        return 1
    tool_args = args.args.split() if args.args else []
    for spec in args.screen:
        name, _, keys = spec.partition("=")
        if args.only and name != args.only:
            continue
        frame = to_html(capture(binary, tool_args, parse_keys(keys),
                                args.settle, args.budget))
        png = os.path.join(args.out, f"{prefix}-{name}.png")
        window = fit_window(args.window, COLS, ROWS, args.title)
        screenshot(chrome, page_html(frame, args.title), png, window)
        print(png)
    return 0


def main() -> int:
    ap = argparse.ArgumentParser(
        formatter_class=argparse.RawDescriptionHelpFormatter,
        description=__doc__)
    mode = ap.add_mutually_exclusive_group(required=True)
    mode.add_argument("--bin", help="path to the tool binary")
    mode.add_argument("--from-ansi", action="append", metavar="PATH",
                      help="a `tmux capture-pane -e -p` file, or a directory "
                           "of them; repeat for more")
    ap.add_argument("--out", default="docs/screenshots",
                    help="directory the PNGs are written to")
    ap.add_argument("--name", default="",
                    help="file name prefix; defaults to the binary name")
    ap.add_argument("--screen", action="append", default=[], metavar="NAME=KEYS",
                    help="one screen to render; repeat for more")
    ap.add_argument("--args", default="--demo",
                    help="arguments passed to the binary (default: --demo)")
    ap.add_argument("--only", default="", help="render just this screen")
    ap.add_argument("--cols", type=int, default=None,
                    help=f"screen width (binary: {DEFAULT_COLS}; capture: "
                         "its widest line)")
    ap.add_argument("--rows", type=int, default=None,
                    help=f"screen height (binary: {DEFAULT_ROWS}; capture: "
                         "its line count)")
    ap.add_argument("--title", default="",
                    help="text for a window bar above the frame, e.g. "
                         "'ubuntu: tui-tailscale'")
    ap.add_argument("--crop-rows", default="", metavar="A:B",
                    help="captures only: keep rows A..B-1 (0-based slice)")
    ap.add_argument("--settle", type=float, default=2.0,
                    help="seconds to wait before typing the keys")
    ap.add_argument("--budget", type=float, default=5.0,
                    help="seconds to keep the tool running")
    ap.add_argument("--window", default="",
                    help="minimum headless Chrome window size, W,H; the "
                         "window always grows to fit the frame")
    args = ap.parse_args()
    if args.crop_rows and not args.from_ansi:
        ap.error("--crop-rows only applies to --from-ansi")
    if args.crop_rows:
        try:
            parse_crop(args.crop_rows)
        except ValueError as err:
            ap.error(str(err))

    chrome = find_chrome()
    if not chrome:
        print("no chrome/chromium found", file=sys.stderr)
        return 1
    os.makedirs(args.out, exist_ok=True)
    if args.from_ansi:
        return render_captures(args, chrome)
    return render_binary(args, chrome)


if __name__ == "__main__":
    sys.exit(main())
