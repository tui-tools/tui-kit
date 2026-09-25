#!/usr/bin/env python3
r"""Render a tool README's Install section from its tool.json.

The manifest is the single source for how a tool is installed: the family
website reads it, and so does the README, so the two cannot disagree. This
script replaces everything between the markers

    <!-- install:start -->
    <!-- install:end -->

with one subsection per package manager declared in `install`, in a fixed
order, marking the channels that are not published yet.

    tui-kit/tools/render-install.py --manifest tool.json --readme README.md

Without --check it rewrites the README in place; with --check it only reports
whether the file is already up to date, which is what CI runs.

`{version}` and `{arch}` in a command or a pattern are expanded: the version
comes from --version, or from the repository's latest tag, or falls back to
the placeholder itself when there is no tag yet.

The same run owns the stability banner at the top of the README, between

    <!-- stability:start -->
    <!-- stability:end -->

It renders the family's "Beta." note for a manifest without `stability` (or
with `"beta"`), and a "Stable since vX.Y.Z" line for `"stable"`. The markers
are optional while a tool is beta, so a README with a hand-written banner keeps
working; a stable tool must carry them, or the README would keep calling it
beta. The bar a tool meets before it says `stable` is docs/stability.md. A
stable claim whose tag does not exist yet (the promotion PR, rendered with
--version 1.0.0 or before the tag) is only a warning.
"""
from __future__ import annotations

import argparse
import json
import pathlib
import re
import subprocess
import sys
import textwrap

START = "<!-- install:start -->"
END = "<!-- install:end -->"

STABILITY_START = "<!-- stability:start -->"
STABILITY_END = "<!-- stability:end -->"

# Where the bar a stable tool has met is written down. The stable banner links
# it, so a reader can see what the word promises.
STABILITY_DOC = "https://github.com/tui-tools/tui-kit/blob/main/docs/stability.md"

# The family's beta note, word for word as the READMEs carried it by hand.
BETA_BANNER = """> **Beta.** The family is days old and still changing. Package names, flags
> and keys may move without notice until 1.0. Pin versions, and report what
> breaks."""

# The order the channels are presented in, and how each one is titled. A
# reader picks the row that matches their machine, so the distributions come
# first and the escape hatches last.
CHANNELS = [
    ("pacman", "Arch Linux", "sh"),
    ("aur", "Arch Linux (AUR)", "sh"),
    ("apt", "Debian and Ubuntu", "sh"),
    ("dnf", "Fedora and RHEL", "sh"),
    ("zypper", "openSUSE", "sh"),
    ("binary", "Any distribution, static binary", "sh"),
    ("source", "From source", "sh"),
]

# The family website. Every rendered README points back at it, so it lives
# in one place here.
SITE_URL = "https://tui.tools"
SETUP_URL = f"{SITE_URL}/install/"

# The one-time repository setup, by hand, per package manager. `{repo}` is the
# manifest's `install.repo_url`. These are printed next to the one-liner
# because a family whose promise is "preview before you run" cannot only offer
# `curl … | sh`: the reader who wants to see every command gets them here.
#
# The commands are deliberately plain: fetch the public key, put it where the
# package manager looks for repository keys, point the manager at the
# repository, refresh. Nothing is executed to produce them, so the rendered
# README is the same on every machine.
MANUAL_SETUP = {
    "apt": """sudo install -d -m 0755 /etc/apt/keyrings
curl -fsSL {repo}/pubkey.asc \\
  | sudo gpg --dearmor -o /etc/apt/keyrings/tui-tools.gpg
echo "deb [signed-by=/etc/apt/keyrings/tui-tools.gpg] {repo}/deb stable main" \\
  | sudo tee /etc/apt/sources.list.d/tui-tools.list
sudo apt update""",
    "dnf": """sudo rpm --import {repo}/pubkey.asc
sudo curl -fsSL -o /etc/yum.repos.d/tui-tools.repo {repo}/rpm/tui-tools.repo
sudo dnf makecache""",
    "pacman": """curl -fsSL -o /tmp/tui-tools.asc {repo}/pubkey.asc
sudo pacman-key --add /tmp/tui-tools.asc
sudo pacman-key --lsign-key \\
  "$(gpg --show-keys --with-colons /tmp/tui-tools.asc | awk -F: '/^fpr:/{{print $10; exit}}')"
printf '[tui-tools]\\nServer = {repo}/arch/$arch\\n' \\
  | sudo tee -a /etc/pacman.conf
sudo pacman -Sy""",
}


def latest_version(repo_dir: pathlib.Path) -> str | None:
    """The newest tag in the repository, without its leading v."""
    try:
        out = subprocess.run(
            ["git", "describe", "--tags", "--abbrev=0"],
            cwd=repo_dir,
            capture_output=True,
            text=True,
            check=True,
        ).stdout.strip()
    except (subprocess.CalledProcessError, FileNotFoundError):
        return None
    return out[1:] if out.startswith("v") else out or None


