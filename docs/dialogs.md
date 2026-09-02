# Dialogs

`ui.Confirm`, `ui.Input` and `ui.Picker` are the three dialogs every tool in the
family uses. They are values the host model stores and forwards key messages
to, not Bubble Tea models, so a tool keeps its own update loop.

Nothing here is opt-in. A tool that bumps the kit gets the wrapping, the
scrolling and the picker filter without touching a line of its own code.

## Nothing is cut

The confirm dialog is where the family's promise lives: the command in the box
is the command that runs. A preview clipped at the dialog's edge breaks that
promise, because a command line the user cannot read to its end is a command
line they cannot check.

So the dialogs wrap, they never truncate:

- **Prose** — `Confirm.Body`, `Input.Help` — is word-wrapped to the dialog's
  inner width. Blank lines are kept, because they are the paragraph breaks the
  body author wrote.
- **Pre-formatted lines** are wrapped too, with their continuations indented
  under the first line so the eye still reads them as one thing. Two shapes are
  recognised: a line starting with `$ ` (the command preview) and a line
  starting with two spaces (an indented block).

```
│  Command to run:                                     │
│  $ sudo -n certbot certonly --non-interactive        │
│    --agree-tos --webroot -w /var/www/html -d         │
│    api.example.com -d www.api.example.com            │
```

Wrapping a command rather than clipping it is a deliberate choice. A shell line
broken across three rows is slightly harder to copy by eye than one that fits;
a shell line whose tail is invisible is impossible to audit, and auditing it is
the entire point of the dialog.

Widths are counted in terminal cells through the same helper the tables use, so
a CJK ideograph or an emoji costs two — the wrap holds for a Japanese unit
description as well as an English one.

The helpers are exported, for the tools that lay out their own panels:

```go
lines := ui.Wrap("a paragraph", 40)      // word wrap, hard-splitting long words
lines := ui.WrapBody(body, 40)           // multi-line, keeps "$ " and indented blocks
```

## Scrolling a tall dialog

A body taller than the terminal used to be cut by it. Now the dialog grows to
the terminal height and the body scrolls under a pinned title and above a
pinned footer, so the keys are never off screen. One line goes to a position
marker:

```
│  Change 3: move the interface eth2 into the trusted  │
│  7-14 of 43   j/k pgup/pgdn scroll                   │
│                                                      │
│  y confirm    n cancel                               │
```

`j`/`down` and `k`/`up` move a line, `pgup`/`pgdn` move ten, `home`/`end` jump
to the ends. A body that fits does not scroll and shows no marker: a dialog
that always fitted renders exactly as it did before.

`Confirm.Scroll` is exported so a tool can reopen a dialog where it left it.
The bounds are measured by the render, which is why `Confirm.View` and
`Picker.View` take a pointer receiver. Every call site of the form
`a.confirm.View(theme, w, h)` keeps compiling unchanged; a call on a composite
literal — `ui.Confirm{…}.View(…)` — does not, and has to name the value first.

## The picker filter

`ui.Picker` has a type-ahead filter. Printable characters narrow the list to
the options that contain what was typed, case-insensitively and anywhere in the
string. That is what makes a three-hundred entry list — every systemd unit on a
server — something a person can drive.

```
│  Follow which unit?                                  │
│  filter: systemd                                     │
│                                                      │
│   > systemd-journald.service                         │
│     systemd-logind.service                           │
│                                                      │
│  ↑/↓ move    type filter    enter select    esc cancel  │
```

| Key | What it does |
| --- | --- |
| any printable character | appends to the filter |
| `backspace` | deletes the last character of the filter |
| `ctrl+u` | clears the filter |
| `esc` | clears the filter when there is one, otherwise cancels |
| `ctrl+c` | always cancels |
| `↑` `↓` `pgup` `pgdn` `home` `end` | move the cursor |
| `enter` | selects the highlighted option |

The cursor stays on its option while that option survives the filter, and falls
to the first match when it does not. `Picker.Visible()` returns what the filter
keeps, in the original order, and `Picker.Cursor` indexes that — which is
`Picker.Options` itself while the filter is empty, so the field means what it
always meant.

### One breaking change for the tools

Because every printable character now goes to the filter, the vi-style
navigation the picker used to accept is gone: `j`, `k`, `g` and `G` no longer
move the cursor and `q` no longer cancels. Arrows, `home`/`end`, `pgup`/`pgdn`,
`enter` and `esc` do all of it.

A tool whose tests drive a picker with `j` has to press `down` instead after
bumping the kit. The dialogs' own footers already advertise the arrows, so
nothing a user reads on screen changed.
