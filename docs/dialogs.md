# Dialogs

`ui.Confirm`, `ui.Input`, `ui.Picker` and `ui.FilePicker` are the dialogs every
tool in the family uses. They are values the host model stores and forwards key
messages to, not Bubble Tea models, so a tool keeps its own update loop.

Nothing here is opt-in. A tool that bumps the kit gets the wrapping, the
scrolling and the picker filter without touching a line of its own code.

## Nothing is cut

The confirm dialog is where the family's promise lives: the command in the box
is the command that runs. A preview clipped at the dialog's edge breaks that
promise, because a command line the user cannot read to its end is a command
line they cannot check.

So the dialogs wrap, they never truncate:

- **Prose** — `Confirm.Body`, `Input.Help` — is word-wrapped to the dialog's
  inner width, runs of spaces folded into one. Blank lines are kept, because
  they are the paragraph breaks the body author wrote.
- **Pre-formatted lines** are wrapped too, with their continuations indented
  under the first line so the eye still reads them as one thing, and their
  spacing is kept exactly as written: a line only breaks at a run of spaces,
  never inside one. Three shapes are recognised: a line starting with `$ ` (the
  command preview), a line starting with two spaces (an indented block) and a
  unified-diff line (`+ `, `- `, `@@`, `+++ `, `--- `). In a diff of a YAML
  file the spaces after the marker are the file's indentation, which is its
  meaning, so the change the user approves reads as the file that is written.

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
lines := ui.WrapBody(body, 40)           // multi-line, keeps "$ ", indented and diff lines
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

## The file picker

`ui.FilePicker` is for the paths a tool asks for: a certificate and its key, an
SSH key, a share directory, a script, an export target. It lists a directory
under a path field, and the same field is where a path is typed or pasted,
which is also the way through on a terminal too narrow to browse comfortably.

```
│  Certificate for the TLS listener                     │
│  /etc/ssl/certs                                       │
│                                                       │
│   > ca.pem                                            │
│     server.crt                                        │
│                                                       │
│  PEM, readable by the service account.                │
│                                                       │
│  ↑/↓ move    enter open/select    ⌫/h up    . hidden  │
│  / type path    esc cancel                            │
```

```go
a.picker = ui.NewFilePicker(ui.FilePickerOptions{
    Title:      "Certificate for the TLS listener",
    Help:       "PEM, readable by the service account.",
    Start:      cfg.TLSCertPath, // a file opens its directory with it highlighted
    Extensions: []string{".pem", ".crt"},
})

// In Update, while the picker is open:
cmd, _ := a.picker.Update(msg)
if a.picker.Done {
    if a.picker.Accepted {
        path := a.picker.Value() // absolute and clean
        // validate it as before: exists, readable by the service account
    }
}

// In View:
a.picker.View(theme, width, height)
```

| Option | What it does |
| --- | --- |
| `Start` | A directory, or a file (opens its directory with the file highlighted). A path that is gone opens its nearest existing parent. Empty is the working directory. |
| `Extensions` | Lists and accepts only these files, `.pem` or `pem`, case-insensitively. Directories are always listed. |
| `ShowHidden` | Lists dot files from the start. |
| `DirsOnly` | Picks a directory. The list starts with a `./` row that chooses the directory being listed. |
| `NewFile` | Accepts a typed path that does not exist yet, as long as its directory does. The zero value requires the path to exist. |
| `Home` | What a typed `~` expands to. Defaults to the user's home on the real filesystem. |
| `FS` | The filesystem to read, see below. Nil is the real one. |

| Key | What it does |
| --- | --- |
| `↑` `↓` `pgup` `pgdn` `home` `end` (and `j` `k` `g` `G`) | move the cursor |
| `enter` | on a file, chooses it; on a directory, opens it (a directory picker chooses it) |
| `→` `l` | opens the highlighted directory |
| `backspace` `h` `←` | goes up one directory, with the cursor on the one just left |
| `.` | shows or hides dot files |
| `/` `tab` | moves to the path field, filled with the listed directory |
| `~` | moves to the path field at the home directory |
| a paste | lands in the path field, whatever had the focus |
| `esc` | cancels; in the path field, goes back to the list |
| `ctrl+c` | always cancels |

In the path field, `enter` uses what was typed: a relative path is taken from
the listed directory, a directory is opened (a directory picker chooses it)
and a file is chosen. What the options rule out, a missing file, a new file in
a directory that does not exist, the wrong extension, is said on the line
under the list and the field stays as typed, ready to fix.

A directory the tool cannot read is listed as such, "cannot read this
directory: permission denied", instead of closing the dialog. Backspace goes
back up, and a path inside it can still be typed.

The picker reads the filesystem to list it, the way `Input` reads keystrokes:
the family's exec-site rule is about mutations, and the picker performs none.
It checks only what it needs to list and select. The tool keeps validating the
result as it did when the path was a typed `ui.Input`.

### A fake tree for `--demo` and tests

`FS` takes anything with `ReadDir` and `Stat` over absolute paths.
`ui.FileSystemFromFS` adapts an `fs.FS`, so an `fstest.MapFS` is a whole demo
tree, and a screenshot never shows the machine it was rendered on:

```go
tree := fstest.MapFS{
    "etc/ssl/certs/ca.pem":       {Data: []byte("demo")},
    "etc/ssl/private/server.key": {Data: []byte("demo")},
}
opts.FS = ui.FileSystemFromFS(tree)
```

The listing is the kit's own rather than `bubbles/filepicker`'s: that model
reads `os.ReadDir` directly, so it cannot list a fake tree, and it drops a read
error without showing it. The path field is a `bubbles/textinput`, as in
`Input`.