def wrap(text: str) -> list[str]:
    """Wrap prose the way the family's markdown is written: 79 columns."""
    return textwrap.wrap(text, width=79, break_long_words=False, break_on_hyphens=False)


def expand(text: str, version: str | None, arch: str) -> str:
    """Fill the {version} and {arch} placeholders a manifest command carries."""
    if version:
        text = text.replace("{version}", version)
    return text.replace("{arch}", arch)


def render(manifest: dict, version: str | None, arch: str) -> str:
    """Build the markdown that sits between the two markers."""
    install = manifest.get("install", {})
    name = manifest["name"]
    repo_url = install.get("repo_url")

    lines: list[str] = []
    lines.append("<!-- Generated by tui-kit/tools/render-install.py from tool.json. -->")
    lines.append("<!-- Edit the manifest, then run `make readme`. -->")

    # What works today comes first; the channels that are still coming are
    # kept, in the same order, under their own heading.
    declared = [(k, t, l) for k, t, l in CHANNELS if k in install]
    ready = [c for c in declared if install[c[0]]["available"]]
    coming = [c for c in declared if not install[c[0]]["available"]]

    def section(channel: tuple[str, str, str]) -> None:
        key, title, lang = channel
        method = install[key]
        suffix = "" if method["available"] else " — coming soon"

        lines.append("")
        lines.append(f"### {title}{suffix}")
        lines.append("")

        if method.get("requires_repo_setup"):
            lines.extend(
                wrap(
                    "Needs the tui-tools repository, which is a "
                    f"[one-time setup]({SETUP_URL})."
                )
            )
            lines.append("")

            manual = MANUAL_SETUP.get(key)
            if repo_url and manual:
                lines.extend(
                    wrap(
                        "The one-liner detects the distribution and adds the "
                        "repository and its signing key:"
                    )
                )
                lines.append("")
                lines.append("```sh")
                lines.append(f"curl -fsSL {repo_url}/install.sh | sh")
                lines.append("```")
                lines.append("")
                lines.extend(
                    wrap(
                        "Piping a script into a shell is not this family's "
                        "style, so here is the same setup by hand — read it, "
                        "or read the script first with "
                        f"`curl -fsSL {repo_url}/install.sh -o install.sh`:"
                    )
                )
                lines.append("")
                lines.append("```sh")
                lines.extend(manual.format(repo=repo_url).split("\n"))
                lines.append("```")
                lines.append("")
                lines.extend(wrap("Then, and for every other tool in the family:"))
                lines.append("")

        lines.append(f"```{lang}")
        lines.extend(expand(method["command"], version, arch).split("\n"))
        lines.append("```")

        note = method.get("note")
        if note:
            lines.append("")
            lines.extend(wrap(note))

    for channel in ready:
        section(channel)

    if coming:
        lines.append("")
        # This sits under whatever is already installable, so it can only
        # speak about the channels below it. It used to announce that the
        # distribution packages were unpublished, which read as a
        # contradiction once apt, dnf and pacman were live above it.
        lines.extend(
            wrap(
                "Not packaged for these yet; the static binary works "
                "everywhere in the meantime."
            )
        )
        for channel in coming:
            section(channel)

    lines.append("")
    lines.append("### Verify a download")
    lines.append("")
    lines.extend(
        wrap(
            f"Every release of `{name}` ships a `checksums.txt`. Check an archive "
            "against it before installing:"
        )
    )
    lines.append("")
    lines.append("```sh")
    lines.append("sha256sum -c checksums.txt --ignore-missing")
    lines.append("```")

    # The tool's own page on the family website, so every README links it.
    lines.append("")
    lines.append(f"Website: {SITE_URL}/tools/{name}/")

    return "\n".join(lines)


def parse_version(text: str) -> tuple[int, int, int] | None:
    """A plain MAJOR.MINOR.PATCH as a tuple, or None for anything else.

    A pre-release or build suffix (1.0.0-rc.1) is not a release a stable
    claim can rest on, so it parses to None and the check treats it as
    unknown rather than guessing its order.
    """
    match = re.fullmatch(r"v?(\d+)\.(\d+)\.(\d+)", text.strip())
    if not match:
        return None
    return tuple(int(part) for part in match.groups())


def stability_errors(manifest: dict) -> list[str]:
    """What is wrong with the manifest's stability claim, if anything.

    The schema already refuses a `stableSince` below 1.0.0, a `stable`
    without one and a `stableSince` on a beta manifest; this repeats those
    rules for a checkout that never runs the schema. Whether the release
    exists yet is not an error: see stability_warnings.
    """
    stability = manifest.get("stability", "beta")
    since = manifest.get("stableSince")
    if stability not in ("beta", "stable"):
        return [f'stability must be "beta" or "stable", not {stability!r}']
    if stability == "beta":
        if since is not None:
            return ["stableSince is only allowed with \"stability\": \"stable\""]
        return []

    if since is None:
        return ['"stability": "stable" needs "stableSince", the first stable release']
    parsed = parse_version(since)
    if parsed is None or parsed[0] < 1:
        return [
            f"stableSince {since!r} is not a 1.0.0-or-later release: "
            "a 0.x tool is beta by definition"
        ]
    return []


