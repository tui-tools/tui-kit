# The tool manifest

Every tool in the family carries a `tool.json` at the root of its repository. It
is what the family website — [tui-tools.github.io](https://tui-tools.github.io)
— reads to build the tool's card and its detail page. Nothing about a tool is
described twice: the manifest holds the facts, the release holds the binaries,
and the site joins them.

The schema is [`schema/tool.schema.json`](../schema/tool.schema.json), JSON
Schema draft 2020-12. Validate a manifest with:

```sh
npx --yes ajv-cli@5 validate --spec=draft2020 -c ajv-formats \
  -s schema/tool.schema.json -d tool.json
```

`tui-template` runs exactly that in CI, so a tool started from the template
keeps its manifest honest from the first commit.

## Why a manifest and not the README

The README is written for a person reading the repository. The card on the
website needs a tagline that fits in one line, a category to sit under, an icon
at a known path and a security posture it can render as a checklist. Parsing
that out of prose would be guesswork; declaring it is three minutes of work per
tool and it never drifts.

## The fields

| Field | Required | What it is |
| --- | --- | --- |
| `schemaVersion` | yes | `1`. Bumped only for a breaking change. |
| `name` | yes | `tui-<target>`, the repository name. |
| `binary` | yes | The installed executable. Same as `name` in this family. |
| `tagline` | yes | One line, 80 characters at most, no trailing period. It is the card's subtitle. |
| `description` | yes | A short paragraph or two for the detail page. |
| `category` | yes | One shelf from the enum: `firewall`, `systemd`, `network`, `ssh`, `storage`, `snapshots`, `scheduling`, `packages`, `containers`, `processes`, `users`, `security`, `hardware`, `logs`, `library`, `template`, `other`. |
| `homepage`, `repo` | yes | URLs. `repo` must be the `https://github.com/owner/name` form. |
| `license` | yes | SPDX identifier, `MIT` for everything here. |
| `platforms` | yes | The `os/arch` pairs a release ships, e.g. `linux/amd64`. |
| `icon` | yes | Repository-relative path, SVG preferred. The site copies it. |
| `logo` | no | The horizontal lockup, if the repository has one. |
| `screenshots` | no | Ordered `{path, caption}`. **The first is the card thumbnail**, so make it the opening screen. |
| `keys` | no | The shortcuts worth advertising as `{key, action}`, not the whole help screen. |
| `backends` | no | The system backends the tool drives, and what it knows about their versions, see below. |
| `install` | yes | One entry per channel, see below. |
| `security` | yes | The family's promises, answered for this tool, see below. |
| `maintainers` | yes | `{name, github?, url?}`. |
| `since` | yes | `YYYY-MM-DD`, when the tool was first published. |
| `keywords` | no | Extra search terms. The repository topics are a good source. |
| `unreleased` | no | `true` for a tool that deliberately has no tag, such as the template. |

`description` is a safe subset of markdown: paragraphs, `code`, `**bold**`,
`_italic_` and `[links](url)`. No headings, no images, no HTML — the site
renders it inside a page it already owns the typography of.

### `install`

One entry per **package manager**, because that is how a reader installs
things: `pacman`, `aur`, `apt`, `dnf`, `zypper`, `binary` and `source`. Every
entry is optional and every one has the same shape:

```json
{
  "available": false,
  "package": "tui-firewall",
  "repo": "tui-tools",
  "requires_repo_setup": true,
  "command": "sudo pacman -S tui-firewall",
  "pattern": "tui-firewall_{version}_linux_{arch}.tar.gz",
  "url": "https://pkgs.tui.tools/arch",
  "note": "One line of context."
}
```

| Field | Meaning |
| --- | --- |
| `available` | Required. `true` when it can be installed today. `false` renders as **coming soon** — with the command still shown, so a reader knows what it will be. |
| `command` | Required, always. The line a reader copies once the repository is set up. Multi-line is fine: separate the lines with `\n`. |
| `package` | The package name in that manager's namespace, when it differs from the tool name — an AUR `-bin` package, say. |
| `repo` | Where the package comes from: a repo id (`tui-tools`) or `aur`. |
| `requires_repo_setup` | `true` when the command only works after the family repository has been added. The site links to `/install/` for that one-time setup. |
| `pattern` | Release asset name, for the `binary` channel. |
| `url` | Where the package lives, or where it will. |
| `note` | One line under the command. |

**A channel that does not exist yet is declared, not omitted.** The distro
packages are coming; saying `"available": false` with the real command is more
useful than an empty section, and it is one boolean to flip when the repository
at `pkgs.tui.tools` goes live. The website's distro selector shows every
declared channel and marks the unavailable ones.

`{version}` (no leading `v`) and `{arch}` are expanded against the latest tag,
in both `command` and `pattern`, so download lines keep working without anyone
editing the manifest.

The same block generates the **Install** section of the tool's README, through
[`tools/render-install.py`](../tools/render-install.py), so the repository and
the website never disagree about how to install the thing.

### `backends`

Optional, and the only block a **running binary** reads: the tool embeds its
own `tool.json`, probes the backend at startup through
[`tui-kit/compat`](../compat) and shows the version in its header. That is why
the version numbers live here and not in the code — the README, the website and
the binary would otherwise each carry their own copy.

A worked example, `tui-firewall`'s:

```json
"backends": [
  {
    "name": "ufw",
    "binary": "ufw",
    "versionCommand": ["ufw", "--version"],
    "versionRegex": "ufw ([0-9][0-9.]*)",
    "minimum": "0.36",
    "searchPaths": ["/usr/sbin/ufw", "/sbin/ufw"],
    "tested": ["0.36.1", "0.36.2"],
    "features": [
      { "name": "numbered-status", "since": "0.31" }
    ],
    "notes": [
      {
        "range": "<0.36",
        "impact": "`status numbered` omits the app profile column, so rules added from a profile show their ports instead of the profile name"
      }
    ]
  }
]
```

| Field | Required | Meaning |
| --- | --- | --- |
| `name` | yes | How the backend is known to the user: `ufw`, `systemd`. |
| `binary` | yes | The executable that is probed and driven. |
| `versionCommand` | yes | The argv that prints the version, `argv[0]` included. Run once at startup, unprivileged, with a two second timeout. |
| `versionRegex` | no | Extracts the version from that output; a capturing group is the version. Omit it to take the first version-shaped token, which is what nearly every backend prints. |
| `minimum` | no | The oldest version the tool claims to work with. Below it the header says so in the error colour — the tool still runs. |
| `tested` | **generated** | The versions a real run passed against. `tui-kit/tools/compat-sync.py` rewrites it from `compat/results.jsonl`; do not edit it by hand. |
| `searchPaths` | no | Absolute fallbacks for a binary a plain `PATH` misses, such as `/usr/sbin/ufw`. |
| `features` | no | `{name, since}` capabilities the tool asks for by name — `caps.Has("timers")` — instead of comparing versions in code. |
| `notes` | no | `{range, impact}`: a simple constraint (`<0.11`, `>=250`, `0.36.x`, or a comma-separated list of those) and one sentence on what changes for the user. |

`minimum` is a claim about the tool, `tested` is a record of runs, and the two
are deliberately different things: a tool can support 0.36 upwards and have
been run against exactly two of those releases. The header tells the user which
of the two they are in.

The whole loop — where the evidence comes from, how `tested` is regenerated and
how the README section is rendered — is
[`docs/compatibility.md`](compatibility.md).

### `security`

This block is the reason the manifest exists. The website renders it as a
checklist on both `/security/` and the tool page, and a reader should be able to
answer "what can this thing do to my machine" without opening the source.

| Field | What it means |
| --- | --- |
| `needs_sudo` | A sentence naming what the tool escalates **for**, or `false` when it never does. Not a boolean dressed as prose: say which operations. |
| `preview_before_run` | Must be `true`. It is the family contract: every mutation is shown as an exact command line and confirmed first. A tool that cannot say `true` here does not belong in the family. |
| `no_daemon` | Nothing keeps running after you quit, and nothing is installed to run later. |
| `no_network` | The tool opens no network connection of its own. When `false`, `no_network_note` is required and must say what it talks to and why. |
| `static_binary` | Released statically linked, no runtime dependencies. |
| `signed_releases` | A signature beyond the SHA-256 `checksums.txt`. `false` today for every tool; the site says so plainly rather than hiding it. |
| `config_paths` | Every path the tool reads or writes for its own configuration, in precedence order. |

`signed_releases: false` being visible on the site is on purpose. A security
page that only lists the good answers is marketing; this one lists the answer.

## Adding a manifest to a new tool

1. Copy `tool.json` from [`tui-template`](https://github.com/tui-tools/tui-template)
   and replace the values. The template's copy is a real, valid manifest, not a
   set of TODOs.
2. Point `icon` at `assets/icon.svg` and `screenshots` at the frames
   `make screenshots` renders — the same files the README embeds.
3. Validate: `make manifest` in the template, or the `npx ajv` line above.
4. Tag `v0.1.0`. The site picks the tool up on its next build: hourly, or
   immediately if the release workflow dispatches
   (see `tui-tools.github.io`'s README).

A repository without a `tool.json` is simply not listed. That is how `tui-kit`
and the org profile stay out of the marketplace grid while living in the same
organization.
