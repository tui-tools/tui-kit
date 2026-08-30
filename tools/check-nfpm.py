#!/usr/bin/env python3
"""Assert that a tool's package metadata matches its manifest.

GoReleaser builds the .deb, .rpm and pacman packages from `nfpms` in
.goreleaser.yaml, and it cannot read tool.json: the package description has
to be written out a second time. Two places holding the same sentence drift,
so this script is the thing that stops them.

    tui-kit/tools/check-nfpm.py --manifest tool.json --config .goreleaser.yaml

It checks the fields the family fixed by convention, not just the
description: the package name is the tool name, the maintainer and homepage
are the family's, the licence is MIT, the binary goes to /usr/bin, all three
formats and both architectures are built, and README and LICENSE are
installed under /usr/share/doc/<name>/.

Exits non-zero, with one line per problem, when anything disagrees. `make
manifest` runs it, and so does CI.
"""
from __future__ import annotations

import argparse
import json
import pathlib
import sys

try:
    import yaml
except ImportError:  # pragma: no cover - depends on the environment
    print(
        "check-nfpm.py needs PyYAML: pip install pyyaml "
        "(or apt-get install python3-yaml)",
        file=sys.stderr,
    )
    raise SystemExit(2)

MAINTAINER = "tui-tools <maintainers@tui.tools>"
VENDOR = "tui-tools"
LICENSE = "MIT"
BINDIR = "/usr/bin"
FORMATS = {"deb", "rpm", "archlinux"}
GOARCHES = {"amd64", "arm64"}
HOMEPAGE = "https://tui.tools/tools/{name}/"


def problems(manifest: dict, config: dict) -> list[str]:
    """Every disagreement between the manifest and the GoReleaser config."""
    found: list[str] = []
    name = manifest["name"]
    tagline = manifest["tagline"]

    nfpms = config.get("nfpms") or []
    if len(nfpms) != 1:
        return [
            f"expected exactly one nfpms entry, found {len(nfpms)}: a tool is "
            "one binary and one package"
        ]
    pkg = nfpms[0]

    def want(field: str, expected, actual) -> None:
        if actual != expected:
            found.append(f"nfpms[0].{field} is {actual!r}, expected {expected!r}")

    # The one that actually drifts, and the reason this script exists.
    want("description", tagline, pkg.get("description"))

    want("package_name", name, pkg.get("package_name"))
    want("maintainer", MAINTAINER, pkg.get("maintainer"))
    want("vendor", VENDOR, pkg.get("vendor"))
    want("license", LICENSE, pkg.get("license"))
    want("homepage", HOMEPAGE.format(name=name), pkg.get("homepage"))
    want("bindir", BINDIR, pkg.get("bindir"))

    formats = set(pkg.get("formats") or [])
    if formats != FORMATS:
        found.append(
            f"nfpms[0].formats is {sorted(formats)}, expected {sorted(FORMATS)}"
        )

    # A package that claims the command has to conflict with anything else
    # shipping it, or two installs quietly overwrite one another.
    if name not in (pkg.get("provides") or []):
        found.append(f"nfpms[0].provides does not list {name!r}")
    if f"{name}-bin" not in (pkg.get("conflicts") or []):
        found.append(f"nfpms[0].conflicts does not list {name + '-bin'!r}")

    # The documentation the package installs. A path per file, so a rename in
    # the repository is caught here rather than in an empty /usr/share/doc.
    docs = {
        entry.get("dst") for entry in (pkg.get("contents") or []) if isinstance(entry, dict)
    }
    for required in (
        f"/usr/share/doc/{name}/README.md",
        f"/usr/share/doc/{name}/LICENSE",
    ):
        if required not in docs:
            found.append(f"nfpms[0].contents does not install {required}")

    # The package is built from the tool's build, for both released targets.
    builds = config.get("builds") or []
    if not builds:
        found.append("no builds: there is nothing to package")
    else:
        arches = set(builds[0].get("goarch") or [])
        if arches != GOARCHES:
            found.append(
                f"builds[0].goarch is {sorted(arches)}, expected {sorted(GOARCHES)}"
            )
        build_id = builds[0].get("id")
        if build_id and build_id not in (pkg.get("ids") or []):
            found.append(f"nfpms[0].ids does not reference the build {build_id!r}")

    # The manifest's own package names have to be the package that is built.
    for channel in ("apt", "dnf", "pacman"):
        method = (manifest.get("install") or {}).get(channel)
        if method and method.get("package", name) != name:
            found.append(
                f"install.{channel}.package is {method['package']!r}, but the "
                f"package built is {name!r}"
            )

    return found


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", default="tool.json", type=pathlib.Path)
    parser.add_argument("--config", default=".goreleaser.yaml", type=pathlib.Path)
    args = parser.parse_args()

    manifest = json.loads(args.manifest.read_text())
    config = yaml.safe_load(args.config.read_text())

    found = problems(manifest, config)
    if found:
        print(f"{args.config} disagrees with {args.manifest}:", file=sys.stderr)
        for line in found:
            print(f"  - {line}", file=sys.stderr)
        return 1

    print(f"{args.config}: package metadata matches {args.manifest}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
