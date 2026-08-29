# tui-tools branding

The family has one mark: a rounded terminal square carrying a green prompt
chevron. What sits to the right of the chevron says which tool it is — a cursor
block for the family itself, a shield for `tui-firewall`, a cog for
`tui-systemd`. The frame never changes, so a new tool is recognisable as family
before anyone reads the name.

<img src="../assets/branding/icon-128.png" width="72" alt="tui-tools">
<img src="../assets/branding/icon-firewall-128.png" width="72" alt="tui-firewall">
<img src="../assets/branding/icon-systemd-128.png" width="72" alt="tui-systemd">

## Colors

Tokyo Night, the same palette the tools default to, so the branding and the
running program agree.

| Role | Hex | Used for |
| --- | --- | --- |
| Background | `#1a1b26` | The terminal square, and the org avatar's field |
| Frame | `#2f334d` | The square's outline |
| Accent green | `#9ece6a` | The prompt chevron, and the `tui-` prefix in a wordmark |
| Accent blue | `#7aa2f7` | The cursor block and every per-tool glyph |
| Foreground | `#c0caf5` | Wordmark text on a dark background |
| Foreground (light) | `#24283b` | Wordmark text on a light background |

Green marks what the family shares, blue marks what a tool adds. That split is
the whole system: in a wordmark the prefix `tui-` is green and the tool name is
foreground, which makes the naming rule visible at a glance.

## Files

Everything in [`assets/branding/`](../assets/branding) is generated. Do not edit
the PNGs by hand:

```sh
make branding          # or: tools/render-branding.py --out assets/branding
```

| File | What it is |
| --- | --- |
| `icon.svg`, `icon-{512,256,128,64}.png` | The family icon, square |
| `icon-firewall.*`, `icon-systemd.*` | Per-tool icons, same frame |
| `logo.svg`, `logo-dark.svg`, `logo-light.svg` | The `tui-tools` horizontal lockup |
| `logo-{firewall,systemd,kit}-{dark,light}.*` | Per-tool lockups |
| `org-avatar.svg`, `org-avatar-500.png` | The GitHub organization avatar, 500×500 |

Each lockup ships as PNG at 640 and 320 px wide. READMEs use the PNG, not the
SVG: the wordmark is live text, and a PNG renders the same on every machine.

Rendering needs one of `rsvg-convert`, `magick` or `inkscape` on PATH.

## Usage

- **Every README opens with the lockup**, at 240 px, linked to nothing. Use the
  dark variant: GitHub renders both themes, and the dark one holds up on white.
- **The icon is for avatars and favicons**, never inline in prose.
- Do not recolor the mark, restretch it, or put the chevron on a background
  other than `#1a1b26`. If a tool needs its own glyph, draw it in `#7aa2f7` on
  the shared frame and add it to `tools/render-branding.py` — that script is the
  source of truth, not the exported files.
- The wordmark font is JetBrains Mono, falling back to DejaVu Sans Mono and then
  the generic monospace. A machine without either still renders a correct
  lockup, just in a different mono face.

## Uploading the org avatar

The GitHub API does not expose organization avatars, so this one is manual:

1. open `https://github.com/organizations/tui-tools/settings/profile`;
2. upload `assets/branding/org-avatar-500.png`;
3. repeat for the `tuitools` name-reservation org.

## A note on the name

The family follows the Omarchy visual style and reads its theme files. It is
**not** part of the Omarchy project, is not endorsed by its maintainers, and
must never use Omarchy's own name or marks in its branding.
