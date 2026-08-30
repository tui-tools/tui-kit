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


def tested_versions(results: list[dict], tool: str, backend: str) -> list[str]:
    """The passing versions recorded for one backend, sorted and de-duplicated."""
    versions = set()
    for entry in results:
        if entry["tool"] != tool or entry["backend"] != backend:
            continue
        if entry["result"] != "pass":
            continue
        version = str(entry["version"]).strip()
        if not VERSION_RE.match(version):
            print(f"skipping an unusable version {version!r}", file=sys.stderr)
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
        versions = tested_versions(results, manifest["name"], backend["name"])
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
