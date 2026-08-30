# Backend compatibility

Every tool in this family is a face on someone else's program. `tui-firewall`
is `ufw`, `tui-systemd` is `systemctl`, `tui-snapper` is `snapper`. Those
programs change: `ufw` grew numbered status, `systemd` grew
`list-timers --output=json` in 250, and a user on an older machine meets the
difference as a screen that is empty for no visible reason.

The family's answer is one block in the manifest, `backends[]`, and one package
in the kit, `compat`, that reads it.

## What a tool does with it

At startup — once, unprivileged, with a two second timeout — the tool runs the
backend's `versionCommand` and classifies what came back:

| Status | Means | In the header |
| --- | --- | --- |
| `tested` | The version is in `tested`, so a real run passed against it | `ufw 0.36.2` |
| `untested` | Usable, but nobody has run the suite against it | `ufw 0.37.0 (untested)`, warning colour |
| `below-minimum` | Older than `minimum` | `ufw 0.35 (below minimum 0.36)`, error colour |
| `unknown` | Binary missing, call failed, or nothing parsable came back | `ufw (version unknown)`, muted |

**A probe never fails a tool.** An unknown version is a header that says so,
not an exit. The tool still runs, and the backend still refuses what it cannot
do, in its own words.

```go
//go:embed tool.json
var manifestJSON []byte

m, _ := manifest.Load(manifestJSON)
backend, _ := m.Backend("ufw")
result := compat.Probe(ctx, backend)

header.Facts = append(header.Facts, ui.CompatFact(theme, result))
```

## Capabilities, not version numbers

`features[]` names the things that appeared in a known version. The tool asks
for them by name:

```go
if a.caps.Has("timers") {
    // the timers view is offered
}
```

instead of writing `if version >= 250` somewhere in the update loop, where the
next person will not find it. An undeclared feature is assumed present — the
manifest lists what is *version-gated*, not everything a backend can do — and
so is a feature on a version the probe could not read, because hiding a working
view over an unparsable version string is the worse failure.

## The evidence file

`tested` is **generated**. It is not a claim, it is a record of runs, and it
lives in the tool's repository at:

```text
compat/results.jsonl
```

One JSON object per line, one line per suite run:

```json
{"backend":"ufw","date":"2026-08-29","distro":"ubuntu-24.04","result":"pass","suite":"smoke","tool":"tui-firewall","version":"0.36.2"}
```

| Field | What it is |
| --- | --- |
| `tool` | The tool the run exercised, matching `name` in the manifest. |
| `backend` | Which declared backend was driven. |
| `version` | The version the tool itself probed, not the one the tester assumed. |
| `distro` | `$(. /etc/os-release; echo $ID-$VERSION_ID)` on the machine that ran it. |
| `date` | `YYYY-MM-DD`. |
| `result` | `pass` or `fail`. Only `pass` reaches `tested`; a `fail` line is kept, because a version that broke is worth remembering. |
| `suite` | Which suite produced it, `smoke` today. |

The lines are written by the tool's own `test/smoke.sh` while it runs inside a
[tui-lab](https://github.com/tui-tools/tui-lab) guest, so the version recorded
is the one that machine really had. The smoke test also prints the line to
stdout behind a `compat-result:` prefix, which is how it survives the trip out
of the guest and into the lab's log.

## The loop

```sh
# 1. run the suite against real machines
cd ../tui-lab && ./lab.sh test tui-firewall

# 2. harvest the evidence out of the lab logs, then regenerate `tested`
make compat

# 3. put the same facts in the README
make readme
```

`make compat` is:

```sh
python3 $(KIT)/tools/compat-sync.py --manifest tool.json \
    --results compat/results.jsonl \
    --from-log ../tui-lab/out/results/*-tui-firewall/*.log
```

CI runs `compat-sync.py --check` and `render-compat.py --check`, so a manifest
that drifted from the evidence, or a README that drifted from the manifest,
fails the build instead of quietly overstating what the tool supports.

## The exec boundary

Compatibility work means reading versions, and reading a version means starting
a process — which is exactly what the family's central promise limits. The rule
is enforced, not remembered:

```sh
tui-kit/tools/check-exec.sh .
```

`os/exec` may be imported by `tui-kit/runner` and by a tool's
`internal/<backend>/` package, and nowhere else. `compat.Probe` obeys it: the
probe goes through the same `runner` every previewed command goes through.
