# What "stable" means

Every tool in the family starts as beta. A tool is promoted to stable once, by
a pull request that points at the evidence, and every tool is measured by the
same bar: the one below. It lives in the kit so that no tool writes its own.

## The bar

A tool is stable when all seven hold, and its README links the evidence for
each one.

1. Version 1.0.0 or later. From then on the tool follows semver.
2. The contract is frozen: the keys, the flags and the `--check` JSON. A minor
   release only adds fields, keys and flags. Removing one, or changing what it
   means, takes a major release, and the minor before that major warns about
   it both on screen and in `--check`.
3. Every install channel is live: apt, dnf and pacman through pkgs.tui.tools,
   and the static binary. An upgrade from one release to the next through the
   package manager has been run at least once.
4. Compatibility evidence: the tool's own smoke test has passed in tui-lab on
   at least one apt, one dnf and one pacman distribution, and the runs are
   recorded in `compat/results.jsonl`.
5. A real case: the tool has driven a real machine end to end for its main
   purpose, and whatever that surfaced was fixed or filed as an issue.
6. The security gate is green: CodeQL, Scorecard, govulncheck and gitleaks,
   and the release carries signed checksums and build provenance.
7. A guide on tui.tools walks through the tool's main task.

The first point is about the release, the other six about the tool. None of
them is waived for a small tool: a tool too small to meet the bar stays beta,
which says exactly what it is.

## What the manifest says

`tool.json` carries the result, so the README, the website and the binary all
read one answer:

```json
"stability": "stable",
"stableSince": "1.0.0"
```

Both fields are optional. A manifest without `stability`, or with `"beta"`, is
beta. `stableSince` is the first stable release, without the leading `v`; it is
required with `"stable"` and refused without it. See
[`tool-manifest.md`](tool-manifest.md#stability).

`tools/render-install.py` renders the README banner from it, between
`<!-- stability:start -->` and `<!-- stability:end -->`:

- beta keeps the family's note, "Beta. The family is days old and still
  changing...";
- stable renders "Stable since v1.0.0." with one line on what semver promises
  and a link to this page.

## What is checked, and where

- The schema refuses `"stable"` without `stableSince`, a `stableSince` below
  1.0.0, and a `stableSince` on a beta manifest. `make manifest` in a tool
  runs it.
- `render-install.py` repeats those rules for a checkout that never runs the
  schema, and adds the one only the repository can answer: the latest tag must
  be 1.0.0 or later and at least `stableSince`. A stable claim always points at
  a release that exists. In a clone without tags (a shallow CI checkout) that
  part is skipped and the release is the reviewer's check.
- A stable tool must carry the stability markers in its README, or the render
  fails: a hand-written beta note would otherwise contradict the manifest.

Points 2 to 7 of the bar are not machine-checkable. They are what the
promotion pull request shows.

## How to promote a tool

1. Cut the release that is the first stable one, `v1.0.0`, through the usual
   branch, pull request and tag from `main`. It still says beta, and that is
   right: nothing has been claimed yet.
2. Open a pull request in the tool that sets `"stability": "stable"` and
   `"stableSince": "1.0.0"`, adds the stability markers where the beta note
   was if the README does not have them yet, and runs `make readme`.
3. In that pull request's description, link one piece of evidence per point
   of the bar: the release, the contract section of the README, the package
   upgrade run, the `compat/results.jsonl` lines, the real-case issue or
   report, the green security runs and the release's provenance, and the guide
   on tui.tools.
4. Merge with CI green. The website and the README pick up the banner on
   their next build; the next release ships it.

A promotion is not reversed silently. If a stable tool breaks its contract,
the fix is a major release that follows point 2, not a return to beta.