def stability_warnings(manifest: dict, version: str | None) -> list[str]:
    """Notes on a stable claim whose release is not tagged yet.

    The README documents a state in the same pull request that finishes it,
    so the promotion PR sets `stable` before the v1.0.0 tag exists: the tag
    then carries "Stable since v1.0.0". A latest tag below `stableSince` is
    therefore a pending promotion, reported but never an error. `version` is
    the latest tag or --version; unknown (a shallow clone) says nothing.
    """
    if manifest.get("stability") != "stable" or version is None:
        return []
    since = parse_version(manifest.get("stableSince", ""))
    latest = parse_version(version)
    if since is None or latest is None or latest >= since:
        return []
    return [
        f"stable since v{manifest['stableSince']}, pending that tag "
        f"(latest is {version})"
    ]


def render_stability(manifest: dict) -> str:
    """The banner between the stability markers."""
    if manifest.get("stability", "beta") != "stable":
        return BETA_BANNER
    since = manifest["stableSince"]
    # 77 columns: the family's 79 minus the "> " every quoted line carries.
    lines = textwrap.wrap(
        f"**Stable since v{since}.** Keys, flags and the `--check` JSON follow "
        "semver: anything new arrives in a minor release, and a removal or a "
        "change of meaning waits for the next major, announced one minor "
        f"before. What stable means: [the family's bar]({STABILITY_DOC}).",
        width=77,
        break_long_words=False,
        break_on_hyphens=False,
    )
    return "\n".join("> " + line for line in lines)


def splice_stability(readme: str, banner: str, required: bool) -> str:
    """Put the banner between the stability markers.

    A README without the markers is left alone unless the banner is required,
    which is the case for a stable tool: its hand-written beta note would
    otherwise contradict the manifest.
    """
    pattern = re.compile(
        re.escape(STABILITY_START) + r".*?" + re.escape(STABILITY_END), re.DOTALL
    )
    if not pattern.search(readme):
        if required:
            raise SystemExit(
                f"no {STABILITY_START} … {STABILITY_END} markers in the README: "
                "a stable tool renders its banner, add them where the Beta note was"
            )
        return readme
    return pattern.sub(
        lambda _: f"{STABILITY_START}\n{banner}\n{STABILITY_END}", readme, count=1
    )


def splice(readme: str, body: str) -> str:
    """Put the rendered body between the markers, leaving the rest alone."""
    pattern = re.compile(
        re.escape(START) + r".*?" + re.escape(END), re.DOTALL
    )
    if not pattern.search(readme):
        raise SystemExit(
            f"no {START} … {END} markers in the README: add them around the "
            "Install section first"
        )
    # A function, not a string: re.sub reads backslash escapes in a
    # replacement string, and an install command legitimately contains them
    # (`printf '…\n…'`, a line continuation). A callable is substituted
    # verbatim.
    return pattern.sub(lambda _: f"{START}\n{body}\n{END}", readme, count=1)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", default="tool.json", type=pathlib.Path)
    parser.add_argument("--readme", default="README.md", type=pathlib.Path)
    parser.add_argument(
        "--version",
        default=None,
        help="version to expand {version} with; defaults to the latest tag",
    )
    parser.add_argument(
        "--arch",
        default="amd64",
        help="architecture to expand {arch} with in the README (default: amd64)",
    )
    parser.add_argument(
        "--check",
        action="store_true",
        help="do not write; exit non-zero when the README is out of date",
    )
    args = parser.parse_args()

    manifest = json.loads(args.manifest.read_text())
    version = args.version or latest_version(args.manifest.resolve().parent)
    body = render(manifest, version, args.arch)

    errors = stability_errors(manifest)
    if errors:
        for error in errors:
            print(f"{args.manifest}: {error}", file=sys.stderr)
        return 1
    for warning in stability_warnings(manifest, version):
        print(f"{args.manifest}: warning: {warning}", file=sys.stderr)

    current = args.readme.read_text()
    updated = splice(current, body)
    updated = splice_stability(
        updated,
        render_stability(manifest),
        required=manifest.get("stability") == "stable",
    )

    if args.check:
        if current != updated:
            print(f"{args.readme} is out of date: run `make readme`", file=sys.stderr)
            return 1
        print(f"{args.readme} is up to date")
        return 0

    if current != updated:
        args.readme.write_text(updated)
        print(f"wrote the generated sections of {args.readme}")
    else:
        print(f"{args.readme} already up to date")
    return 0


if __name__ == "__main__":
    sys.exit(main())
