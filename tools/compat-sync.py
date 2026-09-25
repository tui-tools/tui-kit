#!/usr/bin/env python3
r"""Regenerate a tool's `tested` version lists from its evidence file.

A tool's manifest says which backend versions it works with. That claim is
worth something only when it comes from a run: `compat/results.jsonl` is the
evidence, one JSON object per line, appended by the tool's own smoke test
inside a lab guest.

    {"tool":"tui-firewall","backend":"ufw","version":"0.36.2",
     "distro":"ubuntu-24.04","date":"2026-08-29","result":"pass",
     "suite":"smoke"}

This script folds that file into `backends[].tested` in tool.json: only the
`pass` lines count, versions are de-duplicated and sorted oldest first, and a
backend with no passing run gets an empty list rather than a stale one.

The evidence stays raw and the manifest canonical: each recorded version is
read through the backend's `versionRegex` (the pattern the binary's probe
uses) before it goes into `tested`, so `4.19.5-Ubuntu` becomes `4.19.5` when
the probe on that host reports `4.19.5`. A version that cannot be used is
reported on stderr, naming its line, never dropped without a word.

    tui-kit/tools/compat-sync.py --manifest tool.json --results compat/results.jsonl

`--from-log` harvests the evidence out of a lab log first. The smoke test
prints its line prefixed with `compat-result: ` so it survives the trip from
the guest through the lab's per-VM log, and this pulls those lines into the
results file, skipping the ones already recorded:

    tui-kit/tools/compat-sync.py --from-log ../tui-lab/out/results/*/\*.log

With --check nothing is written and a non-zero exit means the manifest is out
of date, which is what CI runs.
"""
from __future__ import annotations

import argparse
import json
import pathlib
import re
import sys

# The prefix a smoke test puts in front of its result line, so it can be found
# again in a log full of PASS/FAIL rows.
LOG_PREFIX = "compat-result:"

# Every field a result line must carry. A line missing one is evidence of
# nothing, so it is refused loudly instead of silently ignored.
REQUIRED = ("tool", "backend", "version", "distro", "date", "result", "suite")

VERSION_RE = re.compile(r"^\d+(\.\d+){0,2}([-+][0-9A-Za-z.]+)?$")

# The version token tui-kit/compat.ParseVersion takes when a backend has no
# versionRegex (compat/version.go, versionRe).
TOKEN_RE = re.compile(r"\d+(?:\.\d+){0,2}(?:[-+][0-9A-Za-z.]+)?")


def first_group(pattern: str) -> str | None:
    """The text of the first capturing group of a regular expression.

    It skips escapes, character classes and non-capturing or flag groups
    (`(?:`, `(?i)`), and returns None when the pattern captures nothing.
    """
    depth = 0
    start = None
    i = 0
    in_class = False
    while i < len(pattern):
        ch = pattern[i]
        if ch == "\\":
            i += 2
            continue
        if in_class:
            if ch == "]":
                in_class = False
        elif ch == "[":
            in_class = True
            # A ] right after [ or [^ is a literal.
            if pattern[i + 1:i + 2] == "^":
                i += 1
            if pattern[i + 1:i + 2] == "]":
                i += 1
        elif ch == "(":
            named = pattern.startswith(("(?P<", "(?<"), i) and not \
                pattern.startswith(("(?<=", "(?<!"), i)
            if start is None and (pattern[i + 1:i + 2] != "?" or named):
                start, depth = i, 0
                if named:
                    i = pattern.index(">", i)
                    start = i
            if start is not None:
                depth += 1
        elif ch == ")" and start is not None:
            depth -= 1
            if depth == 0:
                return pattern[start + 1:i]
        i += 1
    return None


def canonical_version(version: str, pattern: str) -> str:
    """Read a recorded version the way the tool's probe reads its backend.

    The recorded string is what the backend printed as its version, and the
    probe runs the manifest's versionRegex over the whole `--version` output
    (tui-kit/compat.ParseVersion: the first capturing group, else the whole
    match). The regex is tried on the recorded string first. A pattern
    anchored on text around the version (`ufw ([0-9.]+)`) cannot match a
    bare version, so its capturing group is then matched at the start of the
    string on its own. When neither matches, the version is kept as recorded.
    """
    if not pattern:
        match = TOKEN_RE.search(version)
        return match.group(0).strip() if match else version
    try:
        compiled = re.compile(pattern)
    except re.error:
        # compat.ParseVersion falls back to the default token the same way.
        return canonical_version(version, "")
    match = compiled.search(version)
    if match:
        group = match.group(1) if compiled.groups and match.group(1) else None
        return (group or match.group(0)).strip()
    group = first_group(pattern)
    if group is not None:
        try:
            match = re.match(group, version)
        except re.error:
            match = None
        if match and match.group(0):
            return match.group(0).strip()
    return version


