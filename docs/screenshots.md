# Screenshots

`tools/render-screenshots.py` turns terminal output into the PNGs the family
uses in READMEs and guides. It has two input modes that share the same
palette, frame, window size rules and fonts, so a guide image sits next to a
README screenshot without looking like it came from somewhere else.

Both modes need `google-chrome` or `chromium` on the `PATH`. Nothing else
outside the Python standard library is required.

## From the binary (README screenshots)

The script runs the tool in `--demo` mode inside a pseudo-terminal, types one
key sequence per screen and screenshots the final frame:

```sh
tui-kit/tools/render-screenshots.py \
    --bin bin/tui-firewall --name tui-firewall --out docs/screenshots \
    --screen main= --screen add=a --screen delete=d --screen help=?
```

Each `--screen` is `name=keys` and becomes `<out>/<name>-<screen>.png`.
Escapes are accepted in the key string: `\t`, `\n`, `\r`, `\e` and `\xNN`.
The screen is 104x26 unless `--cols`/`--rows` say otherwise.

## From captured frames (guides)

A guide built from a real run on a lab host cannot use `--demo`: the frames
have to come from the host itself. Capture them with tmux and render them
later with `--from-ansi`.

Start the tool in a detached tmux session with a fixed size, drive it with
`send-keys`, and save each frame with `capture-pane -e -p`:

```sh
tmux new-session -d -s guide -x 120 -y 32 -e COLORTERM=truecolor tui-tailscale
tmux send-keys -t guide a                              # open a dialog
tmux capture-pane -t guide -e -p > frames/02-accept-routes.ansi
tmux kill-session -t guide
```

`-e` keeps the colors and attributes as SGR sequences and `-p` prints the
pane to stdout. Do not add `-J` or `-C`: the renderer expects one line per
row, exactly as on screen. `-e COLORTERM=truecolor` on `new-session`
(tmux 3.2 or later) makes the tool pick the same truecolor palette as the
README screenshots.

Then render every frame in the directory:

```sh
tui-kit/tools/render-screenshots.py \
    --from-ansi frames/ --out docs/guide --title "ubuntu: tui-tailscale"
```

- `--from-ansi` takes a file or a directory (every `*.ansi` and `*.txt` in
  it, sorted by name) and can be repeated. Each file becomes
  `<out>/<basename>.png`, so `02-accept-routes.ansi` gives
  `02-accept-routes.png`.
- The screen size defaults to the widest line and the line count of the
  capture, after dropping the blank rows tmux pads the pane with, capped at
  400x200. `--cols`/`--rows` override it; a larger size pads, a smaller one
  clips.
- `--crop-rows a:b` keeps rows `a` to `b-1`, 0-based like a Python slice;
  either end may be left out or negative (`3:`, `:-2`). The whole capture is
  replayed first, so the cropped region keeps colors set on earlier lines.
  The width then follows the region.
- `--title` adds a window bar with the given text above the frame, useful to
  say which host a frame came from. It works in the binary mode as well;
  without it the page is exactly the README one.
- The Chrome window is fitted to the frame. `--window W,H` overrides it.

The renderer understands what `capture-pane -e` emits: the 16 basic colors
(mapped to the Tokyo Night palette), 256 colors and truecolor, in the
semicolon or the colon form, plus bold, dim, italic, underline and reverse.
Dim blends the foreground toward the cell background, the way terminals draw
faint text. Cells with the default colors take the frame's foreground and
background.