def version_key(version: str) -> tuple:
    """Sort key matching tui-kit/compat.Compare: numbers first, then suffix.

    A dash suffix marks a pre-release and sorts before the plain release with
    the same numbers, which is the same loose rule the Go side applies.
    """
    numbers: list[int] = []
    suffix = ""
    rest = version[1:] if version.startswith("v") else version
    i = 0
    while i < len(rest):
        j = i
        while j < len(rest) and rest[j].isdigit():
            j += 1
        if j == i:
            suffix = rest[i:]
            break
        numbers.append(int(rest[i:j]))
        i = j
        if i < len(rest) and rest[i] != ".":
            suffix = rest[i:]
            break
        i += 1
    numbers += [0] * (4 - len(numbers))
    prerelease = 0 if suffix.startswith("-") else 1
    return (tuple(numbers[:4]), prerelease, suffix)


def read_results(path: pathlib.Path) -> list[dict]:
    """Parse the evidence file, refusing a line that is not a full result."""
    if not path.exists():
        return []
    out = []
    for number, line in enumerate(path.read_text().splitlines(), start=1):
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        try:
            entry = json.loads(line)
        except json.JSONDecodeError as err:
            raise SystemExit(f"{path}:{number}: not JSON: {err}") from err
        missing = [field for field in REQUIRED if field not in entry]
        if missing:
            raise SystemExit(f"{path}:{number}: missing {', '.join(missing)}")
        out.append(entry)
    return out


def harvest(logs: list[pathlib.Path], results: pathlib.Path) -> int:
    """Append the result lines found in lab logs, skipping known ones.

    Returns how many lines were added. Identity is the whole record minus the
    date, so re-running the same suite on the same distro and version does not
    grow the file.
    """
    existing = read_results(results)
    seen = {fingerprint(entry) for entry in existing}

    added: list[dict] = []
    for log in logs:
        if not log.exists():
            continue
        for line in log.read_text(errors="replace").splitlines():
            index = line.find(LOG_PREFIX)
            if index < 0:
                continue
            payload = line[index + len(LOG_PREFIX):].strip()
            try:
                entry = json.loads(payload)
            except json.JSONDecodeError:
                print(f"{log}: skipped an unparsable result line", file=sys.stderr)
                continue
            if [field for field in REQUIRED if field not in entry]:
                print(f"{log}: skipped an incomplete result line", file=sys.stderr)
                continue
            key = fingerprint(entry)
            if key in seen:
                continue
            seen.add(key)
            added.append(entry)

    if added:
        results.parent.mkdir(parents=True, exist_ok=True)
        with results.open("a") as handle:
            for entry in added:
                handle.write(json.dumps(entry, sort_keys=True) + "\n")
    return len(added)


def fingerprint(entry: dict) -> tuple:
    """What makes two result lines the same observation."""
    return (
        entry.get("tool"),
        entry.get("backend"),
        entry.get("version"),
        entry.get("distro"),
        entry.get("result"),
        entry.get("suite"),
    )


def tested_versions(results: list[dict], tool: str, backend: str,
                    pattern: str = "") -> list[str]:
    """The passing versions recorded for one backend, read through its
    versionRegex (canonical_version), sorted and de-duplicated."""
    versions = set()
    for entry in results:
        if entry["tool"] != tool or entry["backend"] != backend:
            continue
        if entry["result"] != "pass":
            continue
        recorded = str(entry["version"]).strip()
        version = canonical_version(recorded, pattern)
        if not VERSION_RE.match(version):
            print(f"warning: {backend}: the {entry['distro']} "
                  f"{entry['date']} pass recorded version {recorded!r}, "
                  f"which is not a version tested can hold; left out",
                  file=sys.stderr)
            continue
        versions.add(version)
    return sorted(versions, key=version_key)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", default="tool.json", type=pathlib.Path)
    parser.add_argument(
        "--results",
        default=pathlib.Path("compat/results.jsonl"),
        type=pathlib.Path,
        help="the evidence file (default: compat/results.jsonl)",
    )
    parser.add_argument(
        "--from-log",
        nargs="*",
        default=[],
        type=pathlib.Path,
        help="lab logs to harvest `compat-result:` lines from first",
    )
    parser.add_argument(
        "--check",
        action="store_true",
        help="do not write; exit non-zero when tool.json is out of date",
    )
    args = parser.parse_args()

    if args.from_log:
        if args.check:
            raise SystemExit("--from-log writes evidence, so it cannot be used with --check")
        added = harvest(list(args.from_log), args.results)
        print(f"harvested {added} new result line(s) into {args.results}")

    manifest = json.loads(args.manifest.read_text())
    backends = manifest.get("backends")
    if not backends:
        print(f"{args.manifest} declares no backends, nothing to sync")
        return 0

    results = read_results(args.results)
    changed = False
    for backend in backends:
        versions = tested_versions(results, manifest["name"], backend["name"],
                                   backend.get("versionRegex", ""))
        current = backend.get("tested", [])
        if current == versions:
            continue
        changed = True
        if versions:
            backend["tested"] = versions
        else:
            backend.pop("tested", None)
        print(f"{backend['name']}: tested = {versions or '[]'}")

    if not changed:
        print(f"{args.manifest} is up to date")
        return 0
    if args.check:
        print(f"{args.manifest} is out of date: run `make compat`", file=sys.stderr)
        return 1

    args.manifest.write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + "\n")
    print(f"wrote the tested versions into {args.manifest}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
